// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

// listen_test.go 缺陷 5 单测：快速 stop→start 的端口重绑。
package main

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestListenWithRetryAfterClose 模拟 Close() 后立即 Start()：
// 先占端口并建立一条真实连接再全关（端口进 TIME_WAIT），
// 立刻重绑必须在 3 秒内成功。
func TestListenWithRetryAfterClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	// 建立一条连接再关掉，让端口真实进入 TIME_WAIT。
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", addr)
	if err != nil {
		ln.Close()
		t.Fatalf("dial: %v", err)
	}
	server := <-accepted
	client.Close()
	server.Close()
	ln.Close()

	start := time.Now()
	ln2, err := listenWithRetry(context.Background(), "tcp", addr, time.Now().Add(3*time.Second))
	if err != nil {
		t.Fatalf("重绑失败：%v", err)
	}
	defer ln2.Close()
	if got := time.Since(start); got > 3*time.Second {
		t.Fatalf("重绑耗时 %v，超过 3s", got)
	}
}

// TestListenWithRetryWaitsForPort 覆盖重试循环本身：端口先被占着，
// 150ms 后释放，重绑应在重试中成功（不是一次失败就退出）。
func TestListenWithRetryWaitsForPort(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := holder.Addr().String()
	go func() {
		time.Sleep(150 * time.Millisecond)
		holder.Close()
	}()

	start := time.Now()
	ln, err := listenWithRetry(context.Background(), "tcp", addr, time.Now().Add(3*time.Second))
	if err != nil {
		t.Fatalf("重试后仍失败：%v", err)
	}
	defer ln.Close()
	if got := time.Since(start); got > 3*time.Second {
		t.Fatalf("重绑耗时 %v，超过 3s", got)
	}
	// 必须经历过至少一次重试（第一次 bind 时端口还被占着）。
	if got := time.Since(start); got < 100*time.Millisecond {
		t.Fatalf("重绑耗时 %v，疑似未走重试就成功", got)
	}
}

// TestListenGatewayPortFallback 端口降级：basePort 被占时落到 basePort+1。
func TestListenGatewayPortFallback(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer holder.Close()
	base := holder.Addr().(*net.TCPAddr).Port

	ln, port, err := listenGateway(context.Background(), base)
	if err != nil {
		t.Fatalf("listenGateway: %v", err)
	}
	defer ln.Close()
	if port != base+1 {
		t.Fatalf("want port %d, got %d", base+1, port)
	}
}
