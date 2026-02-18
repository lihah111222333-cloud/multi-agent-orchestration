// resource_tools.go — 资源类动态工具 (task DAG, 命令卡, 提示词模板, 共享文件)。
//
// 通过 Dynamic Tool 注入机制暴露给 codex agent,
// 使 agent 能操作编排基础数据。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// buildResourceTools 返回资源类工具定义 (注入 codex agent)。
func (s *Server) buildResourceTools() []codex.DynamicTool {
	// 没有 DB 连接时不暴露资源工具
	if s.dagStore == nil {
		return nil
	}
	tools := []codex.DynamicTool{
		// ── Task DAG ──
		{
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
		{
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
		{
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

		// ── 命令卡 ──
		{
			Name:        "command_list",
			Description: "List available command cards. Command cards define reusable operations with templates and argument schemas.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{"type": "string", "description": "Search keyword (optional)"},
				},
			},
		},
		{
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

		// ── 提示词模板 ──
		{
			Name:        "prompt_list",
			Description: "List available prompt templates. Templates can be used to generate structured prompts for agents.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{"type": "string", "description": "Search keyword (optional)"},
				},
			},
		},
		{
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

		// ── 共享文件 ──
		{
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
		{
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

		// ── Workspace Run (双通道: 虚拟目录 + PG 状态) ──
		{
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
		{
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
		{
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
		{
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
		{
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
	}

	// workspace manager 未启用时，隐藏 workspace_* 工具定义，避免暴露不可用能力。
	if s.workspaceMgr == nil {
		filtered := make([]codex.DynamicTool, 0, len(tools))
		for _, tool := range tools {
			if strings.HasPrefix(tool.Name, "workspace_") {
				continue
			}
			filtered = append(filtered, tool)
		}
		return filtered
	}

	return tools
}

// ========================================
// Resource Tool Handlers
// ========================================

func (s *Server) resourceTaskCreateDAG(args json.RawMessage) string {
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
		return toolError(fmt.Errorf("invalid args: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 创建 DAG
	dag, err := s.dagStore.SaveDAG(ctx, &store.TaskDAG{
		DagKey:      p.DagKey,
		Title:       p.Title,
		Description: p.Description,
		Status:      "draft",
	})
	if err != nil {
		return toolError(fmt.Errorf("create dag: %w", err))
	}

	// 创建节点
	nodesCreated := 0
	for _, n := range p.Nodes {
		_, err := s.dagStore.SaveNode(ctx, &store.TaskDAGNode{
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

func (s *Server) resourceTaskGetDAG(args json.RawMessage) string {
	var p struct {
		DagKey string `json:"dag_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dag, nodes, err := s.dagStore.GetDAGDetail(ctx, p.DagKey)
	if err != nil {
		return toolError(fmt.Errorf("get dag: %w", err))
	}
	if dag == nil {
		return toolError(fmt.Errorf("dag %s not found", p.DagKey))
	}

	data, _ := json.Marshal(map[string]any{
		"dag":   dag,
		"nodes": nodes,
	})
	return string(data)
}

func (s *Server) resourceTaskUpdateNode(args json.RawMessage) string {
	var p struct {
		DagKey  string `json:"dag_key"`
		NodeKey string `json:"node_key"`
		Status  string `json:"status"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}

	var result any
	if p.Result != "" {
		result = p.Result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	node, err := s.dagStore.UpdateNodeStatus(ctx, p.DagKey, p.NodeKey, p.Status, result)
	if err != nil {
		return toolError(fmt.Errorf("update node: %w", err))
	}
	if node == nil {
		return `{"error":"node not found"}`
	}

	logger.Info("resource: node updated", logger.FieldDAG, p.DagKey, logger.FieldNode, p.NodeKey, logger.FieldStatus, p.Status)
	data, _ := json.Marshal(node)
	return string(data)
}

func (s *Server) resourceCommandList(args json.RawMessage) string {
	var p struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		logger.Debug("resource: unmarshal command list args", logger.FieldError, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cards, err := s.cmdStore.List(ctx, p.Keyword, 50)
	if err != nil {
		return toolError(fmt.Errorf("list commands: %w", err))
	}
	data, _ := json.Marshal(cards)
	return string(data)
}

func (s *Server) resourceCommandGet(args json.RawMessage) string {
	var p struct {
		CardKey string `json:"card_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	card, err := s.cmdStore.Get(ctx, p.CardKey)
	if err != nil {
		return toolError(fmt.Errorf("get command: %w", err))
	}
	if card == nil {
		return toolError(fmt.Errorf("command %s not found", p.CardKey))
	}
	data, _ := json.Marshal(card)
	return string(data)
}

func (s *Server) resourcePromptList(args json.RawMessage) string {
	var p struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		logger.Debug("resource: unmarshal prompt list args", logger.FieldError, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prompts, err := s.promptStore.List(ctx, "", p.Keyword, 50)
	if err != nil {
		return toolError(fmt.Errorf("list prompts: %w", err))
	}
	data, _ := json.Marshal(prompts)
	return string(data)
}

func (s *Server) resourcePromptGet(args json.RawMessage) string {
	var p struct {
		PromptKey string `json:"prompt_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prompt, err := s.promptStore.Get(ctx, p.PromptKey)
	if err != nil {
		return toolError(fmt.Errorf("get prompt: %w", err))
	}
	if prompt == nil {
		return toolError(fmt.Errorf("prompt %s not found", p.PromptKey))
	}
	data, _ := json.Marshal(prompt)
	return string(data)
}

func (s *Server) resourceSharedFileRead(args json.RawMessage) string {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file, err := s.fileStore.Read(ctx, p.Path)
	if err != nil {
		return toolError(fmt.Errorf("read file: %w", err))
	}
	if file == nil {
		return toolError(fmt.Errorf("file %s not found", p.Path))
	}
	data, _ := json.Marshal(file)
	return string(data)
}

func (s *Server) resourceSharedFileWrite(args json.RawMessage) string {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}
	if p.Path == "" || p.Content == "" {
		return `{"error":"path and content are required"}`
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file, err := s.fileStore.Write(ctx, p.Path, p.Content, "agent")
	if err != nil {
		return toolError(fmt.Errorf("write file: %w", err))
	}

	logger.Info("resource: file written", logger.FieldPath, p.Path, logger.FieldLen, len(p.Content))
	data, _ := json.Marshal(file)
	return string(data)
}

func (s *Server) resourceWorkspaceCreateRun(args json.RawMessage) string {
	if s.workspaceMgr == nil {
		return toolError(fmt.Errorf("workspace manager not initialized"))
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
		return toolError(fmt.Errorf("invalid args: %w", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, err := s.workspaceMgr.CreateRun(ctx, service.WorkspaceCreateRequest{
		RunKey:     p.RunKey,
		DagKey:     p.DagKey,
		SourceRoot: p.SourceRoot,
		CreatedBy:  p.CreatedBy,
		Files:      p.Files,
		Metadata:   p.Metadata,
	})
	if err != nil {
		return toolError(err)
	}
	s.Notify("workspace/run/created", map[string]any{
		"runKey": run.RunKey,
		"run":    run,
	})
	data, _ := json.Marshal(run)
	return string(data)
}

func (s *Server) resourceWorkspaceGetRun(args json.RawMessage) string {
	if s.workspaceMgr == nil {
		return toolError(fmt.Errorf("workspace manager not initialized"))
	}
	var p struct {
		RunKey string `json:"run_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, err := s.workspaceMgr.GetRun(ctx, p.RunKey)
	if err != nil {
		return toolError(err)
	}
	if run == nil {
		return toolError(fmt.Errorf("workspace run %s not found", p.RunKey))
	}
	data, _ := json.Marshal(run)
	return string(data)
}

func (s *Server) resourceWorkspaceListRuns(args json.RawMessage) string {
	if s.workspaceMgr == nil {
		return toolError(fmt.Errorf("workspace manager not initialized"))
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runs, err := s.workspaceMgr.ListRuns(ctx, p.Status, p.DagKey, p.Limit)
	if err != nil {
		return toolError(err)
	}
	data, _ := json.Marshal(runs)
	return string(data)
}

func (s *Server) resourceWorkspaceMergeRun(args json.RawMessage) string {
	if s.workspaceMgr == nil {
		return toolError(fmt.Errorf("workspace manager not initialized"))
	}
	var p struct {
		RunKey        string `json:"run_key"`
		UpdatedBy     string `json:"updated_by"`
		DryRun        bool   `json:"dry_run"`
		DeleteRemoved bool   `json:"delete_removed"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := s.workspaceMgr.MergeRun(ctx, service.WorkspaceMergeRequest{
		RunKey:        p.RunKey,
		UpdatedBy:     p.UpdatedBy,
		DryRun:        p.DryRun,
		DeleteRemoved: p.DeleteRemoved,
	})
	if err != nil {
		return toolError(err)
	}
	s.Notify("workspace/run/merged", map[string]any{
		"runKey": p.RunKey,
		"result": result,
	})
	data, _ := json.Marshal(result)
	return string(data)
}

func (s *Server) resourceWorkspaceAbortRun(args json.RawMessage) string {
	if s.workspaceMgr == nil {
		return toolError(fmt.Errorf("workspace manager not initialized"))
	}
	var p struct {
		RunKey    string `json:"run_key"`
		UpdatedBy string `json:"updated_by"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(fmt.Errorf("invalid args: %w", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, err := s.workspaceMgr.AbortRun(ctx, p.RunKey, p.UpdatedBy, p.Reason)
	if err != nil {
		return toolError(err)
	}
	s.Notify("workspace/run/aborted", map[string]any{
		"runKey": p.RunKey,
		"run":    run,
		"reason": p.Reason,
	})
	data, _ := json.Marshal(run)
	return string(data)
}
