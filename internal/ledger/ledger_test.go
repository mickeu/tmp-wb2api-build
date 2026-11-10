// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

package ledger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockLedger §4.2 契约的内存版实现（测试用）：token 鉴权、认领/终态/释放、/health。
type mockLedger struct {
	mu      sync.Mutex
	token   string
	date    string
	entries map[string]*mockEntry
}

type mockEntry struct {
	holder string
	state  string // running | done
}

func newMockLedger(token string) *mockLedger {
	return &mockLedger{token: token, date: "2026-11-10", entries: map[string]*mockEntry{}}
}

func (m *mockLedger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/health" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "tz": "CST+8", "now": time.Now().Format(time.RFC3339), "date": m.date,
		})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+m.token {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "unauthorized"})
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	task, _ := body["task"].(string)
	uid, _ := body["uid"].(string)
	inst, _ := body["instance"].(string)
	key := task + ":" + uid + ":" + m.date

	m.mu.Lock()
	defer m.mu.Unlock()
	switch r.URL.Path {
	case "/claim":
		if e, ok := m.entries[key]; ok && e.state == "running" && e.holder != inst {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"claimed": false, "holder": e.holder, "state": "running", "ts": time.Now().Unix(),
			})
			return
		}
		if e, ok := m.entries[key]; ok && e.state == "done" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"claimed": false, "holder": e.holder, "state": "done", "ts": time.Now().Unix(),
			})
			return
		}
		m.entries[key] = &mockEntry{holder: inst, state: "running"}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"claimed": true, "date": m.date, "ts": time.Now().Unix(),
		})
	case "/finish":
		e, ok := m.entries[key]
		if !ok || e.holder != inst {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "not-holder"})
			return
		}
		if okFlag, _ := body["ok"].(bool); okFlag {
			e.state = "done"
		} else {
			delete(m.entries, key)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case "/release":
		e, ok := m.entries[key]
		if !ok || e.holder != inst {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "not-holder"})
			return
		}
		delete(m.entries, key)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestClaimConflict(t *testing.T) {
	srv := httptest.NewServer(newMockLedger("tok-123"))
	defer srv.Close()
	ctx := context.Background()

	app := New(srv.URL, "tok-123", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	claimed, _, err := app.Claim(ctx, "checkin", "u1")
	if err != nil || !claimed {
		t.Fatalf("app first claim: claimed=%v err=%v", claimed, err)
	}

	// 另一实例同 (task,uid) 抢锁 → claimed=false，holder 是 app-ios。
	jb := New(srv.URL, "tok-123", "jb-iphone12", filepath.Join(t.TempDir(), "ledger-local.json"))
	claimed, heldBy, err := jb.Claim(ctx, "checkin", "u1")
	if err != nil {
		t.Fatalf("jb claim err=%v", err)
	}
	if claimed || heldBy != "app-ios" {
		t.Fatalf("jb claim: claimed=%v heldBy=%q want false/app-ios", claimed, heldBy)
	}

	// 不同 uid 不受影响。
	claimed, _, err = jb.Claim(ctx, "checkin", "u2")
	if err != nil || !claimed {
		t.Fatalf("jb claim u2: claimed=%v err=%v", claimed, err)
	}
}

func TestFinishNotHolder(t *testing.T) {
	srv := httptest.NewServer(newMockLedger("tok-123"))
	defer srv.Close()
	ctx := context.Background()

	app := New(srv.URL, "tok-123", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	jb := New(srv.URL, "tok-123", "jb-iphone12", filepath.Join(t.TempDir(), "ledger-local.json"))
	if claimed, _, err := app.Claim(ctx, "checkin", "u1"); err != nil || !claimed {
		t.Fatalf("app claim: claimed=%v err=%v", claimed, err)
	}
	// 非持有者写终态 → 拒绝。
	if err := jb.Finish(ctx, "checkin", "u1", true, "done"); err == nil || !strings.Contains(err.Error(), "not-holder") {
		t.Fatalf("jb finish: err=%v want not-holder", err)
	}
	// 持有者写终态成功；此后同 (task,uid) 再抢 → done 永久拒。
	if err := app.Finish(ctx, "checkin", "u1", true, "+30"); err != nil {
		t.Fatalf("app finish: err=%v", err)
	}
	claimed, heldBy, err := jb.Claim(ctx, "checkin", "u1")
	if err != nil || claimed || heldBy != "app-ios" {
		t.Fatalf("jb claim after done: claimed=%v heldBy=%q err=%v", claimed, heldBy, err)
	}
}

func TestRelease(t *testing.T) {
	srv := httptest.NewServer(newMockLedger("tok-123"))
	defer srv.Close()
	ctx := context.Background()

	app := New(srv.URL, "tok-123", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	jb := New(srv.URL, "tok-123", "jb-iphone12", filepath.Join(t.TempDir(), "ledger-local.json"))
	if claimed, _, err := app.Claim(ctx, "checkin", "u1"); err != nil || !claimed {
		t.Fatalf("app claim: claimed=%v err=%v", claimed, err)
	}
	// 非持有者释放 → 拒绝。
	if err := jb.Release(ctx, "checkin", "u1"); err == nil || !strings.Contains(err.Error(), "not-holder") {
		t.Fatalf("jb release: err=%v want not-holder", err)
	}
	// 持有者释放后，对方可以抢到。
	if err := app.Release(ctx, "checkin", "u1"); err != nil {
		t.Fatalf("app release: err=%v", err)
	}
	claimed, _, err := jb.Claim(ctx, "checkin", "u1")
	if err != nil || !claimed {
		t.Fatalf("jb claim after release: claimed=%v err=%v", claimed, err)
	}
}

func TestClaimNoToken401(t *testing.T) {
	srv := httptest.NewServer(newMockLedger("tok-123"))
	defer srv.Close()
	ctx := context.Background()

	// 无 token → 401，Claim 返回错误（调用方走降级，不阻塞）。
	anon := New(srv.URL, "", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	if _, _, err := anon.Claim(ctx, "checkin", "u1"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("anon claim: err=%v want 401", err)
	}
	// /health 免认证，照常返回服务端日期。
	date, err := anon.Health(ctx)
	if err != nil || date != "2026-11-10" {
		t.Fatalf("health: date=%q err=%v", date, err)
	}
}

func TestClaimTimeoutDegrades(t *testing.T) {
	// 服务端 200ms 才回包，客户端超时 50ms → Claim 返回错误（调用方降级执行）。
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"claimed": true, "date": "2026-11-10"})
	}))
	defer slow.Close()

	c := New(slow.URL, "tok-123", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	c.httpClient = &http.Client{Timeout: 50 * time.Millisecond}
	if _, _, err := c.Claim(context.Background(), "checkin", "u1"); err == nil {
		t.Fatalf("slow claim: want timeout error, got nil")
	}
	if c.Reachable() {
		t.Fatalf("Reachable()=true after failed claim, want false")
	}
}

