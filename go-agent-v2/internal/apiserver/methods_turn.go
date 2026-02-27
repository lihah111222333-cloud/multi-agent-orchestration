package apiserver

import (
	"context"
	"encoding/json"
	goruntime "runtime"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// UserInput 用户输入 (支持多种类型)。
type UserInput struct {
	Type    string `json:"type"`              // text, image, localImage, skill, mention, fileContent
	Text    string `json:"text,omitempty"`    // type=text
	URL     string `json:"url,omitempty"`     // type=image
	Path    string `json:"path,omitempty"`    // type=localImage/mention/fileContent
	Name    string `json:"name,omitempty"`    // type=skill/mention
	Content string `json:"content,omitempty"` // type=skill/fileContent
}

type turnStartParams struct {
	ThreadID             string          `json:"threadId"`
	Input                []UserInput     `json:"input"`
	SelectedSkills       []string        `json:"selectedSkills,omitempty"`
	ManualSkillSelection bool            `json:"manualSkillSelection,omitempty"`
	Cwd                  string          `json:"cwd,omitempty"`
	ApprovalPolicy       string          `json:"approvalPolicy,omitempty"`
	Model                string          `json:"model,omitempty"`
	OutputSchema         json.RawMessage `json:"outputSchema,omitempty"`
}

// turnInfo 通用 turn 信息。
type turnInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// turnStartResponse turn/start 响应。
type turnStartResponse struct {
	Turn turnInfo `json:"turn"`
}

func toCodexTurnInputs(input []UserInput) []contracts.TurnInput {
	if len(input) == 0 {
		return nil
	}
	out := make([]contracts.TurnInput, 0, len(input))
	for _, item := range input {
		out = append(out, contracts.TurnInput{
			Type:    item.Type,
			Text:    item.Text,
			URL:     item.URL,
			Path:    item.Path,
			Name:    item.Name,
			Content: item.Content,
		})
	}
	return out
}

func (s *Server) turnStartTyped(ctx context.Context, p turnStartParams) (any, error) {
	startResult, err := s.codexAdapter.TurnStart(ctx, contracts.TurnStartRequest{
		ThreadID:             p.ThreadID,
		Cwd:                  p.Cwd,
		Input:                toCodexTurnInputs(p.Input),
		SelectedSkills:       p.SelectedSkills,
		ManualSkillSelection: p.ManualSkillSelection,
		OutputSchema:         p.OutputSchema,
	})
	if err != nil {
		return nil, err
	}

	return turnStartResponse{
		Turn: turnInfo{ID: startResult.TurnID, Status: "inProgress"},
	}, nil
}

type turnSteerParams struct {
	ThreadID             string      `json:"threadId"`
	ExpectedTurnID       string      `json:"expectedTurnId"`
	Input                []UserInput `json:"input"`
	SelectedSkills       []string    `json:"selectedSkills,omitempty"`
	ManualSkillSelection bool        `json:"manualSkillSelection,omitempty"`
}

func (s *Server) turnSteerTyped(_ context.Context, p turnSteerParams) (any, error) {
	return s.codexAdapter.TurnSteerFromInputAligned(contracts.TurnSteerRequest{
		ThreadID:             p.ThreadID,
		ExpectedTurnID:       p.ExpectedTurnID,
		Input:                toCodexTurnInputs(p.Input),
		SelectedSkills:       p.SelectedSkills,
		ManualSkillSelection: p.ManualSkillSelection,
	})
}

type turnInterruptParams struct {
	ThreadID string `json:"threadId"`
}

func (s *Server) turnInterrupt(_ context.Context, p turnInterruptParams) (any, error) {
	return s.codexAdapter.TurnInterrupt(p.ThreadID)
}

type turnForceCompleteParams struct {
	ThreadID string `json:"threadId"`
}

// turnForceComplete 强制完成当前 turn (中断 + 清理跟踪状态)。
func (s *Server) turnForceComplete(_ context.Context, p turnForceCompleteParams) (any, error) {
	return s.codexAdapter.TurnForceComplete(p.ThreadID)
}

type threadRealtimeStartParams struct {
	ThreadID  string  `json:"threadId"`
	Prompt    string  `json:"prompt"`
	SessionID *string `json:"sessionId,omitempty"`
}

type threadRealtimeAppendAudioParams struct {
	ThreadID string `json:"threadId"`
	Audio    any    `json:"audio"`
}

type threadRealtimeAppendTextParams struct {
	ThreadID string `json:"threadId"`
	Text     string `json:"text"`
}

type threadRealtimeStopParams struct {
	ThreadID string `json:"threadId"`
}

// reviewStartParams review/start 请求参数。
type reviewTarget struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions,omitempty"`
	Branch       string `json:"branch,omitempty"`
	Sha          string `json:"sha,omitempty"`
}

type reviewStartParams struct {
	ThreadID string       `json:"threadId"`
	Target   reviewTarget `json:"target"`
	Delivery string       `json:"delivery,omitempty"`
}

type reviewStartTurn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Items  []any  `json:"items"`
}

type reviewStartResponse struct {
	Turn           reviewStartTurn `json:"turn"`
	ReviewThreadID string          `json:"reviewThreadId"`
}

