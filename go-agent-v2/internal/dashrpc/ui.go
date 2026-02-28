package dashrpc

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type threadListItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type threadListResponse struct {
	Threads []threadListItem `json:"threads"`
}

func newDashboardPayload() map[string]any {
	return map[string]any{
		"agents":       []any{},
		"dags":         []any{},
		"taskAcks":     []any{},
		"taskTraces":   []any{},
		"skills":       []any{},
		"commandCards": []any{},
		"prompts":      []any{},
		"memory":       []any{},
	}
}

func copyListField(dst map[string]any, dstKey string, src any, srcKey string) {
	if srcMap, ok := src.(map[string]any); ok {
		if value, ok := srcMap[srcKey]; ok {
			dst[dstKey] = value
		}
	}
}

func callMethod(ctx context.Context, caller MethodCaller, method string, params json.RawMessage) (any, error) {
	if caller == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return caller(ctx, method, params)
}

func callDash(ctx context.Context, caller MethodCaller, method string) (any, error) {
	return callMethod(ctx, caller, "dashboard/"+method, json.RawMessage(`{}`))
}

func decodeThreadList(out any) (threadListResponse, bool) {
	if out == nil {
		return threadListResponse{}, false
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return threadListResponse{}, false
	}
	var resp threadListResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return threadListResponse{}, false
	}
	return resp, true
}

func buildAgentFallbackFromThreads(ctx context.Context, caller MethodCaller) []any {
	out, err := callMethod(ctx, caller, "thread/list", json.RawMessage(`{}`))
	if err != nil || out == nil {
		return nil
	}

	resp, ok := decodeThreadList(out)
	if !ok || len(resp.Threads) == 0 {
		return nil
	}

	now := time.Now().UTC()
	agents := make([]any, 0, len(resp.Threads))
	for _, thread := range resp.Threads {
		agentID := strings.TrimSpace(thread.ID)
		if agentID == "" {
			continue
		}
		agentName := strings.TrimSpace(thread.Name)
		if agentName == "" {
			agentName = agentID
		}
		status := strings.TrimSpace(thread.State)
		if status == "" {
			status = "idle"
		}

		agents = append(agents, map[string]any{
			"agent_id":   agentID,
			"agent_name": agentName,
			"status":     status,
			"updated_at": now,
		})
	}
	return agents
}

// UIDashboardGet returns the stable payload for ui/dashboard/get.
func UIDashboardGet(ctx context.Context, caller MethodCaller, p UIGetParams) (any, error) {
	result := newDashboardPayload()

	switch p.Page {
	case "agents":
		out, _ := callDash(ctx, caller, "agentStatus")
		copyListField(result, "agents", out, "agents")
		if current, ok := result["agents"].([]any); !ok || len(current) == 0 {
			if fallback := buildAgentFallbackFromThreads(ctx, caller); len(fallback) > 0 {
				result["agents"] = fallback
			}
		}
	case "dags":
		out, _ := callDash(ctx, caller, "dags")
		copyListField(result, "dags", out, "dags")
	case "tasks":
		acks, _ := callDash(ctx, caller, "taskAcks")
		traces, _ := callDash(ctx, caller, "taskTraces")
		copyListField(result, "taskAcks", acks, "acks")
		copyListField(result, "taskTraces", traces, "traces")
	case "skills":
		out, _ := callDash(ctx, caller, "skills")
		copyListField(result, "skills", out, "skills")
	case "commands":
		cards, _ := callDash(ctx, caller, "commandCards")
		prompts, _ := callDash(ctx, caller, "prompts")
		copyListField(result, "commandCards", cards, "cards")
		copyListField(result, "prompts", prompts, "prompts")
	case "memory":
		out, _ := callDash(ctx, caller, "sharedFiles")
		copyListField(result, "memory", out, "files")
	default:
		// keep stable empty shape
	}

	return result, nil
}
