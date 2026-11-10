// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

//go:build ios

// ioslib.go iOS 宿主 App 的 Go 侧入口：把网关以 c-archive 静态链接进 App 进程，
// 通过 C 导出函数供 ObjC 层启动/停止/查询。
//
// 行为契约（TASK.md §5.1）：
//   - WBG_Start 立即返回，内部 goroutine 跑 boot()；重复调用幂等。
//   - 端口冲突按 17865 → 17866 → 17867 降级，实际端口经 WBG_Port() 暴露。
//   - 不调 os.Exit、不 exec；panic 由启动 goroutine 外层的 recover 兜住。
//   - 路径：相对路径由 boot() 里的 resolveSandboxPaths 按配置文件所在目录
//     解析，绝不依赖进程工作目录或家目录（沙盒里两者都不可靠）。
//   - WBG_LastError 返回的 C 字符串归 Go 侧所有（缓存在 iosLastErr，
//     下次记录时释放）；调用方只读并拷走，**不要 free**，否则 double-free。
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
	"unsafe"

	_ "time/tzdata" // 沙盒无 /usr/share/zoneinfo，必须内嵌；体积 +约 450KB

	"github.com/yreidev/workbuddy2api-panel/internal/server"
)

var (
	iosMu      sync.Mutex
	iosCancel  context.CancelFunc
	iosRunning bool
	iosPort    int
	// iosLastErr 最近一次错误的 C 字符串缓存；nil = 无错误。
	// 由 Go 侧持有，下次 setLastErr 时释放；调用方只读不释放。
	iosLastErr *C.char
	iosLogFile *os.File
)

// setLastErr 记录最近一次错误（调用方须持有 iosMu）。
func setLastErr(err error) {
	if iosLastErr != nil {
		C.free(unsafe.Pointer(iosLastErr))
		iosLastErr = nil
	}
	if err != nil {
		iosLastErr = C.CString(err.Error())
	}
}

// WBG_SetLogFD 注入宿主 App 的日志文件 fd（ObjC 侧在启动网关前调用一次，
// 日志轮转时再调一次换新 fd）。
// Go 侧只写不关闭：fd 归宿主所有，os.NewFile 的 finalizer 已清掉，
// 避免 GC 回收时关掉宿主的 fd；fd < 0 表示停用（目标恢复为丢弃）。
//
//export WBG_SetLogFD
func WBG_SetLogFD(fd C.int) {
	iosMu.Lock()
	defer iosMu.Unlock()
	if fd < 0 {
		setLogFile(nil)
		iosLogFile = nil
		return
	}
	f := os.NewFile(uintptr(fd), "wbg")
	if f == nil {
		return
	}
	runtime.SetFinalizer(f, nil)
	iosLogFile = f
	setLogFile(f)
}

// applyTimeZone 显式固定本地时区。
// 调用时机：每次 WBG_Start，而不是 init —— 宿主可能先 stop 再 start，且 TZ 由 ObjC 在
// 每次启动前设置，init 阶段读到的是旧值。
func applyTimeZone() {
	for _, name := range []string{os.Getenv("TZ"), "Asia/Shanghai", "CST-8"} {
		if name == "" {
			continue
		}
		if loc, err := time.LoadLocation(name); err == nil {
			time.Local = loc
			log.Printf("timezone: %s (now %s)", time.Local.String(), time.Now().Format(time.RFC3339))
			return
		}
	}
	// 全部失败：中国无夏令时，固定 +8 与正解等价
	time.Local = time.FixedZone("CST", 8*3600)
	log.Printf("timezone: %s (fallback, now %s)", time.Local.String(), time.Now().Format(time.RFC3339))
}

// WBG_Start 启动网关（立即返回，0 = 已启动/已在运行，-1 = 失败，
// 失败原因经 WBG_LastError 取）。重复调用幂等。
//
//export WBG_Start
func WBG_Start(cfgPath *C.char, port C.int) C.int {
	applyTimeZone()        // 时区三层保险之 Go 层：必须在 boot 之前
	server.ClearDraining() // stop→start 复用同一进程：复位停机标记，否则新起的 server 一直 503
	// iOS App 内存与 GC 上限：384 MiB、GOGC 50、最多 2 个 P。
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(384 << 20)
	runtime.GOMAXPROCS(2)

	iosMu.Lock()
	defer iosMu.Unlock()
	if iosRunning {
		return 0
	}
	if cfgPath == nil {
		setLastErr(fmt.Errorf("WBG_Start: cfgPath is nil"))
		return -1
	}
	cfg := C.GoString(cfgPath)
	// 早期日志（读配置、DNS）也进宿主日志文件；run() 里会再接上面板环形缓冲。
	if iosLogFile != nil {
		log.SetOutput(io.MultiWriter(iosLogWriter, os.Stderr))
	}
	// 端口绑定：SO_REUSEADDR + 指数退避重试 + 冲突时 +1/+2 降级。
	// TIME_WAIT 下快速 stop→start 不再一次失败就退出（总时长 ≤3s）。
	ln, p, err := listenGateway(context.Background(), int(port))
	if err != nil {
		setLastErr(fmt.Errorf("listen 127.0.0.1:%d: %w", int(port), err))
		return -1
	}
	iosPort = p
	ctx, cancel := context.WithCancel(context.Background())
	iosCancel = cancel
	iosRunning = true
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("WBG: 启动 goroutine panic 已捕获: %v", r)
				iosMu.Lock()
				iosRunning = false
				iosPort = 0
				setLastErr(fmt.Errorf("panic: %v", r))
				iosMu.Unlock()
			}
		}()
		_, err := boot(ctx, cfg, ln)
		iosMu.Lock()
		iosRunning = false
		iosPort = 0
		if err != nil {
			setLastErr(err)
		}
		iosMu.Unlock()
	}()
	return 0
}

// WBG_Stop 停止网关：取消内部 ctx，run() 走优雅停机后退出。幂等。
//
//export WBG_Stop
func WBG_Stop() {
	iosMu.Lock()
	cancel := iosCancel
	iosMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WBG_Port 返回实际监听端口（端口冲突降级后的真实值；未启动返回 0）。
//
//export WBG_Port
func WBG_Port() C.int {
	iosMu.Lock()
	defer iosMu.Unlock()
	return C.int(iosPort)
}

// WBG_LastError 返回最近一次错误字符串（nil = 无错误）。
// 缓冲归 Go 侧所有，调用方只读并拷走，不要 free。
//
//export WBG_LastError
func WBG_LastError() *C.char {
	iosMu.Lock()
	defer iosMu.Unlock()
	return iosLastErr
}

// init 在包初始化阶段就把本地时区钉死。
//
// 为什么不能只靠 WBG_Start：daemon 走 main → boot → run，从不经过 WBG_Start，
// 于是 applyTimeZone 永远不执行。Go 的 initLocal 在 rootless 越狱机上会回落 UTC
// （/etc/localtime 不存在，/etc 又是只读快照建不了软链，TZ 环境变量也不被认），
// 结果 scheduler 的 nextFire 按 UTC 算钟点，「09:00 签到」实际跑到北京时间 17:00。
// applyTimeZone 末段的 FixedZone("CST", 8h) 兜底保证无论 LoadLocation 成败都落到 +8。
// 与 WBG_Start 内的调用重复，但幂等。
func init() { applyTimeZone() }