func buildReviewStartArgs(p reviewStartParams) (string, error) {
	targetType := strings.TrimSpace(p.Target.Type)
	if targetType == "" {
		return "", pkgerr.New("Server.reviewStart", "target.type is required")
	}
	switch targetType {
	case "custom":
		instructions := strings.TrimSpace(p.Target.Instructions)
		if instructions == "" {
			return "", pkgerr.New("Server.reviewStart", "target.instructions is required when target.type is custom")
		}
		return instructions, nil
	case "baseBranch":
		branch := strings.TrimSpace(p.Target.Branch)
		if branch == "" {
			return "", pkgerr.New("Server.reviewStart", "target.branch is required when target.type is baseBranch")
		}
		return branch, nil
	case "commit":
		sha := strings.TrimSpace(p.Target.Sha)
		if sha == "" {
			return "", pkgerr.New("Server.reviewStart", "target.sha is required when target.type is commit")
		}
		return sha, nil
	case "uncommittedChanges":
		return strings.TrimSpace(p.Delivery), nil
	default:
		return "", pkgerr.New("Server.reviewStart", "target.type must be one of: uncommittedChanges, baseBranch, commit, custom")
	}
}

func validateReviewStartParams(p reviewStartParams) (string, error) {
	if strings.TrimSpace(p.ThreadID) == "" {
		return "", pkgerr.New("Server.reviewStart", "threadId is required")
	}
	return buildReviewStartArgs(p)
}

func normalizeReviewStartResponse(threadID string, result map[string]any) reviewStartResponse {
	response := reviewStartResponse{
		Turn: reviewStartTurn{
			ID:     "",
			Status: "inProgress",
			Items:  []any{},
		},
		ReviewThreadID: threadID,
	}
	if result == nil {
		return response
	}
	if reviewThreadID, ok := result["reviewThreadId"].(string); ok {
		reviewThreadID = strings.TrimSpace(reviewThreadID)
		if reviewThreadID != "" {
			response.ReviewThreadID = reviewThreadID
		}
	}
	if turnMap, ok := result["turn"].(map[string]any); ok {
		if id, ok := turnMap["id"].(string); ok {
			response.Turn.ID = id
		}
		if status, ok := turnMap["status"].(string); ok {
			status = strings.TrimSpace(status)
			if status != "" {
				response.Turn.Status = status
			}
		}
		if items, ok := turnMap["items"].([]any); ok {
			response.Turn.Items = items
		}
	}
	return response
}

func (s *Server) reviewStartTyped(_ context.Context, p reviewStartParams) (any, error) {
	args, err := validateReviewStartParams(p)
	if err != nil {
		return nil, err
	}
	result, err := s.codexAdapter.ReviewStart(p.ThreadID, args)
	if err != nil {
		return nil, err
	}
	return normalizeReviewStartResponse(p.ThreadID, result), nil
}

type fuzzySearchParams struct {
	Query string   `json:"query"`
	Roots []string `json:"roots"`
}

func fuzzyFileSearchTyped(s *Server, _ context.Context, p fuzzySearchParams) (any, error) {
	if s == nil || s.codexAdapter == nil {
		return map[string]any{"files": []map[string]any{}}, nil
	}
	results := s.codexAdapter.FuzzyFileSearch(p.Query, p.Roots, commonadapter.FuzzyMatch)
	return map[string]any{"files": results}, nil
}
func debugRuntime(s *Server, _ context.Context, _ json.RawMessage) (any, error) {
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)

	result := map[string]any{
		"go": map[string]any{
			"goroutines":     goruntime.NumGoroutine(),
			"heapAllocMB":    float64(mem.HeapAlloc) / 1024 / 1024,
			"heapSysMB":      float64(mem.HeapSys) / 1024 / 1024,
			"heapInuseMB":    float64(mem.HeapInuse) / 1024 / 1024,
			"heapObjects":    mem.HeapObjects,
			"sysMB":          float64(mem.Sys) / 1024 / 1024,
			"gcCycles":       mem.NumGC,
			"gcTotalPauseMs": float64(mem.PauseTotalNs) / 1e6,
			"gcLastPauseMs":  float64(mem.PauseNs[(mem.NumGC+255)%256]) / 1e6,
			"stackInuseMB":   float64(mem.StackInuse) / 1024 / 1024,
			"mallocs":        mem.Mallocs,
			"frees":          mem.Frees,
			"liveObjects":    mem.Mallocs - mem.Frees,
			"nextGCMB":       float64(mem.NextGC) / 1024 / 1024,
			"gcCPUPercent":   mem.GCCPUFraction * 100,
		},
	}

	if s.uiRuntime != nil {
		result["timeline"] = s.uiRuntime.TimelineStats()
	}

	return result, nil
}

func debugForceGC(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
	var before goruntime.MemStats
	goruntime.ReadMemStats(&before)

	goruntime.GC()

	var after goruntime.MemStats
	goruntime.ReadMemStats(&after)

	return map[string]any{
		"before": map[string]any{
			"heapAllocMB": float64(before.HeapAlloc) / 1024 / 1024,
			"heapObjects": before.HeapObjects,
			"liveObjects": before.Mallocs - before.Frees,
		},
		"after": map[string]any{
			"heapAllocMB": float64(after.HeapAlloc) / 1024 / 1024,
			"heapObjects": after.HeapObjects,
			"liveObjects": after.Mallocs - after.Frees,
		},
		"freedMB":      float64(before.HeapAlloc-after.HeapAlloc) / 1024 / 1024,
		"freedObjects": int64(before.HeapObjects) - int64(after.HeapObjects),
		"gcCycles":     after.NumGC,
	}, nil
}
