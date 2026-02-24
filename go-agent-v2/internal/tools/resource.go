package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ResourceTools returns resource tool schemas and handlers.
func ResourceTools(provider ResourceProvider) []Tool {
	if provider == nil || provider.DAGStore() == nil {
		return nil
	}

	tools := []Tool{
		{
			Schema: agentcore.DynamicTool{
				Name:        "task_create_dag",
				Description: "Create a task DAG with nodes. Each node can have dependencies, be assigned to an agent, and reference a command card.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"dag_key":     map[string]any{"type": "string", "description": "Unique key for the DAG"},
						"title":       map[string]any{"type": "string", "description": "DAG title"},
						"description": map[string]any{"type": "string", "description": "What this DAG does"},
						"nodes": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"node_key":    map[string]any{"type": "string", "description": "Unique key within DAG"},
									"title":       map[string]any{"type": "string", "description": "Node title"},
									"assigned_to": map[string]any{"type": "string", "description": "Agent ID to assign this node to"},
									"depends_on":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Node keys this depends on"},
									"command_ref": map[string]any{"type": "string", "description": "Command card key to use"},
								},
								"required": []string{"node_key", "title"},
							},
						},
					},
					"required": []string{"dag_key", "title"},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceTaskCreateDAG(provider, args) },
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "task_get_dag",
				Description: "Get a task DAG with all its nodes and their statuses.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"dag_key": map[string]any{"type": "string", "description": "DAG key to look up"},
					},
					"required": []string{"dag_key"},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceTaskGetDAG(provider, args) },
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "task_update_node",
				Description: "Update a task DAG node's status (pending/running/done/failed).",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"dag_key":  map[string]any{"type": "string", "description": "DAG key"},
						"node_key": map[string]any{"type": "string", "description": "Node key to update"},
						"status":   map[string]any{"type": "string", "description": "New status: pending, running, done, failed", "enum": []string{"pending", "running", "done", "failed"}},
						"result":   map[string]any{"type": "string", "description": "Result summary (optional)"},
					},
					"required": []string{"dag_key", "node_key", "status"},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceTaskUpdateNode(provider, args) },
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "command_list",
				Description: "List available command cards. Command cards define reusable operations with templates and argument schemas.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"keyword": map[string]any{"type": "string", "description": "Search keyword (optional)"},
					},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceCommandList(provider, args) },
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "command_get",
				Description: "Get a specific command card by its key, including the command template and argument schema.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"card_key": map[string]any{"type": "string", "description": "Command card key"},
					},
					"required": []string{"card_key"},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceCommandGet(provider, args) },
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "prompt_list",
				Description: "List available prompt templates. Templates can be used to generate structured prompts for agents.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"keyword": map[string]any{"type": "string", "description": "Search keyword (optional)"},
					},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourcePromptList(provider, args) },
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "prompt_get",
				Description: "Get a specific prompt template by its key, including the prompt text and variables.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"prompt_key": map[string]any{"type": "string", "description": "Prompt template key"},
					},
					"required": []string{"prompt_key"},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourcePromptGet(provider, args) },
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "shared_file_read",
				Description: "Read a shared file by path. Shared files are stored in the database and can be accessed by all agents.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string", "description": "File path (e.g. 'config/settings.json')"},
					},
					"required": []string{"path"},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceSharedFileRead(provider, args) },
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "shared_file_write",
				Description: "Write content to a shared file. Creates or overwrites the file at the given path.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":    map[string]any{"type": "string", "description": "File path (e.g. 'config/settings.json')"},
						"content": map[string]any{"type": "string", "description": "File content to write"},
					},
					"required": []string{"path", "content"},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceSharedFileWrite(provider, args) },
		},
	}

	if provider.WorkspaceManager() != nil {
		tools = append(tools,
			Tool{
				Schema: agentcore.DynamicTool{
					Name:        "workspace_create_run",
					Description: "Create a virtual workspace run. Filesystem workspace is used for edits; run status and file states are persisted in PostgreSQL.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"run_key":     map[string]any{"type": "string", "description": "Optional run key. Auto-generated if omitted."},
							"dag_key":     map[string]any{"type": "string", "description": "Related DAG key (optional)."},
							"source_root": map[string]any{"type": "string", "description": "Absolute or relative source project root."},
							"created_by":  map[string]any{"type": "string", "description": "Creator identifier (optional)."},
							"files": map[string]any{
								"type":        "array",
								"description": "Optional bootstrap files to copy from source root to workspace.",
								"items":       map[string]any{"type": "string"},
							},
							"metadata": map[string]any{"type": "object", "description": "Optional metadata for run record."},
						},
						"required": []string{"source_root"},
					},
				},
				Handler: func(_ ToolCallContext, args json.RawMessage) string {
					return resourceWorkspaceCreateRun(provider, args)
				},
			},
			Tool{
				Schema: agentcore.DynamicTool{
					Name:        "workspace_get_run",
					Description: "Get workspace run detail by run key.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"run_key": map[string]any{"type": "string", "description": "Workspace run key."},
						},
						"required": []string{"run_key"},
					},
				},
				Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceWorkspaceGetRun(provider, args) },
			},
			Tool{
				Schema: agentcore.DynamicTool{
					Name:        "workspace_list_runs",
					Description: "List workspace runs with optional status/dag filters.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"status":  map[string]any{"type": "string", "description": "Optional run status filter."},
							"dag_key": map[string]any{"type": "string", "description": "Optional DAG key filter."},
							"limit":   map[string]any{"type": "number", "description": "Max number of runs to return."},
						},
					},
				},
				Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceWorkspaceListRuns(provider, args) },
			},
			Tool{
				Schema: agentcore.DynamicTool{
					Name:        "workspace_merge_run",
					Description: "Merge changed files from virtual workspace back to source root with conflict detection. Also updates PostgreSQL run/file states.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"run_key":        map[string]any{"type": "string", "description": "Workspace run key."},
							"updated_by":     map[string]any{"type": "string", "description": "Operator id (optional)."},
							"dry_run":        map[string]any{"type": "boolean", "description": "Only simulate merge without writing source files."},
							"delete_removed": map[string]any{"type": "boolean", "description": "Delete source files removed in workspace when safe."},
						},
						"required": []string{"run_key"},
					},
				},
				Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceWorkspaceMergeRun(provider, args) },
			},
			Tool{
				Schema: agentcore.DynamicTool{
					Name:        "workspace_abort_run",
					Description: "Abort a workspace run and mark it as aborted in PostgreSQL state.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"run_key":    map[string]any{"type": "string", "description": "Workspace run key."},
							"updated_by": map[string]any{"type": "string", "description": "Operator id (optional)."},
							"reason":     map[string]any{"type": "string", "description": "Abort reason (optional)."},
						},
						"required": []string{"run_key"},
					},
				},
				Handler: func(_ ToolCallContext, args json.RawMessage) string { return resourceWorkspaceAbortRun(provider, args) },
			},
		)
	}

	return tools
}

func resourceToolCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func resourceTaskCreateDAG(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		DagKey      string `json:"dag_key"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Nodes       []struct {
			NodeKey    string   `json:"node_key"`
			Title      string   `json:"title"`
			AssignedTo string   `json:"assigned_to"`
			DependsOn  []string `json:"depends_on"`
			CommandRef string   `json:"command_ref"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.CreateDAG", "invalid args"))
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()

	dag, err := provider.DAGStore().SaveDAG(ctx, &store.TaskDAG{
		DagKey:      p.DagKey,
		Title:       p.Title,
		Description: p.Description,
		Status:      "draft",
	})
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.CreateDAG", "create dag"))
	}

	nodesCreated := 0
	for _, n := range p.Nodes {
		_, err := provider.DAGStore().SaveNode(ctx, &store.TaskDAGNode{
			DagKey:     p.DagKey,
			NodeKey:    n.NodeKey,
			Title:      n.Title,
			AssignedTo: n.AssignedTo,
			DependsOn:  n.DependsOn,
			CommandRef: n.CommandRef,
		})
		if err != nil {
			logger.Warn("resource: save node failed", logger.FieldDAG, p.DagKey, logger.FieldNode, n.NodeKey, logger.FieldError, err)
			continue
		}
		nodesCreated++
	}

	logger.Info("resource: DAG created", logger.FieldDAG, p.DagKey, "nodes", nodesCreated)
	data, _ := json.Marshal(map[string]any{
		"dag_key":       dag.DagKey,
		"title":         dag.Title,
		"nodes_created": nodesCreated,
		"status":        dag.Status,
	})
	return string(data)
}

