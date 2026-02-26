package apiserver

import (
	"context"
	"errors"

	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
)

type codeExecRunnerAdapter struct {
	runner *executor.CodeRunner
}

// adaptCodeExecRunner adapts executor.CodeRunner into tools.CodeExecRunner.
func adaptCodeExecRunner(runner *executor.CodeRunner) tools.CodeExecRunner {
	if runner == nil {
		return nil
	}
	return codeExecRunnerAdapter{runner: runner}
}

func (a codeExecRunnerAdapter) Run(ctx context.Context, req tools.CodeRunRequest) (*tools.CodeRunResult, error) {
	if a.runner == nil {
		return nil, errors.New("code runner is nil")
	}
	result, err := a.runner.Run(ctx, executor.RunRequest{
		Mode:     req.Mode,
		Language: req.Language,
		Code:     req.Code,
		Command:  req.Command,
		TestFunc: req.TestFunc,
		TestPkg:  req.TestPkg,
		AutoWrap: req.AutoWrap,
		WorkDir:  req.WorkDir,
		Timeout:  req.Timeout,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return &tools.CodeRunResult{
		Success:   result.Success,
		Output:    result.Output,
		ExitCode:  result.ExitCode,
		Duration:  result.Duration,
		Language:  result.Language,
		Mode:      result.Mode,
		Truncated: result.Truncated,
	}, nil
}

type auditLoggerAdapter struct {
	store *store.AuditLogStore
}

// adaptAuditLogger adapts store.AuditLogStore into tools.AuditLogger.
func adaptAuditLogger(s *store.AuditLogStore) tools.AuditLogger {
	if s == nil {
		return nil
	}
	return auditLoggerAdapter{store: s}
}

func (a auditLoggerAdapter) Append(ctx context.Context, e *tools.AuditEvent) error {
	if a.store == nil || e == nil {
		return nil
	}
	return a.store.Append(ctx, &store.AuditEvent{
		Ts:        e.Ts,
		EventType: e.EventType,
		Action:    e.Action,
		Result:    e.Result,
		Actor:     e.Actor,
		Target:    e.Target,
		Detail:    e.Detail,
		Level:     e.Level,
		Extra:     e.Extra,
	})
}

type dagManagerAdapter struct {
	store *store.TaskDAGStore
}

// adaptDAGManager adapts store.TaskDAGStore into tools.DAGManager.
func adaptDAGManager(s *store.TaskDAGStore) tools.DAGManager {
	if s == nil {
		return nil
	}
	return dagManagerAdapter{store: s}
}

func (a dagManagerAdapter) SaveDAG(ctx context.Context, d *tools.TaskDAG) (*tools.TaskDAG, error) {
	if a.store == nil {
		return nil, errors.New("dag store is nil")
	}
	if d == nil {
		return nil, errors.New("dag is nil")
	}
	saved, err := a.store.SaveDAG(ctx, toStoreTaskDAG(d))
	if err != nil {
		return nil, err
	}
	return toToolsTaskDAG(saved), nil
}

func (a dagManagerAdapter) ListDAGs(ctx context.Context, keyword, status string, limit int) ([]tools.TaskDAG, error) {
	if a.store == nil {
		return nil, errors.New("dag store is nil")
	}
	list, err := a.store.ListDAGs(ctx, keyword, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]tools.TaskDAG, 0, len(list))
	for i := range list {
		out = append(out, fromStoreTaskDAGValue(list[i]))
	}
	return out, nil
}

func (a dagManagerAdapter) GetDAGDetail(ctx context.Context, dagKey string) (*tools.TaskDAG, []tools.TaskDAGNode, error) {
	if a.store == nil {
		return nil, nil, errors.New("dag store is nil")
	}
	dag, nodes, err := a.store.GetDAGDetail(ctx, dagKey)
	if err != nil {
		return nil, nil, err
	}
	outNodes := make([]tools.TaskDAGNode, 0, len(nodes))
	for i := range nodes {
		outNodes = append(outNodes, fromStoreTaskDAGNodeValue(nodes[i]))
	}
	return toToolsTaskDAG(dag), outNodes, nil
}

func (a dagManagerAdapter) SaveNode(ctx context.Context, n *tools.TaskDAGNode) (*tools.TaskDAGNode, error) {
	if a.store == nil {
		return nil, errors.New("dag store is nil")
	}
	if n == nil {
		return nil, errors.New("dag node is nil")
	}
	saved, err := a.store.SaveNode(ctx, toStoreTaskDAGNode(n))
	if err != nil {
		return nil, err
	}
	return toToolsTaskDAGNode(saved), nil
}

func (a dagManagerAdapter) UpdateNodeStatus(ctx context.Context, dagKey, nodeKey, status string, result any) (*tools.TaskDAGNode, error) {
	if a.store == nil {
		return nil, errors.New("dag store is nil")
	}
	node, err := a.store.UpdateNodeStatus(ctx, dagKey, nodeKey, status, result)
	if err != nil {
		return nil, err
	}
	return toToolsTaskDAGNode(node), nil
}

func (a dagManagerAdapter) ListNodes(ctx context.Context, dagKey string) ([]tools.TaskDAGNode, error) {
	if a.store == nil {
		return nil, errors.New("dag store is nil")
	}
	nodes, err := a.store.ListNodes(ctx, dagKey)
	if err != nil {
		return nil, err
	}
	out := make([]tools.TaskDAGNode, 0, len(nodes))
	for i := range nodes {
		out = append(out, fromStoreTaskDAGNodeValue(nodes[i]))
	}
	return out, nil
}

type cardStoreAdapter struct {
	store *store.CommandCardStore
}

// adaptCardStore adapts store.CommandCardStore into tools.CardStore.
func adaptCardStore(s *store.CommandCardStore) tools.CardStore {
	if s == nil {
		return nil
	}
	return cardStoreAdapter{store: s}
}

func (a cardStoreAdapter) Save(ctx context.Context, c any) (any, error) {
	if a.store == nil {
		return nil, errors.New("command card store is nil")
	}
	switch v := c.(type) {
	case *store.CommandCard:
		return a.store.Save(ctx, v)
	case store.CommandCard:
		vv := v
		return a.store.Save(ctx, &vv)
	default:
		return nil, errors.New("unsupported command card type")
	}
}

func (a cardStoreAdapter) Get(ctx context.Context, cardKey string) (any, error) {
	if a.store == nil {
		return nil, errors.New("command card store is nil")
	}
	return a.store.Get(ctx, cardKey)
}

func (a cardStoreAdapter) List(ctx context.Context, keyword string, limit int) (any, error) {
	if a.store == nil {
		return nil, errors.New("command card store is nil")
	}
	return a.store.List(ctx, keyword, limit)
}

func (a cardStoreAdapter) SetEnabled(ctx context.Context, cardKey string, enabled bool, updatedBy string) error {
	if a.store == nil {
		return errors.New("command card store is nil")
	}
	return a.store.SetEnabled(ctx, cardKey, enabled, updatedBy)
}

func (a cardStoreAdapter) Delete(ctx context.Context, cardKey string) error {
	if a.store == nil {
		return errors.New("command card store is nil")
	}
	return a.store.Delete(ctx, cardKey)
}

type templateStoreAdapter struct {
	store *store.PromptTemplateStore
}

// adaptTemplateStore adapts store.PromptTemplateStore into tools.TemplateStore.
func adaptTemplateStore(s *store.PromptTemplateStore) tools.TemplateStore {
	if s == nil {
		return nil
	}
	return templateStoreAdapter{store: s}
}

func (a templateStoreAdapter) Save(ctx context.Context, t any) (any, error) {
	if a.store == nil {
		return nil, errors.New("prompt template store is nil")
	}
	switch v := t.(type) {
	case *store.PromptTemplate:
		return a.store.Save(ctx, v)
	case store.PromptTemplate:
		vv := v
		return a.store.Save(ctx, &vv)
	default:
		return nil, errors.New("unsupported prompt template type")
	}
}

func (a templateStoreAdapter) Get(ctx context.Context, promptKey string) (any, error) {
	if a.store == nil {
		return nil, errors.New("prompt template store is nil")
	}
	return a.store.Get(ctx, promptKey)
}

func (a templateStoreAdapter) List(ctx context.Context, agentKey, keyword string, limit int) (any, error) {
	if a.store == nil {
		return nil, errors.New("prompt template store is nil")
	}
	return a.store.List(ctx, agentKey, keyword, limit)
}

func (a templateStoreAdapter) SetEnabled(ctx context.Context, promptKey string, enabled bool, updatedBy string) error {
	if a.store == nil {
		return errors.New("prompt template store is nil")
	}
	return a.store.SetEnabled(ctx, promptKey, enabled, updatedBy)
}

func (a templateStoreAdapter) Delete(ctx context.Context, promptKey string) error {
	if a.store == nil {
		return errors.New("prompt template store is nil")
	}
	return a.store.Delete(ctx, promptKey)
}

type fileStoreAdapter struct {
	store *store.SharedFileStore
}

// adaptFileStore adapts store.SharedFileStore into tools.FileStore.
func adaptFileStore(s *store.SharedFileStore) tools.FileStore {
	if s == nil {
		return nil
	}
	return fileStoreAdapter{store: s}
}

func (a fileStoreAdapter) Write(ctx context.Context, path, content, actor string) (any, error) {
	if a.store == nil {
		return nil, errors.New("shared file store is nil")
	}
	return a.store.Write(ctx, path, content, actor)
}

func (a fileStoreAdapter) Read(ctx context.Context, path string) (any, error) {
	if a.store == nil {
		return nil, errors.New("shared file store is nil")
	}
	return a.store.Read(ctx, path)
}

func (a fileStoreAdapter) List(ctx context.Context, prefix string, limit int) (any, error) {
	if a.store == nil {
		return nil, errors.New("shared file store is nil")
	}
	return a.store.List(ctx, prefix, limit)
}

func (a fileStoreAdapter) Delete(ctx context.Context, path, actor string) (bool, error) {
	if a.store == nil {
		return false, errors.New("shared file store is nil")
	}
	return a.store.Delete(ctx, path, actor)
}

type workspaceOpsAdapter struct {
	manager *service.WorkspaceManager
}

// adaptWorkspaceOps adapts service.WorkspaceManager into tools.WorkspaceOps.
func adaptWorkspaceOps(mgr *service.WorkspaceManager) tools.WorkspaceOps {
	if mgr == nil {
		return nil
	}
	return workspaceOpsAdapter{manager: mgr}
}

func (a workspaceOpsAdapter) CreateRun(ctx context.Context, req tools.WorkspaceCreateRunRequest) (any, error) {
	if a.manager == nil {
		return nil, errors.New("workspace manager is nil")
	}
	return a.manager.CreateRun(ctx, service.WorkspaceCreateRequest{
		RunKey:     req.RunKey,
		DagKey:     req.DagKey,
		SourceRoot: req.SourceRoot,
		CreatedBy:  req.CreatedBy,
		Files:      req.Files,
		Metadata:   req.Metadata,
	})
}

func (a workspaceOpsAdapter) GetRun(ctx context.Context, runKey string) (any, error) {
	if a.manager == nil {
		return nil, errors.New("workspace manager is nil")
	}
	return a.manager.GetRun(ctx, runKey)
}

func (a workspaceOpsAdapter) ListRuns(ctx context.Context, status, dagKey string, limit int) (any, error) {
	if a.manager == nil {
		return nil, errors.New("workspace manager is nil")
	}
	return a.manager.ListRuns(ctx, status, dagKey, limit)
}

func (a workspaceOpsAdapter) ResolveRunWorkspace(ctx context.Context, runKey string) (string, error) {
	if a.manager == nil {
		return "", errors.New("workspace manager is nil")
	}
	return a.manager.ResolveRunWorkspace(ctx, runKey)
}

func (a workspaceOpsAdapter) AbortRun(ctx context.Context, runKey, updatedBy, reason string) (any, error) {
	if a.manager == nil {
		return nil, errors.New("workspace manager is nil")
	}
	return a.manager.AbortRun(ctx, runKey, updatedBy, reason)
}

func (a workspaceOpsAdapter) MergeRun(ctx context.Context, req tools.WorkspaceMergeRunRequest) (any, error) {
	if a.manager == nil {
		return nil, errors.New("workspace manager is nil")
	}
	return a.manager.MergeRun(ctx, service.WorkspaceMergeRequest{
		RunKey:        req.RunKey,
		UpdatedBy:     req.UpdatedBy,
		DryRun:        req.DryRun,
		DeleteRemoved: req.DeleteRemoved,
	})
}

type agentLauncherAdapter struct {
	manager *runner.AgentManager
}

// adaptAgentLauncher adapts runner.AgentManager into tools.AgentLauncher.
func adaptAgentLauncher(mgr *runner.AgentManager) tools.AgentLauncher {
	if mgr == nil {
		return nil
	}
	return agentLauncherAdapter{manager: mgr}
}

func (a agentLauncherAdapter) Launch(ctx context.Context, id, name, prompt, cwd, instructions string, dynamicTools []agentcore.DynamicTool) error {
	if a.manager == nil {
		return errors.New("agent manager is nil")
	}
	return a.manager.Launch(ctx, id, name, prompt, cwd, instructions, dynamicTools)
}

func (a agentLauncherAdapter) Submit(id, prompt string, images, files []string) error {
	if a.manager == nil {
		return errors.New("agent manager is nil")
	}
	return a.manager.Submit(id, prompt, images, files)
}

func (a agentLauncherAdapter) Stop(id string) error {
	if a.manager == nil {
		return errors.New("agent manager is nil")
	}
	return a.manager.Stop(id)
}

func (a agentLauncherAdapter) List() any {
	if a.manager == nil {
		return nil
	}
	return a.manager.List()
}

func toStoreTaskDAG(d *tools.TaskDAG) *store.TaskDAG {
	if d == nil {
		return nil
	}
	return &store.TaskDAG{
		ID:          d.ID,
		DagKey:      d.DagKey,
		Title:       d.Title,
		Description: d.Description,
		Status:      d.Status,
		CreatedBy:   d.CreatedBy,
		Metadata:    d.Metadata,
		StartedAt:   d.StartedAt,
		FinishedAt:  d.FinishedAt,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

func fromStoreTaskDAGValue(d store.TaskDAG) tools.TaskDAG {
	return tools.TaskDAG{
		ID:          d.ID,
		DagKey:      d.DagKey,
		Title:       d.Title,
		Description: d.Description,
		Status:      d.Status,
		CreatedBy:   d.CreatedBy,
		Metadata:    d.Metadata,
		StartedAt:   d.StartedAt,
		FinishedAt:  d.FinishedAt,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

func toToolsTaskDAG(d *store.TaskDAG) *tools.TaskDAG {
	if d == nil {
		return nil
	}
	v := fromStoreTaskDAGValue(*d)
	return &v
}

func toStoreTaskDAGNode(n *tools.TaskDAGNode) *store.TaskDAGNode {
	if n == nil {
		return nil
	}
	return &store.TaskDAGNode{
		ID:         n.ID,
		DagKey:     n.DagKey,
		NodeKey:    n.NodeKey,
		Title:      n.Title,
		NodeType:   n.NodeType,
		AssignedTo: n.AssignedTo,
		DependsOn:  n.DependsOn,
		Status:     n.Status,
		CommandRef: n.CommandRef,
		Config:     n.Config,
		Result:     n.Result,
		StartedAt:  n.StartedAt,
		FinishedAt: n.FinishedAt,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
	}
}

func fromStoreTaskDAGNodeValue(n store.TaskDAGNode) tools.TaskDAGNode {
	return tools.TaskDAGNode{
		ID:         n.ID,
		DagKey:     n.DagKey,
		NodeKey:    n.NodeKey,
		Title:      n.Title,
		NodeType:   n.NodeType,
		AssignedTo: n.AssignedTo,
		DependsOn:  n.DependsOn,
		Status:     n.Status,
		CommandRef: n.CommandRef,
		Config:     n.Config,
		Result:     n.Result,
		StartedAt:  n.StartedAt,
		FinishedAt: n.FinishedAt,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
	}
}

func toToolsTaskDAGNode(n *store.TaskDAGNode) *tools.TaskDAGNode {
	if n == nil {
		return nil
	}
	v := fromStoreTaskDAGNodeValue(*n)
	return &v
}
