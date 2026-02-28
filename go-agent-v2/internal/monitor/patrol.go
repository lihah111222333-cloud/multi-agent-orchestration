package monitor

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

var (
	errorKeywords        = []string{"traceback", "error", "exception"}
	disconnectedKeywords = []string{"timeout", "connection refused", "econnreset"}
	promptMarkers        = []string{"$", "#", ">>>", "...", ">"}
)

const (
	defaultStuckSec    = 60
	defaultIntervalSec = 5
)

var StatusNames = []string{"running", "idle", "stuck", "error", "disconnected", "unknown"}

type Patrol struct {
	agentStore *store.AgentStatusStore
	eventBus   EventPublisher

	mu     sync.Mutex
	memory map[string]*fingerprint
}

type EventPublisher interface {
	PublishAgentStatus(snapshot map[string]any)
}

type fingerprint struct {
	hash         string
	lastChangeAt time.Time
}

func NewPatrol(as *store.AgentStatusStore, bus EventPublisher) *Patrol {
	return &Patrol{
		agentStore: as,
		eventBus:   bus,
		memory:     make(map[string]*fingerprint),
	}
}

func ClassifyStatus(lines []string, hasSession bool, stagnantSec int) string {
	if !hasSession {
		return "unknown"
	}

	normalized := make([]string, 0, len(lines))
	promptOnly := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
		if promptOnly && !slices.Contains(promptMarkers, trimmed) {
			promptOnly = false
		}
	}
	if len(normalized) == 0 || promptOnly {
		return "idle"
	}

	merged := strings.ToLower(strings.Join(normalized, "\n"))
	if slices.ContainsFunc(errorKeywords, func(kw string) bool { return strings.Contains(merged, kw) }) {
		return "error"
	}
	if slices.ContainsFunc(disconnectedKeywords, func(kw string) bool { return strings.Contains(merged, kw) }) {
		return "disconnected"
	}
	if stagnantSec >= defaultStuckSec {
		return "stuck"
	}
	return "running"
}

type AgentSnapshot struct {
	AgentID     string   `json:"agent_id"`
	AgentName   string   `json:"agent_name"`
	SessionID   string   `json:"session_id"`
	Status      string   `json:"status"`
	StagnantSec int      `json:"stagnant_sec"`
	Error       string   `json:"error"`
	OutputTail  []string `json:"output_tail"`
}

type PatrolResult struct {
	OK      bool            `json:"ok"`
	Ts      time.Time       `json:"ts"`
	Summary map[string]int  `json:"summary"`
	Agents  []AgentSnapshot `json:"agents"`
	Error   string          `json:"error,omitempty"`
}

func (p *Patrol) RunOnce(ctx context.Context) *PatrolResult {
	now := time.Now()
	agents, err := p.agentStore.List(ctx, "")
	if err != nil {
		logger.Error("patrol: list agents failed", logger.FieldError, err)
		return &PatrolResult{OK: false, Ts: now, Error: err.Error(), Summary: emptySummary()}
	}

	var snapshots []AgentSnapshot
	for _, a := range agents {
		lines := parseOutputTail(a.OutputTail)
		stagnant := p.computeStagnantFromLines(a.AgentID, lines, now)
		status := ClassifyStatus(lines, a.SessionID != "", stagnant)
		if status != "error" && status != "disconnected" && a.Error != "" {
			status = "disconnected"
		}

		snapshots = append(snapshots, AgentSnapshot{
			AgentID:     a.AgentID,
			AgentName:   a.AgentName,
			SessionID:   a.SessionID,
			Status:      status,
			StagnantSec: stagnant,
			Error:       a.Error,
			OutputTail:  lines,
		})

		a.Status = status
		a.StagnantSec = stagnant
		if _, err := p.agentStore.Upsert(ctx, &a); err != nil {
			logger.Warn("patrol: upsert failed", logger.FieldAgentID, a.AgentID, logger.FieldError, err)
		}
	}

	result := &PatrolResult{
		OK:      true,
		Ts:      now,
		Summary: summarize(snapshots),
		Agents:  snapshots,
	}

	activeIDs := make(map[string]struct{}, len(agents))
	for _, a := range agents {
		activeIDs[a.AgentID] = struct{}{}
	}
	p.mu.Lock()
	for id := range p.memory {
		if _, alive := activeIDs[id]; !alive {
			delete(p.memory, id)
		}
	}
	p.mu.Unlock()

	logger.Debug("patrol: cycle complete",
		logger.FieldCount, len(snapshots),
		"unhealthy", result.Summary["unhealthy"],
	)

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

func (p *Patrol) Start(ctx context.Context) {
	util.SafeGo(func() {
		ticker := time.NewTicker(defaultIntervalSec * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("patrol: stopped (context cancelled)")
				return
			case <-ticker.C:
				p.RunOnce(ctx)
			}
		}
	})
	logger.Info("patrol started", "interval_sec", defaultIntervalSec)
}

func (p *Patrol) computeStagnantFromLines(agentID string, lines []string, now time.Time) int {
	hash := hashLines(lines)

	p.mu.Lock()
	defer p.mu.Unlock()

	prev, ok := p.memory[agentID]
	if !ok || prev.hash != hash {
		p.memory[agentID] = &fingerprint{hash: hash, lastChangeAt: now}
		return 0
	}
	return int(now.Sub(prev.lastChangeAt).Seconds())
}

func hashLines(lines []string) string {
	tail := lines
	if len(tail) > 6 {
		tail = tail[len(tail)-6:]
	}
	return strings.Join(tail, "\n")
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
		s[a.Status]++
	}
	s["total"] = len(agents)
	s["healthy"] = s["running"] + s["idle"]
	s["unhealthy"] = s["total"] - s["healthy"]
	return s
}
