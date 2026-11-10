// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yreidev/workbuddy2api-panel/internal/ledger"
)

// TestNextWakeShanghai V3（时区）：TZ=Asia/Shanghai 下，9 点签到落在当天 09:00 CST，
// 而不是 UTC 09:00（北京时间 17:00）。nextWake 用 now.Location() 构造触发时刻，
// 进程时区正确时即为 CST。
func TestNextWakeShanghai(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation Asia/Shanghai: %v", err)
	}
	s := New(Config{
		CheckinHours:   []int{9, 21},
		TravelDisabled: true, ActivityDisabled: true,
		KeepaliveDisabled: true, BlackcatDisabled: true, GrowthDisabled: true,
	})
	now := time.Date(2026, 11, 10, 8, 0, 0, 0, loc)
	at, kinds := s.nextWake(now)
	want := time.Date(2026, 11, 10, 9, 0, 0, 0, loc)
	if !at.Equal(want) {
		t.Errorf("next=%v want %v（9 点任务应落当天 09:00 CST）", at, want)
	}
	if len(kinds) != 1 || kinds[0] != taskCheckin {
		t.Errorf("kinds=%v want [checkin]", kinds)
	}
	// 同一时刻换算成 UTC 应为 01:00（+8），不是 09:00 UTC。
	if at.UTC().Hour() != 1 {
		t.Errorf("next UTC hour=%d want 1（CST+8）", at.UTC().Hour())
	}
}

// TestNextWakeStaggerWhenLedgerUnreachable V9（降级·错峰）：App 实例且台账不可达时，
// 9 点任务错峰到 09:07（{7,27,47} 中最接近原时刻的一档），与越狱机 :00 错开 7 分钟。
func TestNextWakeStaggerWhenLedgerUnreachable(t *testing.T) {
	loc := time.Local
	s := New(Config{
		InstanceID:     "app-ios",
		CheckinHours:   []int{9, 21},
		TravelDisabled: true, ActivityDisabled: true,
		KeepaliveDisabled: true, BlackcatDisabled: true, GrowthDisabled: true,
		// Ledger 为 nil = 台账不可达（降级）。
	})
	at, kinds := s.nextWake(time.Date(2026, 11, 10, 8, 0, 0, 0, loc))
	want := time.Date(2026, 11, 10, 9, 7, 0, 0, loc)
	if !at.Equal(want) {
		t.Errorf("next=%v want %v（App + 台账不可达 → 错峰 :07）", at, want)
	}
	if len(kinds) != 1 || kinds[0] != taskCheckin {
		t.Errorf("kinds=%v want [checkin]", kinds)
	}
}

// TestNextWakeNoStaggerWhenLedgerReachable V9（对照）：App 实例但台账可达时，
// 9 点任务按配置整点走 09:00（认领机制已去重，无需错峰）。
func TestNextWakeNoStaggerWhenLedgerReachable(t *testing.T) {
	srv := newClaimOKServer(t)
	defer srv.Close()
	lc := ledger.New(srv.URL, "tok", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	if claimed, _, err := lc.Claim(context.Background(), "checkin", "u1"); err != nil || !claimed {
		t.Fatalf("setup claim: claimed=%v err=%v", claimed, err)
	}
	if !lc.Reachable() {
		t.Fatalf("Reachable()=false after successful claim")
	}
	s := New(Config{
		InstanceID:     "app-ios",
		Ledger:         lc,
		CheckinHours:   []int{9, 21},
		TravelDisabled: true, ActivityDisabled: true,
		KeepaliveDisabled: true, BlackcatDisabled: true, GrowthDisabled: true,
	})
	loc := time.Local
	at, _ := s.nextWake(time.Date(2026, 11, 10, 8, 0, 0, 0, loc))
	want := time.Date(2026, 11, 10, 9, 0, 0, 0, loc)
	if !at.Equal(want) {
		t.Errorf("next=%v want %v（App + 台账可达 → 不错峰）", at, want)
	}
}

// TestNextWakeNoStaggerForJailbroken V9（对照）：越狱机实例即使台账不可达也不错峰
// （越狱机侧本就是 :00/:20/:40，错峰只约束 App）。
func TestNextWakeNoStaggerForJailbroken(t *testing.T) {
	loc := time.Local
	s := New(Config{
		InstanceID:     "jb-iphone12",
		CheckinHours:   []int{9, 21},
		TravelDisabled: true, ActivityDisabled: true,
		KeepaliveDisabled: true, BlackcatDisabled: true, GrowthDisabled: true,
	})
	at, _ := s.nextWake(time.Date(2026, 11, 10, 8, 0, 0, 0, loc))
	want := time.Date(2026, 11, 10, 9, 0, 0, 0, loc)
	if !at.Equal(want) {
		t.Errorf("next=%v want %v（越狱机不该错峰）", at, want)
	}
}

// TestNearestStaggerMinute 错峰分钟取 {7,27,47} 中最接近原分钟的一档。
func TestNearestStaggerMinute(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 7}, {5, 7}, {15, 7}, {17, 7}, {20, 27}, {30, 27}, {37, 27}, {40, 47}, {50, 47}, {59, 47},
	}
	for _, c := range cases {
		if got := nearestStaggerMinute(c.in); got != c.want {
			t.Errorf("nearestStaggerMinute(%d)=%d want %d", c.in, got, c.want)
		}
	}
}

