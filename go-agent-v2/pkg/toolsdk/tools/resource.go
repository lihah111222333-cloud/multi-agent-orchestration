package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ResourceTools returns resource tool schemas and handlers.
func ResourceTools(provider ResourceProvider) []Tool {
	if provider == nil || provider.DAGManager() == nil {
		return nil
	}

	return buildResourceTools(provider, resourceToolSpecs())
}

type resourceToolSpec struct {
	schema        agentcore.DynamicTool
	handler       func(provider ResourceProvider, args json.RawMessage) string
	workspaceOnly bool
}

func buildResourceTools(provider ResourceProvider, specs []resourceToolSpec) []Tool {
	hasWorkspace := provider.WorkspaceOps() != nil
	tools := make([]Tool, 0, len(specs))
	for _, spec := range specs {
		if spec.workspaceOnly && !hasWorkspace {
			continue
		}
		spec := spec
		tools = append(tools, Tool{
			Schema: spec.schema,
			Handler: func(_ ToolCallContext, args json.RawMessage) string {
				return spec.handler(provider, args)
			},
		})
	}

	return tools
}

func resourceObjectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func resourceToolSpecs() []resourceToolSpec {
	return []resourceToolSpec{
		{
			schema: agentcore.DynamicTool{
				Name:        "task_create_dag",
				Description: "Create a task DAG with nodes. Each node can have dependencies, be assigned to an agent, and reference a command card.",
				InputSchema: resourceObjectSchema(
					map[string]any{
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
					"dag_key",
					"title",
				),
			},
			handler: resourceTaskCreateDAG,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "task_get_dag",
				Description: "Get a task DAG with all its nodes and their statuses.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"dag_key": map[string]any{"type": "string", "description": "DAG key to look up"},
					},
					"dag_key",
				),
			},
			handler: resourceTaskGetDAG,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "task_update_node",
				Description: "Update a task DAG node's status (pending/running/done/failed).",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"dag_key":  map[string]any{"type": "string", "description": "DAG key"},
						"node_key": map[string]any{"type": "string", "description": "Node key to update"},
						"status":   map[string]any{"type": "string", "description": "New status: pending, running, done, failed", "enum": []string{"pending", "running", "done", "failed"}},
						"result":   map[string]any{"type": "string", "description": "Result summary (optional)"},
					},
					"dag_key",
					"node_key",
					"status",
				),
			},
			handler: resourceTaskUpdateNode,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "command_list",
				Description: "List available command cards. Command cards define reusable operations with templates and argument schemas.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"keyword": map[string]any{"type": "string", "description": "Search keyword (optional)"},
					},
				),
			},
			handler: resourceCommandList,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "command_get",
				Description: "Get a specific command card by its key, including the command template and argument schema.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"card_key": map[string]any{"type": "string", "description": "Command card key"},
					},
					"card_key",
				),
			},
			handler: resourceCommandGet,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "prompt_list",
				Description: "List available prompt templates. Templates can be used to generate structured prompts for agents.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"keyword": map[string]any{"type": "string", "description": "Search keyword (optional)"},
					},
				),
			},
			handler: resourcePromptList,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "prompt_get",
				Description: "Get a specific prompt template by its key, including the prompt text and variables.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"prompt_key": map[string]any{"type": "string", "description": "Prompt template key"},
					},
					"prompt_key",
				),
			},
			handler: resourcePromptGet,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "shared_file_read",
				Description: "Read a shared file by path. Shared files are stored in the database and can be accessed by all agents.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"path": map[string]any{"type": "string", "description": "File path (e.g. 'config/settings.json')"},
					},
					"path",
				),
			},
			handler: resourceSharedFileRead,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "shared_file_write",
				Description: "Write content to a shared file. Creates or overwrites the file at the given path.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"path":    map[string]any{"type": "string", "description": "File path (e.g. 'config/settings.json')"},
						"content": map[string]any{"type": "string", "description": "File content to write"},
					},
					"path",
					"content",
				),
			},
			handler: resourceSharedFileWrite,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "workspace_create_run",
				Description: "Create a virtual workspace run. Filesystem workspace is used for edits; run status and file states are persisted in PostgreSQL.",
				InputSchema: resourceObjectSchema(
					map[string]any{
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
					"source_root",
				),
			},
			handler:       resourceWorkspaceCreateRun,
			workspaceOnly: true,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "workspace_get_run",
				Description: "Get workspace run detail by run key.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"run_key": map[string]any{"type": "string", "description": "Workspace run key."},
					},
					"run_key",
				),
			},
			handler:       resourceWorkspaceGetRun,
			workspaceOnly: true,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "workspace_list_runs",
				Description: "List workspace runs with optional status/dag filters.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"status":  map[string]any{"type": "string", "description": "Optional run status filter."},
						"dag_key": map[string]any{"type": "string", "description": "Optional DAG key filter."},
						"limit":   map[string]any{"type": "number", "description": "Max number of runs to return."},
					},
				),
			},
			handler:       resourceWorkspaceListRuns,
			workspaceOnly: true,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "workspace_merge_run",
				Description: "Merge changed files from virtual workspace back to source root with conflict detection. Also updates PostgreSQL run/file states.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"run_key":        map[string]any{"type": "string", "description": "Workspace run key."},
						"updated_by":     map[string]any{"type": "string", "description": "Operator id (optional)."},
						"dry_run":        map[string]any{"type": "boolean", "description": "Only simulate merge without writing source files."},
						"delete_removed": map[string]any{"type": "boolean", "description": "Delete source files removed in workspace when safe."},
					},
					"run_key",
				),
			},
			handler:       resourceWorkspaceMergeRun,
			workspaceOnly: true,
		},
		{
			schema: agentcore.DynamicTool{
				Name:        "workspace_abort_run",
				Description: "Abort a workspace run and mark it as aborted in PostgreSQL state.",
				InputSchema: resourceObjectSchema(
					map[string]any{
						"run_key":    map[string]any{"type": "string", "description": "Workspace run key."},
						"updated_by": map[string]any{"type": "string", "description": "Operator id (optional)."},
						"reason":     map[string]any{"type": "string", "description": "Abort reason (optional)."},
					},
					"run_key",
				),
			},
			handler:       resourceWorkspaceAbortRun,
			workspaceOnly: true,
		},
	}
}

func resourceToolCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func resourceJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func resourceDecodeArgs(args json.RawMessage, dst any, op string) string {
	if err := json.Unmarshal(args, dst); err != nil {
		return ToolError(pkgerr.Wrap(err, op, "invalid args"))
	}
	return ""
}

func resourceDecodeArgsOptional(args json.RawMessage, dst any, logMsg string) {
	if err := json.Unmarshal(args, dst); err != nil {
		logger.Debug(logMsg, logger.FieldError, err)
	}
}

func resourceWrapError(err error, op, msg string) string {
	return ToolError(pkgerr.Wrap(err, op, msg))
}

func resourceWorkspaceOps(provider ResourceProvider, op string) (WorkspaceOps, string) {
	if provider == nil {
		return nil, ToolError(pkgerr.New(op, "workspace manager not initialized"))
	}
	ops := provider.WorkspaceOps()
	if ops == nil {
		return nil, ToolError(pkgerr.New(op, "workspace manager not initialized"))
	}
	return ops, ""
}

func resourceWorkspaceDecodeArgs(provider ResourceProvider, args json.RawMessage, dst any, op string) (WorkspaceOps, string) {
	ops, errMsg := resourceWorkspaceOps(provider, op)
	if errMsg != "" { return nil, errMsg }
	if errMsg := resourceDecodeArgs(args, dst, op); errMsg != "" {
		return nil, errMsg
	}
	return ops, ""
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
	if errMsg := resourceDecodeArgs(args, &p, "ResourceTool.CreateDAG"); errMsg != "" {
		return errMsg
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()

	dag, err := provider.DAGManager().SaveDAG(ctx, &TaskDAG{
		DagKey:      p.DagKey,
		Title:       p.Title,
		Description: p.Description,
		Status:      "draft",
	})
	if err != nil {
		return resourceWrapError(err, "ResourceTool.CreateDAG", "create dag")
	}

	nodesCreated := 0
	for _, n := range p.Nodes {
		_, err := provider.DAGManager().SaveNode(ctx, &TaskDAGNode{
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
	return resourceJSON(map[string]any{
		"dag_key":       dag.DagKey,
		"title":         dag.Title,
		"nodes_created": nodesCreated,
		"status":        dag.Status,
	})
}

func resourceTaskGetDAG(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		DagKey string `json:"dag_key"`
	}
	if errMsg := resourceDecodeArgs(args, &p, "ResourceTool.GetDAG"); errMsg != "" {
		return errMsg
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	dag, nodes, err := provider.DAGManager().GetDAGDetail(ctx, p.DagKey)
	if err != nil {
		return resourceWrapError(err, "ResourceTool.GetDAG", "get dag")
	}
	if dag == nil {
		return ToolError(pkgerr.Newf("ResourceTool.GetDAG", "dag %s not found", p.DagKey))
	}

	return resourceJSON(map[string]any{
		"dag":   dag,
		"nodes": nodes,
	})
}

func resourceTaskUpdateNode(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		DagKey  string `json:"dag_key"`
		NodeKey string `json:"node_key"`
		Status  string `json:"status"`
		Result  string `json:"result"`
	}
	if errMsg := resourceDecodeArgs(args, &p, "ResourceTool.UpdateNode"); errMsg != "" {
		return errMsg
	}

	var result any
	if p.Result != "" {
		result = p.Result
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	node, err := provider.DAGManager().UpdateNodeStatus(ctx, p.DagKey, p.NodeKey, p.Status, result)
	if err != nil {
		return resourceWrapError(err, "ResourceTool.UpdateNode", "update node")
	}
	if node == nil {
		return `{"error":"node not found"}`
	}

	logger.Info("resource: node updated", logger.FieldDAG, p.DagKey, logger.FieldNode, p.NodeKey, logger.FieldStatus, p.Status)
	return resourceJSON(node)
}

func resourceCommandList(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		Keyword string `json:"keyword"`
	}
	resourceDecodeArgsOptional(args, &p, "resource: unmarshal command list args")

	ctx, cancel := resourceToolCtx()
	defer cancel()
	cards, err := provider.CommandCardStore().List(ctx, p.Keyword, 50)
	if err != nil {
		return resourceWrapError(err, "ResourceTool.CommandList", "list commands")
	}
	return resourceJSON(cards)
}

func resourceCommandGet(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		CardKey string `json:"card_key"`
	}
	if errMsg := resourceDecodeArgs(args, &p, "ResourceTool.CommandGet"); errMsg != "" {
		return errMsg
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	card, err := provider.CommandCardStore().Get(ctx, p.CardKey)
	if err != nil {
		return resourceWrapError(err, "ResourceTool.CommandGet", "get command")
	}
	if isNilAny(card) {
		return ToolError(pkgerr.Newf("ResourceTool.CommandGet", "command %s not found", p.CardKey))
	}
	return resourceJSON(card)
}

func resourcePromptList(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		Keyword string `json:"keyword"`
	}
	resourceDecodeArgsOptional(args, &p, "resource: unmarshal prompt list args")

	ctx, cancel := resourceToolCtx()
	defer cancel()
	prompts, err := provider.PromptTemplateStore().List(ctx, "", p.Keyword, 50)
	if err != nil {
		return resourceWrapError(err, "ResourceTool.PromptList", "list prompts")
	}
	return resourceJSON(prompts)
}

func resourcePromptGet(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		PromptKey string `json:"prompt_key"`
	}
	if errMsg := resourceDecodeArgs(args, &p, "ResourceTool.PromptGet"); errMsg != "" {
		return errMsg
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	prompt, err := provider.PromptTemplateStore().Get(ctx, p.PromptKey)
	if err != nil {
		return resourceWrapError(err, "ResourceTool.PromptGet", "get prompt")
	}
	if isNilAny(prompt) {
		return ToolError(pkgerr.Newf("ResourceTool.PromptGet", "prompt %s not found", p.PromptKey))
	}
	return resourceJSON(prompt)
}

func resourceSharedFileRead(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		Path string `json:"path"`
	}
	if errMsg := resourceDecodeArgs(args, &p, "ResourceTool.FileRead"); errMsg != "" {
		return errMsg
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	file, err := provider.SharedFileStore().Read(ctx, p.Path)
	if err != nil {
		return resourceWrapError(err, "ResourceTool.FileRead", "read file")
	}
	if isNilAny(file) {
		return ToolError(pkgerr.Newf("ResourceTool.FileRead", "file %s not found", p.Path))
	}
	return resourceJSON(file)
}

func resourceSharedFileWrite(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if errMsg := resourceDecodeArgs(args, &p, "ResourceTool.FileWrite"); errMsg != "" {
		return errMsg
	}
	if strings.TrimSpace(p.Path) == "" {
		return `{"error":"path is required"}`
	}

	ctx, cancel := resourceToolCtx()
	defer cancel()
	file, err := provider.SharedFileStore().Write(ctx, p.Path, p.Content, "agent")
	if err != nil {
		return resourceWrapError(err, "ResourceTool.FileWrite", "write file")
	}

	logger.Info("resource: file written", logger.FieldPath, p.Path, logger.FieldLen, len(p.Content))
	return resourceJSON(file)
}

func resourceWorkspaceCreateRun(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		RunKey     string   `json:"run_key"`
		DagKey     string   `json:"dag_key"`
		SourceRoot string   `json:"source_root"`
		CreatedBy  string   `json:"created_by"`
		Files      []string `json:"files"`
		Metadata   any      `json:"metadata"`
	}
	ops, errMsg := resourceWorkspaceDecodeArgs(provider, args, &p, "ResourceTool.WorkspaceCreate")
	if errMsg != "" { return errMsg }
	ctx, cancel := resourceToolCtx()
	defer cancel()
	run, err := ops.CreateRun(ctx, WorkspaceCreateRunRequest{
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
	runKey := resourceWorkspaceRunKey(run)
	if runKey == "" {
		runKey = p.RunKey
	}
	provider.NotifyEvent("workspace/run/created", map[string]any{
		"runKey": runKey,
		"run":    run,
	})
	return resourceJSON(run)
}

func resourceWorkspaceGetRun(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		RunKey string `json:"run_key"`
	}
	ops, errMsg := resourceWorkspaceDecodeArgs(provider, args, &p, "ResourceTool.WorkspaceGet")
	if errMsg != "" { return errMsg }
	ctx, cancel := resourceToolCtx()
	defer cancel()
	run, err := ops.GetRun(ctx, p.RunKey)
	if err != nil {
		return ToolError(err)
	}
	if isNilAny(run) {
		return ToolError(pkgerr.Newf("ResourceTool.WorkspaceGet", "workspace run %s not found", p.RunKey))
	}
	return resourceJSON(run)
}

func resourceWorkspaceListRuns(provider ResourceProvider, args json.RawMessage) string {
	ops, errMsg := resourceWorkspaceOps(provider, "ResourceTool.WorkspaceList")
	if errMsg != "" { return errMsg }
	var p struct {
		Status string `json:"status"`
		DagKey string `json:"dag_key"`
		Limit  int    `json:"limit"`
	}
	resourceDecodeArgsOptional(args, &p, "resource: unmarshal workspace list args")
	if p.Limit <= 0 || p.Limit > 5000 {
		p.Limit = 200
	}
	ctx, cancel := resourceToolCtx()
	defer cancel()
	runs, err := ops.ListRuns(ctx, p.Status, p.DagKey, p.Limit)
	if err != nil {
		return ToolError(err)
	}
	return resourceJSON(runs)
}

func resourceWorkspaceMergeRun(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		RunKey        string `json:"run_key"`
		UpdatedBy     string `json:"updated_by"`
		DryRun        bool   `json:"dry_run"`
		DeleteRemoved bool   `json:"delete_removed"`
	}
	ops, errMsg := resourceWorkspaceDecodeArgs(provider, args, &p, "ResourceTool.WorkspaceMerge")
	if errMsg != "" { return errMsg }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := ops.MergeRun(ctx, WorkspaceMergeRunRequest{
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
	return resourceJSON(result)
}

func resourceWorkspaceAbortRun(provider ResourceProvider, args json.RawMessage) string {
	var p struct {
		RunKey    string `json:"run_key"`
		UpdatedBy string `json:"updated_by"`
		Reason    string `json:"reason"`
	}
	ops, errMsg := resourceWorkspaceDecodeArgs(provider, args, &p, "ResourceTool.WorkspaceAbort")
	if errMsg != "" { return errMsg }
	ctx, cancel := resourceToolCtx()
	defer cancel()
	run, err := ops.AbortRun(ctx, p.RunKey, p.UpdatedBy, p.Reason)
	if err != nil {
		return ToolError(err)
	}
	provider.NotifyEvent("workspace/run/aborted", map[string]any{
		"runKey": p.RunKey,
		"run":    run,
		"reason": p.Reason,
	})
	return resourceJSON(run)
}

func isNilAny(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func resourceWorkspaceRunKey(run any) string {
	if run == nil {
		return ""
	}
	v := reflect.ValueOf(run)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	f := v.FieldByName("RunKey")
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}
