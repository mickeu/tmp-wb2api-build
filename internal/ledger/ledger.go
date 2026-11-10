// SPDX-FileCopyrightText: 2026 yreidev 与贡献者（见 NOTICE）
// SPDX-License-Identifier: AGPL-3.0-only
// 复制或改写本文件须保留以上声明，并按 NOTICE 的署名条款注明出处：https://github.com/yreidev/workbuddy2api-panel

// Package ledger 任务台账客户端：与越狱机台账服务（:17866）通信，跨实例互检定时任务。
//
// 严守一条原则：台账不可用时降级为「本地去重 + 错峰」，绝不阻塞任务执行。
//
// 日期基准：服务端认领的日期唯一信任服务端返回的 `date`（服务端固定 CST+8，
// 客户端不算、不送本地日期）；本地去重文件（降级路径）才用本地时间（§2 修好后
// time.Local 为 CST+8）。
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// reachableWindow 台账「可达」判定窗口：最近一次认领/写终态成功在此窗口内，
// 错峰判定才认为台账可达。进程刚启动（从未成功联系）→ 不可达（安全默认：错峰）。
const reachableWindow = 10 * time.Minute

// claimTTL 认领锁的服务端 TTL（秒）：任务执行超时未写终态，锁过期后对方可接手。
const claimTTL = 600

// Client 台账客户端。
//
// baseURL 由 config.ledger.base_url 给定，形如 http://100.87.122.27:17866。
// 鉴权：Authorization: Bearer <config.ledger.token>。
// localPath 是降级用的本地去重文件（data/ledger-local.json），由 main 按
// state_file 所在目录传入——绝不用 os.Getwd/UserHomeDir 拼（沙盒里两者都不可靠）。
type Client struct {
	baseURL    string
	token      string
	instanceID string
	httpClient *http.Client // Timeout: 3s（台账是辅助设施，不能拖慢任务）
	localPath  string

	mu         sync.Mutex
	lastOK     time.Time // 最近一次认领/写终态成功（2xx）的时间
	serverDate string    // 最近一次服务端返回的 date（台账日期基准）
}

// New 构造台账客户端。baseURL 为空 = 未配置台账（Claim 一律返回错误，
// 调用方走本地去重；本地文件照常可用）。
func New(baseURL, token, instanceID, localPath string) *Client {
	return &Client{
		baseURL:    baseURL,
		token:      token,
		instanceID: instanceID,
		httpClient: &http.Client{Timeout: 3 * time.Second},
		localPath:  localPath,
	}
}

// Reachable 台账最近是否可协调（最近一次认领/写终态成功在窗口内）。
// 只认 2xx：401/403 证明服务在但本实例无法认领，照样算不可达（错峰 + 本地去重）。
func (c *Client) Reachable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.lastOK.IsZero() && time.Since(c.lastOK) <= reachableWindow
}

// ServerDate 最近一次服务端返回的 date（"" = 尚未成功联系过服务端）。
func (c *Client) ServerDate() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverDate
}

// markOK 记录一次成功往返（2xx）：更新 lastOK 与服务端日期。
func (c *Client) markOK(serverDate string) {
	c.mu.Lock()
	c.lastOK = time.Now()
	if serverDate != "" {
		c.serverDate = serverDate
	}
	c.mu.Unlock()
}

// Claim 尝试认领 (task, uid)。三态返回：
//
//	claimed=true            → 本实例获得执行权，调用方继续跑
//	claimed=false, heldBy=X → 他实例已领或已完成，本次跳过
//	err!=nil                → 台账不可达，调用方降级执行（本地去重 + 错峰）
func (c *Client) Claim(ctx context.Context, task, uid string) (claimed bool, heldBy string, err error) {
	if c.baseURL == "" {
		return false, "", errors.New("ledger: base_url 未配置")
	}
	var resp struct {
		Claimed bool   `json:"claimed"`
		Holder  string `json:"holder"`
		State   string `json:"state"`
		Date    string `json:"date"`
		TS      int64  `json:"ts"`
	}
	if err := c.post(ctx, "/claim", map[string]any{
		"task": task, "uid": uid, "instance": c.instanceID, "ttl": claimTTL,
	}, &resp); err != nil {
		return false, "", err
	}
	c.markOK(resp.Date)
	if resp.Claimed {
		return true, "", nil
	}
	return false, resp.Holder, nil
}

// Finish 写终态。只在本实例 Claim 成功后调用；失败不阻塞主流程。
// ok=true 表示任务完成；ok=false 表示任务未完成（服务端按终态处理）。
func (c *Client) Finish(ctx context.Context, task, uid string, ok bool, detail string) error {
	if c.baseURL == "" {
		return errors.New("ledger: base_url 未配置")
	}
	var resp struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	if err := c.post(ctx, "/finish", map[string]any{
		"task": task, "uid": uid, "instance": c.instanceID, "ok": ok, "detail": detail,
	}, &resp); err != nil {
		return err
	}
	c.markOK("")
	if !resp.OK {
		return fmt.Errorf("ledger: finish 被拒：%s", resp.Reason)
	}
	return nil
}