func resourceTaskGetDAG(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		DagKey string `json:"dag_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.GetDAG", "invalid args"))
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	dag, nodes, err := provider.DAGStore().GetDAGDetail(ctx, p.DagKey)
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.GetDAG", "get dag"))
	}
	if dag == nil {
		return ToolError(pkgerr.Newf("ResourceTool.GetDAG", "dag %s not found", p.DagKey))
	}

	data, _ := json.Marshal(map[string]any{
		"dag":   dag,
		"nodes": nodes,
	})
	return string(data)
}

func resourceTaskUpdateNode(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		DagKey  string `json:"dag_key"`
		NodeKey string `json:"node_key"`
		Status  string `json:"status"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.UpdateNode", "invalid args"))
	}

	var result any
	if p.Result != "" {
		result = p.Result
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	node, err := provider.DAGStore().UpdateNodeStatus(ctx, p.DagKey, p.NodeKey, p.Status, result)
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.UpdateNode", "update node"))
	}
	if node == nil {
		return `{"error":"node not found"}`
	}

	logger.Info("resource: node updated", logger.FieldDAG, p.DagKey, logger.FieldNode, p.NodeKey, logger.FieldStatus, p.Status)
	data, _ := json.Marshal(node)
	return string(data)
}

func resourceCommandList(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		logger.Debug("resource: unmarshal command list args", logger.FieldError, err)
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	cards, err := provider.CommandCardStore().List(ctx, p.Keyword, 50)
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.CommandList", "list commands"))
	}
	data, _ := json.Marshal(cards)
	return string(data)
}

func resourceCommandGet(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		CardKey string `json:"card_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.CommandGet", "invalid args"))
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	card, err := provider.CommandCardStore().Get(ctx, p.CardKey)
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.CommandGet", "get command"))
	}
	if card == nil {
		return ToolError(pkgerr.Newf("ResourceTool.CommandGet", "command %s not found", p.CardKey))
	}
	data, _ := json.Marshal(card)
	return string(data)
}

func resourcePromptList(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		logger.Debug("resource: unmarshal prompt list args", logger.FieldError, err)
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	prompts, err := provider.PromptTemplateStore().List(ctx, "", p.Keyword, 50)
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.PromptList", "list prompts"))
	}
	data, _ := json.Marshal(prompts)
	return string(data)
}

func resourcePromptGet(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		PromptKey string `json:"prompt_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.PromptGet", "invalid args"))
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	prompt, err := provider.PromptTemplateStore().Get(ctx, p.PromptKey)
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.PromptGet", "get prompt"))
	}
	if prompt == nil {
		return ToolError(pkgerr.Newf("ResourceTool.PromptGet", "prompt %s not found", p.PromptKey))
	}
	data, _ := json.Marshal(prompt)
	return string(data)
}

func resourceSharedFileRead(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.FileRead", "invalid args"))
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	file, err := provider.SharedFileStore().Read(ctx, p.Path)
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.FileRead", "read file"))
	}
	if file == nil {
		return ToolError(pkgerr.Newf("ResourceTool.FileRead", "file %s not found", p.Path))
	}
	data, _ := json.Marshal(file)
	return string(data)
}

func resourceSharedFileWrite(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.FileWrite", "invalid args"))
	}
	if strings.TrimSpace(p.Path) == "" {
		return `{"error":"path is required"}`
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	file, err := provider.SharedFileStore().Write(ctx, p.Path, p.Content, "agent")
	if err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.FileWrite", "write file"))
	}

	logger.Info("resource: file written", logger.FieldPath, p.Path, logger.FieldLen, len(p.Content))
	data, _ := json.Marshal(file)
	return string(data)
}

