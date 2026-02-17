// resource_tools.go — 资源类动态工具 (task DAG, 命令卡, 提示词模板, 共享文件)。
//
// 通过 Dynamic Tool 注入机制暴露给 codex agent,
// 使 agent 能操作编排基础数据。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

// buildResourceTools 返回资源类工具定义 (注入 codex agent)。
func (s *Server) buildResourceTools() []codex.DynamicTool {
	// 没有 DB 连接时不暴露资源工具
	if s.dagStore == nil {
		return nil
	}
	return []codex.DynamicTool{
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
	}
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
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}

	ctx := context.Background()

	// 创建 DAG
	dag, err := s.dagStore.SaveDAG(ctx, &store.TaskDAG{
		DagKey:      p.DagKey,
		Title:       p.Title,
		Description: p.Description,
		Status:      "draft",
	})
	if err != nil {
		return fmt.Sprintf(`{"error":"create dag: %s"}`, err.Error())
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
			slog.Warn("resource: save node failed", "dag", p.DagKey, "node", n.NodeKey, "error", err)
			continue
		}
		nodesCreated++
	}

	slog.Info("resource: DAG created", "dag_key", p.DagKey, "nodes", nodesCreated)
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
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}

	dag, nodes, err := s.dagStore.GetDAGDetail(context.Background(), p.DagKey)
	if err != nil {
		return fmt.Sprintf(`{"error":"get dag: %s"}`, err.Error())
	}
	if dag == nil {
		return fmt.Sprintf(`{"error":"dag %s not found"}`, p.DagKey)
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
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}

	var result any
	if p.Result != "" {
		result = p.Result
	}

	node, err := s.dagStore.UpdateNodeStatus(context.Background(), p.DagKey, p.NodeKey, p.Status, result)
	if err != nil {
		return fmt.Sprintf(`{"error":"update node: %s"}`, err.Error())
	}
	if node == nil {
		return `{"error":"node not found"}`
	}

	slog.Info("resource: node updated", "dag", p.DagKey, "node", p.NodeKey, "status", p.Status)
	data, _ := json.Marshal(node)
	return string(data)
}

func (s *Server) resourceCommandList(args json.RawMessage) string {
	var p struct {
		Keyword string `json:"keyword"`
	}
	_ = json.Unmarshal(args, &p) // keyword 可选

	cards, err := s.cmdStore.List(context.Background(), p.Keyword, 50)
	if err != nil {
		return fmt.Sprintf(`{"error":"list commands: %s"}`, err.Error())
	}
	data, _ := json.Marshal(cards)
	return string(data)
}

func (s *Server) resourceCommandGet(args json.RawMessage) string {
	var p struct {
		CardKey string `json:"card_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}

	card, err := s.cmdStore.Get(context.Background(), p.CardKey)
	if err != nil {
		return fmt.Sprintf(`{"error":"get command: %s"}`, err.Error())
	}
	if card == nil {
		return fmt.Sprintf(`{"error":"command %s not found"}`, p.CardKey)
	}
	data, _ := json.Marshal(card)
	return string(data)
}

func (s *Server) resourcePromptList(args json.RawMessage) string {
	var p struct {
		Keyword string `json:"keyword"`
	}
	_ = json.Unmarshal(args, &p)

	prompts, err := s.promptStore.List(context.Background(), "", p.Keyword, 50)
	if err != nil {
		return fmt.Sprintf(`{"error":"list prompts: %s"}`, err.Error())
	}
	data, _ := json.Marshal(prompts)
	return string(data)
}

func (s *Server) resourcePromptGet(args json.RawMessage) string {
	var p struct {
		PromptKey string `json:"prompt_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}

	prompt, err := s.promptStore.Get(context.Background(), p.PromptKey)
	if err != nil {
		return fmt.Sprintf(`{"error":"get prompt: %s"}`, err.Error())
	}
	if prompt == nil {
		return fmt.Sprintf(`{"error":"prompt %s not found"}`, p.PromptKey)
	}
	data, _ := json.Marshal(prompt)
	return string(data)
}

func (s *Server) resourceSharedFileRead(args json.RawMessage) string {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}

	file, err := s.fileStore.Read(context.Background(), p.Path)
	if err != nil {
		return fmt.Sprintf(`{"error":"read file: %s"}`, err.Error())
	}
	if file == nil {
		return fmt.Sprintf(`{"error":"file %s not found"}`, p.Path)
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
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}
	if p.Path == "" || p.Content == "" {
		return `{"error":"path and content are required"}`
	}

	file, err := s.fileStore.Write(context.Background(), p.Path, p.Content, "agent")
	if err != nil {
		return fmt.Sprintf(`{"error":"write file: %s"}`, err.Error())
	}

	slog.Info("resource: file written", "path", p.Path, "len", len(p.Content))
	data, _ := json.Marshal(file)
	return string(data)
}
