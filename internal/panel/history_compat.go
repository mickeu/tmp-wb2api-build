// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

package panel

import (
	"net/http"
	"strconv"
	"time"

	"github.com/yreidev/workbuddy2api-panel/internal/balance"
	"github.com/yreidev/workbuddy2api-panel/internal/logarchive"
	"github.com/yreidev/workbuddy2api-panel/internal/upstream"
)

// creditHistoryCompat adapts actual earning records; earning credits have no before/after balance.
func (p *Panel) creditHistoryCompat(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, creditLogMax)
	}
	entries := make([]map[string]any, 0)
	if p.cfg.CreditLog != nil {
		rows, _, err := logarchive.Read(p.cfg.CreditLog, logarchive.Query[upstream.Earning]{
			Limit: limit,
			Time:  func(e upstream.Earning) time.Time { return e.Time },
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, e := range rows {
			entries = append(entries, map[string]any{
				"account": e.Nickname, "uid": e.UID, "delta": e.Credit, "time": e.Time,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "limit": limit})
}

// balanceHistoryCompat 返回每日余额快照。快照从网关启动后按日采样，
// 没装 Balance 记录器或还没跨过一天时序列为空（不编造数据点）。
func (p *Panel) balanceHistoryCompat(w http.ResponseWriter, r *http.Request) {
	days := 7
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n > 0 {
		days = min(n, 365)
	}
	series := []any{}
	if p.cfg.Balance != nil {
		for _, e := range p.cfg.Balance.Series(days) {
			series = append(series, map[string]any{
				"day": e.Day, "opening": e.Opening, "closing": e.Closing,
				"total_grant": e.TotalGrant, "spent": e.Spent, "sampled_at": e.SampledAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"days": days, "now": time.Now().Format(time.RFC3339), "series": series,
	})
}

// creditToday 按余额差计算「今日消耗」。
//
//	今日消耗 = 开盘余额 + 当天发放 - 当前余额
//
// 三个数都来自 balance.Recorder 的当日快照，与上游 usage.credit 的口径无关：
// 后者对缓存命中的请求不计费，累加值小于余额实际降幅。
func (p *Panel) creditToday(w http.ResponseWriter, r *http.Request) {
	if p.cfg.Balance == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "balance recorder not configured"})
		return
	}
	day := balance.Day(time.Now())
	e, ok := p.cfg.Balance.Get(day)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "no sample for today yet", "day": day})
		return
	}
	issued := e.TotalGrant - e.OpenGrant
	if issued < 0 {
		issued = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "day": day,
		"opening": e.Opening, "issued": issued,
		"current": e.Closing, "spent": e.Spent,
		"sampled_at": e.SampledAt,
	})
}

// tasksJobs 兼容接口：把执行队列以 jobs 列表返回。
//
// 与 tasksQueueStatus 同一份队列状态；除 items 外另给 jobs 键，
// 供只认 jobs 字段的调用方使用。队列未启动时 jobs 为空数组。
// （本 handler 在交付的源码包里缺失，panel.go 只注册了路由；
// 生产参考二进制里该路由存在，此处按 tasksQueueStatus 的同一状态补齐。）
func (p *Panel) tasksJobs(w http.ResponseWriter, r *http.Request) {
	q := p.queue()
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]queueItem, len(q.items))
	copy(items, q.items)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"running":    q.running,
		"total":      len(items),
		"conc":       q.conc,
		"started":    !q.startedAt.IsZero(),
		"started_at": q.startedAt,
		"seq":        q.seq,
		"jobs":       items,
		"items":      items,
	})
}
