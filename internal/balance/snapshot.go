// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only

// Package balance 记录每日余额快照。
//
// 上游 usage 接口的 credits 取自每次请求返回的 usage.credit，缓存命中的请求通常
// 不计费，累加值与账号余额的实际降幅对不上；要回答「今天花了多少分」只能拿
// 余额差来算：
//
//	今日消耗 = 昨日收盘余额 + 今日发放 - 当前余额
//
// 本包只管「余额」，发放量由 credit_log 提供。
package balance

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry 一个自然日（东八区）的余额快照。
type Entry struct {
	Day        string `json:"day"`         // 2026-11-06
	Opening    int64  `json:"opening"`     // 当天 0 点余额（= 前一天的 Closing）
	Closing    int64  `json:"closing"`     // 当天最后一次采样余额
	OpenGrant  int64  `json:"open_grant"`  // 当天首次采样时的累计发放
	TotalGrant int64  `json:"total_grant"` // 最近一次采样的累计发放
	SampledAt  string `json:"sampled_at"`  // 最后一次采样时间 RFC3339
	Spent      int64  `json:"spent"`       // 当天消耗 = Opening + 当天发放 - Closing
}

// ReadStateCredits 读 state.json 里最后落盘的余额。
//
// 开机后第一轮余额刷新失败时，内存里的余额会是 0（上游账号接口偶发连不上），
// 此时用 state.json 的最后已知值兜底，避免把当天消耗算成整个开盘余额。
func ReadStateCredits(stateFile string) (credits, grant int64, err error) {
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return 0, 0, err
	}
	var st struct {
		Accounts json.RawMessage `json:"accounts"`
	}
	if err = json.Unmarshal(raw, &st); err != nil {
		return 0, 0, err
	}
	type acct struct {
		Credits      int64 `json:"credits"`
		CreditsTotal int64 `json:"credits_total"`
	}
	// accounts 在 state.json 里是 uid -> 账号 的字典，早期版本可能是数组，两种都认。
	var list []acct
	if err = json.Unmarshal(st.Accounts, &list); err != nil {
		var m map[string]acct
		if err = json.Unmarshal(st.Accounts, &m); err != nil {
			return 0, 0, err
		}
		for _, a := range m {
			list = append(list, a)
		}
	}
	for _, a := range list {
		credits += a.Credits
		grant += a.CreditsTotal
	}
	return credits, grant, nil
}

// Recorder 按天落盘的余额记录器。文件为 JSON Lines，一天一行，按天覆盖。
type Recorder struct {
	path string

	mu      sync.Mutex
	entries map[string]*Entry // day -> entry
}

// New 打开（或创建）指定目录下的 balance-daily.jsonl。
func New(dir string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Recorder{path: filepath.Join(dir, "balance-daily.jsonl"), entries: map[string]*Entry{}}
	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return r, nil
}

func (r *Recorder) load() error {
	f, err := os.Open(r.path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Day != "" {
			r.entries[e.Day] = &e
		}
	}
	return sc.Err()
}

var cst = time.FixedZone("CST", 8*3600)

// Day 返回 t 在东八区所属的自然日。
func Day(t time.Time) string { return t.In(cst).Format("2006-01-02") }

// Sample 采样当前余额总和。同一天重复调用会覆盖当天的 Closing，
// Opening 只在当天第一次采样时定下（取传入的 opening，第一次通常就是当前值）。
func (r *Recorder) Sample(total int64, grantTotal int64, at time.Time) Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	day := Day(at)
	// 启动初期还有账号没拉到余额，此时总和为 0，不能当作真实收盘写盘，
	// 否则当天消耗会被算成整个开盘余额。
	if total <= 0 {
		if e, ok := r.entries[day]; ok {
			return *e
		}
		return Entry{Day: day}
	}
	e, ok := r.entries[day]
	if !ok {
		e = &Entry{Day: day, Opening: total, OpenGrant: grantTotal}
		r.entries[day] = e
	}
	e.Closing = total
	e.TotalGrant = grantTotal
	e.SampledAt = at.In(cst).Format(time.RFC3339)
	// 当天消耗 = 开盘余额 + 当天发放 - 当前余额。
	// 开盘余额优先取昨日收盘；没有昨日快照时退回当天首次采样值。
	if prev, ok := r.entries[Day(at.AddDate(0, 0, -1))]; ok {
		e.Opening = prev.Closing
	}
	// 当天发放 = 当前累计发放 - 开盘时累计发放。
	issued := grantTotal - e.OpenGrant
	if issued < 0 {
		issued = 0
	}
	e.Spent = e.Opening + issued - total
	r.flush()
	return *e
}

// Get 取某天的快照。
func (r *Recorder) Get(day string) (Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[day]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// Series 取最近 n 天的快照，按日期升序。
func (r *Recorder) Series(n int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().In(cst)
	out := make([]Entry, 0, n)
	for i := n - 1; i >= 0; i-- {
		day := Day(now.AddDate(0, 0, -i))
		if e, ok := r.entries[day]; ok {
			out = append(out, *e)
		}
	}
	return out
}

// flush 全量重写文件（一天一行，条目数上限 365）。
func (r *Recorder) flush() {
	tmp := r.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, e := range r.entries {
		b, err := json.Marshal(e)
		if err != nil {
			continue
		}
		w.Write(b)
		w.WriteByte('\n')
	}
	w.Flush()
	f.Close()
	os.Rename(tmp, r.path)
}
