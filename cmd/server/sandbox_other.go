// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

//go:build !ios

package main

// resolveSandboxPaths 非 iOS 构建上是空操作：相对路径沿用进程工作目录解析，
// 与原有行为一致。
func resolveSandboxPaths(cfg *Config, cfgPath string) {}