func resourceWorkspaceCreateRun(provider ResourceProvider, args json.RawMessage) string {
	if provider.WorkspaceManager() == nil {
		return ToolError(pkgerr.New("ResourceTool.WorkspaceCreate", "workspace manager not initialized"))
	}
	var p struct {
		RunKey     string   `json:"run_key"`
		DagKey     string   `json:"dag_key"`
		SourceRoot string   `json:"source_root"`
		CreatedBy  string   `json:"created_by"`
		Files      []string `json:"files"`
		Metadata   any      `json:"metadata"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.WorkspaceCreate", "invalid args"))
	}
	ctx, cancel := resourceToolCtx()
	defer cancel()
	run, err := provider.WorkspaceManager().CreateRun(ctx, service.WorkspaceCreateRequest{
		RunKey:     p.RunKey,
		DagKey:     p.DagKey,
		SourceRoot: p.SourceRoot,
		CreatedBy:  p.CreatedBy,
		Files:      p.Files,
		Metadata:   p.Metadata,
	})
	if err != nil {
		return ToolError(err)
	}
	provider.NotifyEvent("workspace/run/created", map[string]any{
		"runKey": run.RunKey,
		"run":    run,
	})
	data, _ := json.Marshal(run)
	return string(data)
}

func resourceWorkspaceGetRun(provider ResourceProvider, args json.RawMessage) string {
	if provider.WorkspaceManager() == nil {
		return ToolError(pkgerr.New("ResourceTool.WorkspaceGet", "workspace manager not initialized"))
	}
	var p struct {
		RunKey string `json:"run_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.WorkspaceGet", "invalid args"))
	}
	ctx, cancel := resourceToolCtx()
	defer cancel()
	run, err := provider.WorkspaceManager().GetRun(ctx, p.RunKey)
	if err != nil {
		return ToolError(err)
	}
	if run == nil {
		return ToolError(pkgerr.Newf("ResourceTool.WorkspaceGet", "workspace run %s not found", p.RunKey))
	}
	data, _ := json.Marshal(run)
	return string(data)
}

func resourceWorkspaceListRuns(provider ResourceProvider, args json.RawMessage) string {
	if provider.WorkspaceManager() == nil {
		return ToolError(pkgerr.New("ResourceTool.WorkspaceList", "workspace manager not initialized"))
	}
	var p struct {
		Status string `json:"status"`
		DagKey string `json:"dag_key"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		logger.Debug("resource: unmarshal workspace list args", logger.FieldError, err)
	}
	if p.Limit <= 0 || p.Limit > 5000 {
		p.Limit = 200
	}
	ctx, cancel := resourceToolCtx()
	defer cancel()
	runs, err := provider.WorkspaceManager().ListRuns(ctx, p.Status, p.DagKey, p.Limit)
	if err != nil {
		return ToolError(err)
	}
	data, _ := json.Marshal(runs)
	return string(data)
}

func resourceWorkspaceMergeRun(provider ResourceProvider, args json.RawMessage) string {
	if provider.WorkspaceManager() == nil {
		return ToolError(pkgerr.New("ResourceTool.WorkspaceMerge", "workspace manager not initialized"))
	}
	var p struct {
		RunKey        string `json:"run_key"`
		UpdatedBy     string `json:"updated_by"`
		DryRun        bool   `json:"dry_run"`
		DeleteRemoved bool   `json:"delete_removed"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.WorkspaceMerge", "invalid args"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := provider.WorkspaceManager().MergeRun(ctx, service.WorkspaceMergeRequest{
		RunKey:        p.RunKey,
		UpdatedBy:     p.UpdatedBy,
		DryRun:        p.DryRun,
		DeleteRemoved: p.DeleteRemoved,
	})
	if err != nil {
		return ToolError(err)
	}
	provider.NotifyEvent("workspace/run/merged", map[string]any{
		"runKey": p.RunKey,
		"result": result,
	})
	data, _ := json.Marshal(result)
	return string(data)
}

func resourceWorkspaceAbortRun(provider ResourceProvider, args json.RawMessage) string {
	if provider.WorkspaceManager() == nil {
		return ToolError(pkgerr.New("ResourceTool.WorkspaceAbort", "workspace manager not initialized"))
	}
	var p struct {
		RunKey    string `json:"run_key"`
		UpdatedBy string `json:"updated_by"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(pkgerr.Wrap(err, "ResourceTool.WorkspaceAbort", "invalid args"))
	}
	ctx, cancel := resourceToolCtx()
	defer cancel()
	run, err := provider.WorkspaceManager().AbortRun(ctx, p.RunKey, p.UpdatedBy, p.Reason)
	if err != nil {
		return ToolError(err)
	}
	provider.NotifyEvent("workspace/run/aborted", map[string]any{
		"runKey": p.RunKey,
		"run":    run,
		"reason": p.Reason,
	})
	data, _ := json.Marshal(run)
	return string(data)
}
