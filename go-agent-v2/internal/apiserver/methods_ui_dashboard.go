package apiserver

import (
	"context"
	"encoding/json"
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

// callDash 调用已注册的 dashboard 方法。
func (s *Server) callDash(method string) (any, error) {
	h, ok := s.methods["dashboard/"+method]
	if !ok {
		return nil, nil
	}
	return h(context.Background(), json.RawMessage(`{}`))
}

func (s *Server) uiDashboardGet(ctx context.Context, p uiDashboardGetParams) (any, error) {
	_ = ctx
	result := newDashboardPayload()

	switch p.Page {
	case "agents":
		out, _ := s.callDash("agentStatus")
		copyListField(result, "agents", out, "agents")
	case "dags":
		out, _ := s.callDash("dags")
		copyListField(result, "dags", out, "dags")
	case "tasks":
		acks, _ := s.callDash("taskAcks")
		traces, _ := s.callDash("taskTraces")
		copyListField(result, "taskAcks", acks, "acks")
		copyListField(result, "taskTraces", traces, "traces")
	case "skills":
		out, _ := s.callDash("skills")
		copyListField(result, "skills", out, "skills")
	case "commands":
		cards, _ := s.callDash("commandCards")
		prompts, _ := s.callDash("prompts")
		copyListField(result, "commandCards", cards, "cards")
		copyListField(result, "prompts", prompts, "prompts")
	case "memory":
		out, _ := s.callDash("sharedFiles")
		copyListField(result, "memory", out, "files")
	default:
		// keep stable empty shape
	}

	return result, nil
}
