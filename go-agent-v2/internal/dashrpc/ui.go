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

func callDash(ctx context.Context, caller MethodCaller, method string) (any, error) {
	if caller == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return caller(ctx, "dashboard/"+method, json.RawMessage(`{}`))
}

func buildAgentFallbackFromThreads(ctx context.Context, caller MethodCaller) []any {
	if caller == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out, err := caller(ctx, "thread/list", json.RawMessage(`{}`))
	if err != nil || out == nil {
		return nil
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	var resp threadListResponse
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Threads) == 0 {
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

func UIDashboardGet(ctx context.Context, caller MethodCaller, p UIGetParams) (any, error) {
	result := newDashboardPayload()
	dashCopy := func(dstKey, method, srcKey string) {
		out, _ := callDash(ctx, caller, method)
		copyListField(result, dstKey, out, srcKey)
	}

	switch p.Page {
	case "agents":
		dashCopy("agents", "agentStatus", "agents")
		if current, ok := result["agents"].([]any); !ok || len(current) == 0 {
			if fallback := buildAgentFallbackFromThreads(ctx, caller); len(fallback) > 0 {
				result["agents"] = fallback
			}
		}
	case "dags":
		dashCopy("dags", "dags", "dags")
	case "tasks":
		dashCopy("taskAcks", "taskAcks", "acks")
		dashCopy("taskTraces", "taskTraces", "traces")
	case "skills":
		dashCopy("skills", "skills", "skills")
	case "commands":
		dashCopy("commandCards", "commandCards", "cards")
		dashCopy("prompts", "prompts", "prompts")
	case "memory":
		dashCopy("memory", "sharedFiles", "files")
	default:
		// keep stable empty shape
	}

	return result, nil
}
