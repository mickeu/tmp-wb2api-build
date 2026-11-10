// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

// boot.go 启动流程抽取：main() 与 iOS 库入口共用。
// 对上游代码的唯一结构性修改：把 main() 里「读配置 → 配 DNS → 建 updater →
// run(...) → 重启处理」抽成 boot(ctx, cfgPath, ln)，main() 只剩 flag 解析与
// 信号上下文；业务逻辑一行不动。
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/yreidev/workbuddy2api-panel/internal/upstream"
)

// fdLogWriter 把日志写到宿主 App 注入的文件 fd；目标 fd 可热换
// （日志轮转时 WBG_SetLogFD 换新 fd，run() 的 MultiWriter 持有的是
// 这个 wrapper，无需重新 SetOutput）。
type fdLogWriter struct {
	mu sync.Mutex
	f  *os.File
}

func (w *fdLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return len(p), nil
	}
	return w.f.Write(p)
}

// setLogFile 更换日志目标 fd（WBG_SetLogFD 调用）。
func setLogFile(f *os.File) {
	iosLogWriter.mu.Lock()
	defer iosLogWriter.mu.Unlock()
	iosLogWriter.f = f
}

// iosLogWriter 是 iOS 宿主 App 注入的额外日志目的地（WBG_SetLogFD 传入的 fd）。
// 未注入时丢弃，run() 里的 MultiWriter 可无条件包含它。
// 由 ioslib.go（//go:build ios）在 WBG_SetLogFD 里设置。
var iosLogWriter = &fdLogWriter{}

// boot 启动网关的完整流程：读配置 → 配 DNS → 建 updater → run(...)。
// main() 与 iOS 库入口共用。ln 非 nil 时 run() 直接用它监听，
// 并把 cfg.Listen 改写为实际监听地址，保证面板与日志显示正确。
func boot(ctx context.Context, cfgPath string, ln net.Listener) (bool, error) {
	cfg, err := loadOrInitConfig(cfgPath)
	if err != nil {
		return false, fmt.Errorf("load config: %v", err)
	}
	// iOS 沙盒：相对路径（state_file/auth_dir 等）按配置文件所在目录解析，
	// 不依赖进程工作目录。非 iOS 构建上 resolveSandboxPaths 是空操作。
	resolveSandboxPaths(cfg, cfgPath)
	if ln != nil {
		cfg.Listen = ln.Addr().String()
	}
	// 出站 DNS：没有 /etc/resolv.conf 的环境里 Go 的解析器会退回问本机 53 端口，查什么都失败。
	// 配了 dns_servers / WB2A_DNS 就让整个进程只问它们；发出任何请求之前换。
	if len(cfg.DNSServers) > 0 {
		upstream.UseDNSServers(cfg.DNSServers)
		log.Printf("DNS: 出站解析只问 %v（不走系统解析器）", cfg.DNSServers)
	}
	// 数据目录里有面板装好的新版就 exec 过去（不返回）：放在读取账号、恢复状态之前，什么都还没动。
	upd := launchUpdater(filepath.Dir(cfg.StateFile), appVersion)
	restart, err := run(ctx, cfg, cfgPath, upd, newUpstream(), ln)
	if err != nil {
		return false, err
	}
	// 面板一键更新：run 优雅停机返回后原地换成自带程序（它再决定是否切到数据目录里的新版），
	// 放在 run 之外，让 run 里的 defer（池状态落盘、用量与请求日志收尾）先跑完。
	if restart {
		log.Printf("重启中……")
		if err := reexec(upd); err != nil {
			return false, fmt.Errorf("重启失败：%v", err)
		}
	}
	return false, nil
}