func TestReachableAfterSuccess(t *testing.T) {
	srv := httptest.NewServer(newMockLedger("tok-123"))
	defer srv.Close()
	c := New(srv.URL, "tok-123", "app-ios", filepath.Join(t.TempDir(), "ledger-local.json"))
	if c.Reachable() {
		t.Fatalf("Reachable()=true before any contact, want false")
	}
	if claimed, _, err := c.Claim(context.Background(), "checkin", "u1"); err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
	if !c.Reachable() {
		t.Fatalf("Reachable()=false after successful claim, want true")
	}
	if got := c.ServerDate(); got != "2026-11-10" {
		t.Fatalf("ServerDate()=%q want 2026-11-10", got)
	}
}

func TestLocalDedup(t *testing.T) {
	dir := t.TempDir()
	c := New("", "", "app-ios", filepath.Join(dir, "ledger-local.json"))

	// 首次认领通过。
	run, date := c.LocalClaim("checkin", "u1")
	if !run || date == "" {
		t.Fatalf("first LocalClaim: run=%v date=%q", run, date)
	}
	// 同天同 (task,uid) 再次认领 → 只有任务失败（ok=false）才允许重试；
	// 先把首次标记为成功，再认领应被拦。
	c.LocalFinish(date, "checkin", "u1", true)
	if run, _ := c.LocalClaim("checkin", "u1"); run {
		t.Fatalf("LocalClaim after finish(ok=true): want skip, got run")
	}
	// 不同任务 / 不同账号不受影响。
	if run, _ := c.LocalClaim("travel", "u1"); !run {
		t.Fatalf("LocalClaim travel/u1: want run")
	}
	if run, _ := c.LocalClaim("checkin", "u2"); !run {
		t.Fatalf("LocalClaim checkin/u2: want run")
	}

	// 任务失败（ok=false）→ 下一时点可重试。
	run, date2 := c.LocalClaim("keepalive", "u1")
	if !run {
		t.Fatalf("LocalClaim keepalive/u1: want run")
	}
	c.LocalFinish(date2, "keepalive", "u1", false)
	if run, _ := c.LocalClaim("keepalive", "u1"); !run {
		t.Fatalf("LocalClaim after finish(ok=false): want retry, got skip")
	}

	// 落盘文件结构：{ "<date>": { "<task>:<uid>": {"ts":...,"ok":...} } }。
	raw, err := os.ReadFile(filepath.Join(dir, "ledger-local.json"))
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	var d map[string]map[string]struct {
		TS int64 `json:"ts"`
		OK bool  `json:"ok"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("local file not json: %v", err)
	}
	day, ok := d[date]
	if !ok {
		t.Fatalf("local file missing date %q: %v", date, d)
	}
	if e, ok := day["checkin:u1"]; !ok || !e.OK {
		t.Fatalf("local file checkin:u1=%v want ok=true", e)
	}
}

func TestLocalDedupNoPath(t *testing.T) {
	// localPath 为空目录不存在时，LocalClaim 不应崩（写失败只记日志，仍放行）。
	c := New("", "", "app-ios", filepath.Join(t.TempDir(), "no-such-dir", "ledger-local.json"))
	if run, _ := c.LocalClaim("checkin", "u1"); !run {
		t.Fatalf("LocalClaim with unwritable path: want run")
	}
}
