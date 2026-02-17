// Package monitor 提供 Agent 巡检 (对应 Python agent_monitor.py 392 行)。
//
// Go 优势: goroutine + ticker 替代 Python while+sleep、
// strings.Contains 替代关键词检测、sync.Map 替代 dict。
package monitor

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ========================================
// 常量 (对应 Python 模块级常量)
// ========================================

var (
	errorKeywords        = []string{"traceback", "error", "exception"}
	disconnectedKeywords = []string{"timeout", "connection refused", "econnreset"}
	promptMarkers        = []string{"$", "#", ">>>", "...", ">"}
)

const (
	defaultStuckSec    = 60
	defaultIntervalSec = 5
)

// StatusNames 有效状态名。
var StatusNames = []string{"running", "idle", "stuck", "error", "disconnected", "unknown"}

// ========================================
// Patrol 巡检器
// ========================================

// Patrol Agent 巡检器。
type Patrol struct {
	agentStore *store.AgentStatusStore
	eventBus   EventPublisher

	mu     sync.Mutex
	memory map[string]*fingerprint // 输出指纹缓存
}

// EventPublisher 事件发布接口 (DRY: 解耦 SSE 总线)。
type EventPublisher interface {
	PublishAgentStatus(snapshot map[string]any)
}

type fingerprint struct {
	hash         string
	lastChangeAt time.Time
}

// NewPatrol 创建巡检器。
func NewPatrol(as *store.AgentStatusStore, bus EventPublisher) *Patrol {
	return &Patrol{
		agentStore: as,
		eventBus:   bus,
		memory:     make(map[string]*fingerprint),
	}
}

// ========================================
// ClassifyStatus — 状态分类 (对应 Python classify_status)
// ========================================

// ClassifyStatus 根据输出片段分类 Agent 状态。
func ClassifyStatus(lines []string, hasSession bool, stagnantSec int) string {
	if !hasSession {
		return "unknown"
	}

	normalized := normalizeLines(lines)
	if isPromptOnly(normalized) {
		return "idle"
	}

	merged := strings.ToLower(strings.Join(normalized, "\n"))

	if containsAny(merged, errorKeywords) {
		return "error"
	}
	if containsAny(merged, disconnectedKeywords) {
		return "disconnected"
	}
	if stagnantSec >= defaultStuckSec {
		return "stuck"
	}
	return "running"
}

// ========================================
// RunOnce — 单次巡检 (对应 Python patrol_agents_once)
// ========================================

// AgentSnapshot 单个 Agent 巡检快照。
type AgentSnapshot struct {
	AgentID     string   `json:"agent_id"`
	AgentName   string   `json:"agent_name"`
	SessionID   string   `json:"session_id"`
	Status      string   `json:"status"`
	StagnantSec int      `json:"stagnant_sec"`
	Error       string   `json:"error"`
	OutputTail  []string `json:"output_tail"`
}

// PatrolResult 巡检结果。
type PatrolResult struct {
	OK      bool            `json:"ok"`
	Ts      time.Time       `json:"ts"`
	Summary map[string]int  `json:"summary"`
	Agents  []AgentSnapshot `json:"agents"`
	Error   string          `json:"error,omitempty"`
}

// RunOnce 执行一次巡检周期 + 持久化 + SSE 推送。
func (p *Patrol) RunOnce(ctx context.Context) *PatrolResult {
	now := time.Now()
	agents, err := p.agentStore.List(ctx, "")
	if err != nil {
		logger.Errorw("patrol: list agents failed", "error", err)
		return &PatrolResult{OK: false, Ts: now, Error: err.Error(), Summary: emptySummary()}
	}

	var snapshots []AgentSnapshot
	for _, a := range agents {
		// 计算 stagnation
		stagnant := p.computeStagnant(a.AgentID, a.OutputTail, now)
		status := ClassifyStatus(parseOutputTail(a.OutputTail), a.SessionID != "", stagnant)
		if status != "error" && status != "disconnected" && a.Error != "" {
			status = "disconnected"
		}

		snap := AgentSnapshot{
			AgentID:     a.AgentID,
			AgentName:   a.AgentName,
			SessionID:   a.SessionID,
			Status:      status,
			StagnantSec: stagnant,
			Error:       a.Error,
			OutputTail:  parseOutputTail(a.OutputTail),
		}
		snapshots = append(snapshots, snap)

		// 持久化更新后的状态
		a.Status = status
		a.StagnantSec = stagnant
		if _, err := p.agentStore.Upsert(ctx, &a); err != nil {
			logger.Debugw("patrol: upsert failed", "agent_id", a.AgentID, "error", err)
		}
	}

	result := &PatrolResult{
		OK:      true,
		Ts:      now,
		Summary: summarize(snapshots),
		Agents:  snapshots,
	}

	// SSE 推送
	if p.eventBus != nil {
		p.eventBus.PublishAgentStatus(map[string]any{
			"ok":      result.OK,
			"ts":      result.Ts,
			"summary": result.Summary,
			"agents":  result.Agents,
		})
	}

	return result
}

// ========================================
// Start — 定期巡检 (对应 Python patrol_agents_loop)
// ========================================

// Start 启动定期巡检 (goroutine + ticker)。
func (p *Patrol) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Duration(defaultIntervalSec) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.RunOnce(ctx)
			}
		}
	}()
	logger.Infow("patrol started", "interval_sec", defaultIntervalSec)
}

// ========================================
// 内部工具 (DRY: 共享逻辑)
// ========================================

// computeStagnant 计算输出停滞时间 (指纹对比)。
func (p *Patrol) computeStagnant(agentID string, outputTail any, now time.Time) int {
	hash := hashOutput(outputTail)

	p.mu.Lock()
	defer p.mu.Unlock()

	prev, ok := p.memory[agentID]
	if !ok || prev.hash != hash {
		p.memory[agentID] = &fingerprint{hash: hash, lastChangeAt: now}
		return 0
	}
	return int(now.Sub(prev.lastChangeAt).Seconds())
}

func hashOutput(v any) string {
	lines := parseOutputTail(v)
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}

func parseOutputTail(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case string:
		if val == "" {
			return nil
		}
		return strings.Split(val, "\n")
	case []any:
		var out []string
		for _, item := range val {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func normalizeLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func isPromptOnly(lines []string) bool {
	if len(lines) == 0 {
		return true
	}
	for _, l := range lines {
		found := false
		for _, m := range promptMarkers {
			if l == m {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func emptySummary() map[string]int {
	m := map[string]int{"total": 0, "healthy": 0, "unhealthy": 0}
	for _, name := range StatusNames {
		m[name] = 0
	}
	return m
}

func summarize(agents []AgentSnapshot) map[string]int {
	s := emptySummary()
	for _, a := range agents {
		status := a.Status
		s[status]++
		s["total"]++
	}
	s["healthy"] = s["running"] + s["idle"]
	s["unhealthy"] = s["total"] - s["healthy"]
	return s
}
