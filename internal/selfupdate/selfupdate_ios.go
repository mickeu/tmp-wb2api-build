// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

//go:build ios

// selfupdate_ios.go iOS 构建下的一键更新空实现：App 内 exec 原地重启
// 必然失败且可能被 AMFI 杀进程，故全部禁用，只保留与非 iOS 版同签名
// 的 API，让 main() 与面板照常编译、运行时走"不支持"分支。
package selfupdate

import (
	"errors"
	"time"
)

// StableAfter 新版启动后跑满这么久才算成功（与非 iOS 版同值；
// iOS 上 MarkStable 是空操作，该值仅供 main() 的 AfterFunc 引用）。
const StableAfter = 30 * time.Second

// ErrBusy 已经在更新或重启（iOS 上 Start/Rollback 直接返回禁用错误，
// 该值保留供面板的 updaterErrStatus 判 409 用）。
var ErrBusy = errors.New("正在更新或重启，请稍候")

// Status 面板展示用的状态（与非 iOS 版同字段）。
type Status struct {
	Supported  bool   `json:"supported"`
	Reason     string `json:"reason,omitempty"` // 不支持一键更新的原因
	Base       string `json:"base"`             // 程序自带的版本
	Override   bool   `json:"override"`         // 正在跑面板装到数据目录的版本
	Phase      string `json:"phase"`
	Target     string `json:"target,omitempty"` // 正在更新到 / 退回到的版本
	Done       int64  `json:"done,omitempty"`   // 已下载字节
	Total      int64  `json:"total,omitempty"`  // 安装包大小；不知道时为 0
	Error      string `json:"error,omitempty"`  // 上一次更新失败的原因
	RolledBack string `json:"rolled_back,omitempty"`
	GraceSec   int    `json:"grace_sec"` // 优雅停机最长等待
}

// Updater 一键更新的 iOS 空实现。Launch 创建；main 设好 Restart 和 Grace
// 后交给面板（面板据 Status().Supported=false 展示"不支持"）。
type Updater struct {
	// Restart 让网关优雅停机，停完后由 main 调 Reexec。
	Restart func()
	// Grace 优雅停机最长等待，面板确认框里提示用。
	Grace time.Duration
}

// Launch iOS 上不做任何事：不读数据目录、不 exec，返回空 Updater。
func Launch(dataDir, version string) *Updater {
	return &Updater{}
}

// Reexec iOS 上不支持原地重启。
func (u *Updater) Reexec() error {
	return errors.New("self-update disabled on iOS app")
}

// MarkStable 空操作。
func (u *Updater) MarkStable() {}

// Status 当前状态：一键更新不支持。
func (u *Updater) Status() Status {
	return Status{
		Supported: false,
		Reason:    "iOS App 不支持一键更新，请手动升级",
		GraceSec:  int(u.Grace.Round(time.Second) / time.Second),
	}
}

// Start iOS 上不支持一键更新。
func (u *Updater) Start(version string) error {
	return errors.New("self-update disabled on iOS app")
}

// Rollback iOS 上不支持。
func (u *Updater) Rollback() (bool, error) {
	return false, errors.New("self-update disabled on iOS app")
}
