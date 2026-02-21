package apiserver

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type uiDashboardGetParams struct {
	Page string `json:"page"`
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
	srcMap, ok := src.(map[string]any)
	if !ok {
		return
	}
	value, ok := srcMap[srcKey]
	if !ok {
		return
	}
	dst[dstKey] = value
}

// callMethod 调用已注册的方法。
func (s *Server) callMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	h, ok := s.methods[method]
	if !ok {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return h(ctx, params)
}

// callDash 调用已注册的 dashboard 方法。
func (s *Server) callDash(ctx context.Context, method string) (any, error) {
	return s.callMethod(ctx, "dashboard/"+method, json.RawMessage(`{}`))
}

// buildAgentFallbackFromThreads 从 thread/list 构造 agents 页面兜底数据。
//
// 场景: 重启后 dashboard/agentStatus 暂无记录时, 使用线程列表(含历史线程)保证前端 Agent 页不为空。
func (s *Server) buildAgentFallbackFromThreads(ctx context.Context) []any {
	out, err := s.callMethod(ctx, "thread/list", json.RawMessage(`{}`))
	if err != nil || out == nil {
		return nil
	}

	resp, ok := out.(threadListResponse)
	if !ok {
		if ptr, ok := out.(*threadListResponse); ok && ptr != nil {
			resp = *ptr
		} else {
			raw, marshalErr := json.Marshal(out)
			if marshalErr != nil {
				return nil
			}
			if unmarshalErr := json.Unmarshal(raw, &resp); unmarshalErr != nil {
				return nil
			}
		}
	}
	if len(resp.Threads) == 0 {
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

func (s *Server) uiDashboardGet(ctx context.Context, p uiDashboardGetParams) (any, error) {
	result := newDashboardPayload()

	switch p.Page {
	case "agents":
		out, _ := s.callDash(ctx, "agentStatus")
		copyListField(result, "agents", out, "agents")
		if current, ok := result["agents"].([]any); !ok || len(current) == 0 {
			if fallback := s.buildAgentFallbackFromThreads(ctx); len(fallback) > 0 {
				result["agents"] = fallback
			}
		}
	case "dags":
		out, _ := s.callDash(ctx, "dags")
		copyListField(result, "dags", out, "dags")
	case "tasks":
		acks, _ := s.callDash(ctx, "taskAcks")
		traces, _ := s.callDash(ctx, "taskTraces")
		copyListField(result, "taskAcks", acks, "acks")
		copyListField(result, "taskTraces", traces, "traces")
	case "skills":
		out, _ := s.callDash(ctx, "skills")
		copyListField(result, "skills", out, "skills")
	case "commands":
		cards, _ := s.callDash(ctx, "commandCards")
		prompts, _ := s.callDash(ctx, "prompts")
		copyListField(result, "commandCards", cards, "cards")
		copyListField(result, "prompts", prompts, "prompts")
	case "memory":
		out, _ := s.callDash(ctx, "sharedFiles")
		copyListField(result, "memory", out, "files")
	default:
		// keep stable empty shape
	}

	return result, nil
}