// TestClaimForLocalDedup V9（降级·本地去重）：无台账时 claimFor 走本地去重，
// 同 (task,uid) 当天完成后再次认领被拦；任务失败则下一时点可重试。
func TestClaimForLocalDedup(t *testing.T) {
	lc := ledger.New("", "", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	s := New(Config{Ledger: lc})
	ctx := context.Background()

	ok, ledgerOK, date := s.claimFor(ctx, "checkin", "u1")
	if !ok || ledgerOK || date == "" {
		t.Fatalf("first claimFor: ok=%v ledgerOK=%v date=%q want true/false/non-empty", ok, ledgerOK, date)
	}
	// 任务成功 → 本地写终态；同天再认领被拦。
	s.localFinish(date, "checkin", "u1", true)
	if ok, _, _ := s.claimFor(ctx, "checkin", "u1"); ok {
		t.Fatalf("second claimFor: want skip (local dedup), got run")
	}
	// 任务失败 → 下一时点可重试。
	ok, _, date2 := s.claimFor(ctx, "keepalive", "u1")
	if !ok {
		t.Fatalf("keepalive first claimFor: want run")
	}
	s.localFinish(date2, "keepalive", "u1", false)
	if ok, _, _ := s.claimFor(ctx, "keepalive", "u1"); !ok {
		t.Fatalf("keepalive retry claimFor: want run after failure")
	}
	// 无 Ledger（测试/旧调用方）时 claimFor 不拦截。
	s2 := New(Config{})
	if ok, _, _ := s2.claimFor(ctx, "checkin", "u1"); !ok {
		t.Fatalf("claimFor without ledger: want run")
	}
}

// TestClaimForLedgerConflict V6/V8（台账·互检）：App 认领后，另一实例同 (task,uid)
// 的 claimFor 返回 shouldRun=false（日志里有 [ledger] skip）。
func TestClaimForLedgerConflict(t *testing.T) {
	srv := newStatefulClaimServer(t, "tok")
	defer srv.Close()
	ctx := context.Background()

	appLC := ledger.New(srv.URL, "tok", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	jbLC := ledger.New(srv.URL, "tok", "jb-iphone12", filepath.Join(t.TempDir(), "ledger-local.json"))
	app := New(Config{Ledger: appLC, InstanceID: "app-ios"})
	jb := New(Config{Ledger: jbLC, InstanceID: "jb-iphone12"})

	ok, ledgerOK, _ := app.claimFor(ctx, "checkin", "u1")
	if !ok || !ledgerOK {
		t.Fatalf("app claimFor: ok=%v ledgerOK=%v want true/true", ok, ledgerOK)
	}
	ok, ledgerOK, _ = jb.claimFor(ctx, "checkin", "u1")
	if ok || !ledgerOK {
		t.Fatalf("jb claimFor: ok=%v ledgerOK=%v want false/true", ok, ledgerOK)
	}
	// App 释放后，对方可以认领。
	app.finishOrRelease(ctx, "checkin", "u1", false, "")
	ok, _, _ = jb.claimFor(ctx, "checkin", "u1")
	if !ok {
		t.Fatalf("jb claimFor after release: want run")
	}
}

// newClaimOKServer 恒返回 claimed=true 的台账 mock（仅供错峰判定测试）。
func newClaimOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"claimed": true, "date": "2026-11-10"})
	}))
}

// newStatefulClaimServer 带状态的台账 mock：认领/释放按 (task,uid) 互斥，token 鉴权。
func newStatefulClaimServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	held := map[string]string{} // "task:uid" -> holder
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "unauthorized"})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		key := body["task"].(string) + ":" + body["uid"].(string)
		inst, _ := body["instance"].(string)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/claim":
			if h, ok := held[key]; ok && h != inst {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"claimed": false, "holder": h, "state": "running",
				})
				return
			}
			held[key] = inst
			_ = json.NewEncoder(w).Encode(map[string]any{"claimed": true, "date": "2026-11-10"})
		case "/finish":
			if h, ok := held[key]; !ok || h != inst {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "not-holder"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/release":
			if h, ok := held[key]; !ok || h != inst {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "not-holder"})
				return
			}
			delete(held, key)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}
