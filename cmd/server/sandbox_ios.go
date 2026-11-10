// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

//go:build ios

package main

import (
	"path/filepath"
)

// resolveSandboxPaths 把配置里的相对路径按配置文件所在目录解析为绝对路径。
// iOS 沙盒里进程工作目录不可写、家目录在容器外，相对路径不能靠它们解析；
// 配置文件由宿主 App 以绝对路径传入，其所在目录即沙盒内的数据目录。
// （路径基准只从 cfgPath 派生；本文件不调用任何工作目录/家目录查询。）
func resolveSandboxPaths(cfg *Config, cfgPath string) {
	dir := filepath.Dir(cfgPath)
	if !filepath.IsAbs(cfg.StateFile) {
		cfg.StateFile = filepath.Join(dir, cfg.StateFile)
	}
	if !filepath.IsAbs(cfg.AuthDir) {
		cfg.AuthDir = filepath.Join(dir, cfg.AuthDir)
	}
	if cfg.Upstream.DeviceTokenFile != "" && !filepath.IsAbs(cfg.Upstream.DeviceTokenFile) {
		cfg.Upstream.DeviceTokenFile = filepath.Join(dir, cfg.Upstream.DeviceTokenFile)
	}
	if cfg.Prompt.File != "" && !filepath.IsAbs(cfg.Prompt.File) {
		cfg.Prompt.File = filepath.Join(dir, cfg.Prompt.File)
	}
}
