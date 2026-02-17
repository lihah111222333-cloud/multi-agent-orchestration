package apiserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/multi-agent/go-agent-v2/internal/service"
)

func (s *Server) workspaceRunCreate(_ context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		return nil, fmt.Errorf("workspace manager not initialized")
	}
	var p struct {
		RunKey     string   `json:"runKey"`
		DagKey     string   `json:"dagKey"`
		SourceRoot string   `json:"sourceRoot"`
		CreatedBy  string   `json:"createdBy"`
		Files      []string `json:"files"`
		Metadata   any      `json:"metadata"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.SourceRoot == "" {
		p.SourceRoot = "."
	}
	run, err := s.workspaceMgr.CreateRun(context.Background(), service.WorkspaceCreateRequest{
		RunKey:     p.RunKey,
		DagKey:     p.DagKey,
		SourceRoot: p.SourceRoot,
		CreatedBy:  p.CreatedBy,
		Files:      p.Files,
		Metadata:   p.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("workspace/run/create: %w", err)
	}
	s.Notify("workspace/run/created", map[string]any{
		"runKey": run.RunKey,
		"run":    run,
	})
	return map[string]any{"run": run}, nil
}

func (s *Server) workspaceRunGet(_ context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		return nil, fmt.Errorf("workspace manager not initialized")
	}
	var p struct {
		RunKey string `json:"runKey"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.RunKey == "" {
		return nil, fmt.Errorf("runKey is required")
	}
	run, err := s.workspaceMgr.GetRun(context.Background(), p.RunKey)
	if err != nil {
		return nil, fmt.Errorf("workspace/run/get: %w", err)
	}
	if run == nil {
		return map[string]any{"run": nil}, nil
	}
	return map[string]any{"run": run}, nil
}

func (s *Server) workspaceRunList(_ context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		return nil, fmt.Errorf("workspace manager not initialized")
	}
	var p struct {
		Status string `json:"status"`
		DagKey string `json:"dagKey"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.Limit <= 0 || p.Limit > 5000 {
		p.Limit = 200
	}
	runs, err := s.workspaceMgr.ListRuns(context.Background(), p.Status, p.DagKey, p.Limit)
	if err != nil {
		return nil, fmt.Errorf("workspace/run/list: %w", err)
	}
	return map[string]any{"runs": runs}, nil
}

func (s *Server) workspaceRunMerge(_ context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		return nil, fmt.Errorf("workspace manager not initialized")
	}
	var p struct {
		RunKey        string `json:"runKey"`
		UpdatedBy     string `json:"updatedBy"`
		DryRun        bool   `json:"dryRun"`
		DeleteRemoved bool   `json:"deleteRemoved"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.RunKey == "" {
		return nil, fmt.Errorf("runKey is required")
	}
	result, err := s.workspaceMgr.MergeRun(context.Background(), service.WorkspaceMergeRequest{
		RunKey:        p.RunKey,
		UpdatedBy:     p.UpdatedBy,
		DryRun:        p.DryRun,
		DeleteRemoved: p.DeleteRemoved,
	})
	if err != nil {
		return nil, fmt.Errorf("workspace/run/merge: %w", err)
	}
	s.Notify("workspace/run/merged", map[string]any{
		"runKey": p.RunKey,
		"result": result,
	})
	return map[string]any{"result": result}, nil
}

func (s *Server) workspaceRunAbort(_ context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		return nil, fmt.Errorf("workspace manager not initialized")
	}
	var p struct {
		RunKey    string `json:"runKey"`
		UpdatedBy string `json:"updatedBy"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.RunKey == "" {
		return nil, fmt.Errorf("runKey is required")
	}
	run, err := s.workspaceMgr.AbortRun(context.Background(), p.RunKey, p.UpdatedBy, p.Reason)
	if err != nil {
		return nil, fmt.Errorf("workspace/run/abort: %w", err)
	}
	s.Notify("workspace/run/aborted", map[string]any{
		"runKey": p.RunKey,
		"run":    run,
		"reason": p.Reason,
	})
	return map[string]any{"run": run}, nil
}
