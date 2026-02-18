package apiserver

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/service"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

func asMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func (s *Server) workspaceRunCreate(ctx context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		if s.uiRuntime != nil {
			s.uiRuntime.SetWorkspaceUnavailable("workspace manager not initialized")
		}
		return nil, pkgerr.New("WorkspaceRun", "workspace manager not initialized")
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
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Create", "invalid params")
	}
	if p.SourceRoot == "" {
		p.SourceRoot = "."
	}
	run, err := s.workspaceMgr.CreateRun(ctx, service.WorkspaceCreateRequest{
		RunKey:     p.RunKey,
		DagKey:     p.DagKey,
		SourceRoot: p.SourceRoot,
		CreatedBy:  p.CreatedBy,
		Files:      p.Files,
		Metadata:   p.Metadata,
	})
	if err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Create", "create run")
	}
	if s.uiRuntime != nil {
		s.uiRuntime.UpsertWorkspaceRun(asMap(run))
	}
	s.Notify("workspace/run/created", map[string]any{
		"runKey": run.RunKey,
		"run":    run,
	})
	return map[string]any{"run": run}, nil
}

func (s *Server) workspaceRunGet(ctx context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		if s.uiRuntime != nil {
			s.uiRuntime.SetWorkspaceUnavailable("workspace manager not initialized")
		}
		return nil, pkgerr.New("WorkspaceRun", "workspace manager not initialized")
	}
	var p struct {
		RunKey string `json:"runKey"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Get", "invalid params")
	}
	if p.RunKey == "" {
		return nil, pkgerr.New("WorkspaceRun", "runKey is required")
	}
	run, err := s.workspaceMgr.GetRun(ctx, p.RunKey)
	if err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Get", "get run")
	}
	if run == nil {
		return map[string]any{"run": nil}, nil
	}
	return map[string]any{"run": run}, nil
}

func (s *Server) workspaceRunList(ctx context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		if s.uiRuntime != nil {
			s.uiRuntime.SetWorkspaceUnavailable("workspace manager not initialized")
		}
		return nil, pkgerr.New("WorkspaceRun", "workspace manager not initialized")
	}
	var p struct {
		Status string `json:"status"`
		DagKey string `json:"dagKey"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.List", "invalid params")
	}
	if p.Limit <= 0 || p.Limit > 5000 {
		p.Limit = 200
	}
	runs, err := s.workspaceMgr.ListRuns(ctx, p.Status, p.DagKey, p.Limit)
	if err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.List", "list runs")
	}
	if s.uiRuntime != nil {
		rawRuns := make([]map[string]any, 0, len(runs))
		for _, run := range runs {
			rawRuns = append(rawRuns, asMap(run))
		}
		s.uiRuntime.ReplaceWorkspaceRuns(rawRuns)
	}
	return map[string]any{"runs": runs}, nil
}

func (s *Server) workspaceRunMerge(ctx context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		if s.uiRuntime != nil {
			s.uiRuntime.SetWorkspaceUnavailable("workspace manager not initialized")
		}
		return nil, pkgerr.New("WorkspaceRun", "workspace manager not initialized")
	}
	var p struct {
		RunKey        string `json:"runKey"`
		UpdatedBy     string `json:"updatedBy"`
		DryRun        bool   `json:"dryRun"`
		DeleteRemoved bool   `json:"deleteRemoved"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Merge", "invalid params")
	}
	if p.RunKey == "" {
		return nil, pkgerr.New("WorkspaceRun", "runKey is required")
	}
	result, err := s.workspaceMgr.MergeRun(ctx, service.WorkspaceMergeRequest{
		RunKey:        p.RunKey,
		UpdatedBy:     p.UpdatedBy,
		DryRun:        p.DryRun,
		DeleteRemoved: p.DeleteRemoved,
	})
	if err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Merge", "merge run")
	}
	if s.uiRuntime != nil {
		s.uiRuntime.ApplyWorkspaceMergeResult(p.RunKey, asMap(result))
	}
	s.Notify("workspace/run/merged", map[string]any{
		"runKey": p.RunKey,
		"result": result,
	})
	return map[string]any{"result": result}, nil
}

func (s *Server) workspaceRunAbort(ctx context.Context, params json.RawMessage) (any, error) {
	if s.workspaceMgr == nil {
		if s.uiRuntime != nil {
			s.uiRuntime.SetWorkspaceUnavailable("workspace manager not initialized")
		}
		return nil, pkgerr.New("WorkspaceRun", "workspace manager not initialized")
	}
	var p struct {
		RunKey    string `json:"runKey"`
		UpdatedBy string `json:"updatedBy"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Abort", "invalid params")
	}
	if p.RunKey == "" {
		return nil, pkgerr.New("WorkspaceRun", "runKey is required")
	}
	run, err := s.workspaceMgr.AbortRun(ctx, p.RunKey, p.UpdatedBy, p.Reason)
	if err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Abort", "abort run")
	}
	if s.uiRuntime != nil {
		s.uiRuntime.UpsertWorkspaceRun(asMap(run))
	}
	s.Notify("workspace/run/aborted", map[string]any{
		"runKey": p.RunKey,
		"run":    run,
		"reason": p.Reason,
	})
	return map[string]any{"run": run}, nil
}