// Release 释放锁。任务执行失败时调用，让另一方有机会重试。
func (c *Client) Release(ctx context.Context, task, uid string) error {
	if c.baseURL == "" {
		return errors.New("ledger: base_url 未配置")
	}
	var resp struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	if err := c.post(ctx, "/release", map[string]any{
		"task": task, "uid": uid, "instance": c.instanceID,
	}, &resp); err != nil {
		return err
	}
	c.markOK("")
	if !resp.OK {
		return fmt.Errorf("ledger: release 被拒：%s", resp.Reason)
	}
	return nil
}

// Health 探活（GET /health，免认证）。返回服务端日期；只用于联调/诊断，
// 不计入 Reachable（免认证探活只能证明服务在，不能证明本实例能认领）。
func (c *Client) Health(ctx context.Context) (string, error) {
	if c.baseURL == "" {
		return "", errors.New("ledger: base_url 未配置")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("ledger: GET /health: %s", resp.Status)
	}
	var out struct {
		OK   bool   `json:"ok"`
		Date string `json:"date"`
		TZ   string `json:"tz"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("ledger: GET /health: 解析响应失败: %w", err)
	}
	if !out.OK {
		return "", errors.New("ledger: GET /health: ok=false")
	}
	c.mu.Lock()
	c.serverDate = out.Date
	c.mu.Unlock()
	return out.Date, nil
}

// post 向台账服务发一个带鉴权的 JSON POST，响应解到 out（非 2xx 按状态码返回错误）。
// 401/403 单独说明（token 无效/非持有者），便于调用方从日志区分「服务挂了」与「配错了」。
func (c *Client) post(ctx context.Context, path string, reqBody any, out any) error {
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("ledger: POST %s: 401（token 无效或缺失）", path)
	case http.StatusForbidden:
		return fmt.Errorf("ledger: POST %s: 403（非持有者）", path)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ledger: POST %s: %s", path, resp.Status)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("ledger: POST %s: 解析响应失败: %w", path, err)
		}
	}
	return nil
}

// ---- 本地去重（降级路径）----

// localEntry 本地去重文件里的一条记录。
type localEntry struct {
	TS int64 `json:"ts"` // Unix 秒
	OK bool  `json:"ok"` // true = 当天已完成
}

// localData 本地去重文件结构：{ "<date>": { "<task>:<uid>": {"ts":...,"ok":...} } }。
type localData map[string]map[string]localEntry

// localDate 本地去重用的日期：本地时区（§2 修好后 time.Local 为 CST+8）。
func localDate() string {
	return time.Now().Format("2006-01-02")
}

// localLoad 读本地去重文件（不存在/损坏 → 空；损坏时记一行日志，不阻塞任务）。
func (c *Client) localLoad() localData {
	raw, err := os.ReadFile(c.localPath)
	if err != nil {
		return localData{}
	}
	var d localData
	if err := json.Unmarshal(raw, &d); err != nil {
		log.Printf("ledger: 本地去重文件损坏（%s），按空处理", c.localPath)
		return localData{}
	}
	if d == nil {
		return localData{}
	}
	return d
}

// localSave 原子写本地去重文件（写临时文件 + 重命名；只保留当天日期，文件有界）。
func (c *Client) localSave(d localData) {
	today := localDate()
	for date := range d {
		if date != today {
			delete(d, date)
		}
	}
	raw, err := json.Marshal(d)
	if err != nil {
		log.Printf("ledger: 本地去重文件编码失败：%v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.localPath), 0o755); err != nil {
		log.Printf("ledger: 本地去重目录创建失败：%v", err)
		return
	}
	tmp := c.localPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		log.Printf("ledger: 本地去重文件写失败：%v", err)
		return
	}
	if err := os.Rename(tmp, c.localPath); err != nil {
		log.Printf("ledger: 本地去重文件重命名失败：%v", err)
	}
}

// LocalClaim 本地去重认领：同一 (date, task, uid) 当天已完成则返回 false（跳过），
// 否则记录一条「已认领」（ok=false）并返回 true。
// 返回 (shouldRun, date)：date 是本次认领用的本地日期，调用方在任务结束后用
// 同一 date 调 LocalFinish 写终态（跨午夜也归属到认领那一天）。
func (c *Client) LocalClaim(task, uid string) (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	date := localDate()
	d := c.localLoad()
	day, ok := d[date]
	if !ok {
		day = map[string]localEntry{}
		d[date] = day
	}
	key := task + ":" + uid
	if e, exists := day[key]; exists && e.OK {
		return false, date
	}
	day[key] = localEntry{TS: time.Now().Unix(), OK: false}
	c.localSave(d)
	return true, date
}

// LocalFinish 本地去重写终态：把 date 那天的 (task, uid) 标记为 ok（成功）
// 或保持未完成（失败 → 下一时点可重试）。date 用 LocalClaim 返回的同一天。
func (c *Client) LocalFinish(date, task, uid string, ok bool) {
	if date == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.localLoad()
	day, exists := d[date]
	if !exists {
		day = map[string]localEntry{}
		d[date] = day
	}
	day[task+":"+uid] = localEntry{TS: time.Now().Unix(), OK: ok}
	c.localSave(d)
}
