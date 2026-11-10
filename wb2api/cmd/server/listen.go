// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

// listen.go 监听端口绑定：SO_REUSEADDR + 指数退避重试。
//
// 背景（缺陷 5）：WBG_Stop 关闭 listener 后端口进入 TIME_WAIT，
// WBG_Start 立刻 rebind 失败，旧代码一次失败就整体退出；实测要等 10 秒
// 重试才成功。iOS/Darwin 上 net.Listen 不一定带 SO_REUSEADDR，
// 必须用 ListenConfig.Control + syscall.SetsockoptInt 手动设。
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"syscall"
	"time"
)

// listenAttempts 单个地址最多尝试次数；listenDelays 为各次重试前的等待
// （100ms → 200ms → 400ms → 800ms，合计 1.5s；5 次尝试总时长 ≤3s）。
const listenAttempts = 5

var listenDelays = []time.Duration{
	100 * time.Millisecond,
	200 * time.Millisecond,
	400 * time.Millisecond,
	800 * time.Millisecond,
}

// listenWithRetry 绑定一个地址：SO_REUSEADDR + 指数退避重试。
// deadline 之前仍未成功返回最后一次错误；ctx 取消立即返回。
// 每次重试打一条日志：bind 重试第 N 次（原因）。
func listenWithRetry(ctx context.Context, network, address string, deadline time.Time) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				opErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	var err error
	for attempt := 1; attempt <= listenAttempts; attempt++ {
		var ln net.Listener
		ln, err = lc.Listen(ctx, network, address)
		if err == nil {
			return ln, nil
		}
		if attempt == listenAttempts {
			return nil, err
		}
		// 剩余时间不够一次延迟就直接放弃（总时长 ≤3s）。
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, err
		}
		delay := listenDelays[attempt-1]
		if delay > remaining {
			delay = remaining
		}
		log.Printf("bind 重试第 %d 次（%v）", attempt, err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, err
}

// listenGateway 绑定网关监听端口：basePort 起依次 +1 降级（17865→17866→17867），
// 每个端口走 listenWithRetry。整个绑定序列共享一个 3 秒预算；
// 全部失败返回最后一次错误。
func listenGateway(ctx context.Context, basePort int) (net.Listener, int, error) {
	deadline := time.Now().Add(3 * time.Second)
	var ln net.Listener
	var err error
	for i := 0; i < 3; i++ {
		p := basePort + i
		ln, err = listenWithRetry(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", p), deadline)
		if err == nil {
			return ln, p, nil
		}
		if time.Now().After(deadline) {
			break
		}
	}
	return nil, 0, err
}
