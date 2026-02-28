package apiserver

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/service"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

const workspaceManagerNotInitialized = "workspace manager not initialized"

func workspaceManager(s *Server) (*service.WorkspaceManager, error) {
	if s.workspaceMgr == nil {
		if s.uiRuntime != nil {
			s.uiRuntime.SetWorkspaceUnavailable(workspaceManagerNotInitialized)
		}
		return nil, pkgerr.New("WorkspaceRun", workspaceManagerNotInitialized)
	}
	return s.workspaceMgr, nil
}

func asMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if out, ok := value.(map[string]any); ok {
		return out
	}
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func workspaceRunCreate(s *Server, ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := workspaceManager(s)
	if err != nil {
		return nil, err
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
	run, err := mgr.CreateRun(ctx, service.WorkspaceCreateRequest{
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
	notify(s, "workspace/run/created", map[string]any{
		"runKey": run.RunKey,
		"run":    run,
	})
	return map[string]any{"run": run}, nil
}

func workspaceRunGet(s *Server, ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := workspaceManager(s)
	if err != nil {
		return nil, err
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
	run, err := mgr.GetRun(ctx, p.RunKey)
	if err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Get", "get run")
	}
	return map[string]any{"run": run}, nil
}

func workspaceRunList(s *Server, ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := workspaceManager(s)
	if err != nil {
		return nil, err
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
	runs, err := mgr.ListRuns(ctx, p.Status, p.DagKey, p.Limit)
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

func workspaceRunMerge(s *Server, ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := workspaceManager(s)
	if err != nil {
		return nil, err
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
	result, err := mgr.MergeRun(ctx, service.WorkspaceMergeRequest{
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
	notify(s, "workspace/run/merged", map[string]any{
		"runKey": p.RunKey,
		"result": result,
	})
	return map[string]any{"result": result}, nil
}

func workspaceRunAbort(s *Server, ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := workspaceManager(s)
	if err != nil {
		return nil, err
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
	run, err := mgr.AbortRun(ctx, p.RunKey, p.UpdatedBy, p.Reason)
	if err != nil {
		return nil, pkgerr.Wrap(err, "WorkspaceRun.Abort", "abort run")
	}
	if s.uiRuntime != nil {
		s.uiRuntime.UpsertWorkspaceRun(asMap(run))
	}
	notify(s, "workspace/run/aborted", map[string]any{
		"runKey": p.RunKey,
		"run":    run,
		"reason": p.Reason,
	})
	return map[string]any{"run": run}, nil
}
