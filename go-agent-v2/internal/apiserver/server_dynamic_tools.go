// server_dynamic_tools.go — LSP 动态工具: 注册、构建、调用 & 回传。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func defaultSkillsCacheDir() string {
	ensureLocalFallback := func(path string) string {
		if err := os.MkdirAll(path, 0o755); err != nil {
			logger.Warn("skills directory: ensure local fallback failed", logger.FieldError, err, logger.FieldPath, path)
		}
		return path
	}
	localFallback := filepath.Join(".multi-agent", "skills-cache")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Warn("skills directory: resolve user home failed, fallback to local path",
			logger.FieldError, err,
		)
		return ensureLocalFallback(localFallback)
	}
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		logger.Warn("skills directory: user home empty, fallback to local path")
		return ensureLocalFallback(localFallback)
	}

	appRootDir := filepath.Join(homeDir, ".multi-agent")
	if err := os.MkdirAll(appRootDir, 0o755); err != nil {
		logger.Warn("skills directory: ensure app root failed, fallback to local path",
			logger.FieldError, err,
			logger.FieldPath, appRootDir,
		)
		return ensureLocalFallback(localFallback)
	}
	cacheDir := filepath.Join(appRootDir, "skills-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		logger.Warn("skills directory: ensure cache dir failed, fallback to local path",
			logger.FieldError, err,
			logger.FieldPath, cacheDir,
		)
		return ensureLocalFallback(localFallback)
	}
	return cacheDir
}

// registerDynamicTools 注册所有动态工具处理函数。
//
// 新增工具只需一行: s.dynTools["tool_name"] = s.toolHandler
func (s *Server) registerDynamicTools() {
	// LSP 工具
	s.dynTools["lsp_hover"] = s.lspTools.Hover
	s.dynTools["lsp_open_file"] = s.lspTools.OpenFile
	s.dynTools["lsp_diagnostics"] = s.lspTools.Diagnostics
	s.dynTools["lsp_definition"] = s.lspTools.Definition
	s.dynTools["lsp_references"] = s.lspTools.References
	s.dynTools["lsp_document_symbol"] = s.lspTools.DocumentSymbol
	s.dynTools["lsp_rename"] = s.lspTools.Rename
	s.dynTools["lsp_completion"] = s.lspTools.Completion
	s.dynTools["lsp_did_change"] = s.lspTools.DidChange
	s.registerExtendedLSPDynamicTools()

	// 编排工具
	s.dynTools["orchestration_list_agents"] = func(_ json.RawMessage) string { return s.orchestrationListAgents() }
	s.dynTools["orchestration_send_message"] = s.orchestrationSendMessage
	s.dynTools["orchestration_launch_agent"] = s.orchestrationLaunchAgent
	s.dynTools["orchestration_stop_agent"] = s.orchestrationStopAgent

	// 资源工具
	s.dynTools["task_create_dag"] = s.resourceTaskCreateDAG
	s.dynTools["task_get_dag"] = s.resourceTaskGetDAG
	s.dynTools["task_update_node"] = s.resourceTaskUpdateNode
	s.dynTools["command_list"] = s.resourceCommandList
	s.dynTools["command_get"] = s.resourceCommandGet
	s.dynTools["prompt_list"] = s.resourcePromptList
	s.dynTools["prompt_get"] = s.resourcePromptGet
	s.dynTools["shared_file_read"] = s.resourceSharedFileRead
	s.dynTools["shared_file_write"] = s.resourceSharedFileWrite
	s.dynTools["workspace_create_run"] = s.resourceWorkspaceCreateRun
	s.dynTools["workspace_get_run"] = s.resourceWorkspaceGetRun
	s.dynTools["workspace_list_runs"] = s.resourceWorkspaceListRuns
	s.dynTools["workspace_merge_run"] = s.resourceWorkspaceMergeRun
	s.dynTools["workspace_abort_run"] = s.resourceWorkspaceAbortRun
}

// SetupLSP 初始化 LSP 事件转发: 诊断缓存 + 广播。
func (s *Server) SetupLSP(rootDir string) {
	if s.lsp == nil {
		return
	}
	if rootDir != "" {
		s.lsp.SetRootURI("file://" + rootDir)
	}
	s.lsp.SetDiagnosticHandler(func(uri string, diagnostics []lsp.Diagnostic) {
		s.SetDiagnostics(uri, diagnostics)

		// 广播诊断通知给前端
		items := make([]map[string]any, 0, len(diagnostics))
		for _, d := range diagnostics {
			items = append(items, map[string]any{
				"message":  d.Message,
				"severity": d.Severity.String(),
				"line":     d.Range.Start.Line,
				"column":   d.Range.Start.Character,
			})
		}
		s.Notify("lsp/diagnostics/published", map[string]any{
			"uri":         uri,
			"diagnostics": items,
		})
	})
}

// buildLSPDynamicTools 构建 LSP 动态工具列表 (注入 codex agent)。
func (s *Server) buildLSPDynamicTools() []agentcore.DynamicTool {
	if s.lsp == nil {
		logger.Info("lsp dynamic tools disabled: lsp manager is not initialized")
		return nil
	}
	statuses := s.lsp.Statuses()
	hasAvailableServer := false
	for _, st := range statuses {
		if st.Available {
			hasAvailableServer = true
			break
		}
	}
	if !hasAvailableServer {
		logger.Info("lsp dynamic tools disabled: no language server available on PATH")
		return nil
	}
	tools := []agentcore.DynamicTool{
		{
			Name:        "lsp_hover",
			Description: "Get type info and documentation for a symbol at a specific position in a file via LSP hover.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_open_file",
			Description: "Open a file for LSP analysis. Triggers didOpen and starts diagnostics. Call before hover/diagnostics for accurate results.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "lsp_diagnostics",
			Description: "Get current diagnostics (errors, warnings) for a file. If file_path is provided and the file was not opened, it will be auto-synchronized first.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file. Empty = all files."},
				},
			},
		},
		{
			Name:        "lsp_definition",
			Description: "Go to definition. Returns the location(s) where a symbol is defined. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_references",
			Description: "Find all references to a symbol. Returns locations where the symbol is used. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path":           map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":                map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":              map[string]any{"type": "number", "description": "0-indexed column number"},
					"include_declaration": map[string]any{"type": "boolean", "description": "Include the declaration in results (default: true)"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_document_symbol",
			Description: "Get file outline (all symbols: functions, types, methods, constants). Returns a hierarchical symbol tree. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "lsp_rename",
			Description: "Rename a symbol across all files. Returns all edits needed. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
					"new_name":  map[string]any{"type": "string", "description": "New name for the symbol"},
				},
				"required": []string{"file_path", "line", "column", "new_name"},
			},
		},
		{
			Name:        "lsp_completion",
			Description: "Get code completion suggestions at a position. Returns candidate items with labels and kinds. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_did_change",
			Description: "Notify the language server that file content has changed. Use after editing a file to keep LSP in sync. Supports unopened files via automatic bootstrap and fail-closed sync.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path":   map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"new_content": map[string]any{"type": "string", "description": "Full new content of the file"},
					"version":     map[string]any{"type": "number", "description": "Document version (increment each change, default: 2)"},
				},
				"required": []string{"file_path", "new_content"},
			},
		},
	}
	tools = append(tools, s.buildExtendedLSPDynamicTools()...)
	return tools
}

// handleDynamicToolCall 处理 codex 发回的动态工具调用 — 调 LSP 并回传结果。
func (s *Server) handleDynamicToolCall(agentID string, event agentcore.Event) {
	// 心跳: 防止 stall 检测在等待 tool 执行期间误杀
	// 使用 stallThreshold/6 而非 stallHeartbeat，确保在 stall 阈值内多次 touch。
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	hbInterval := s.stallThreshold / 6
	if hbInterval <= 0 {
		hbInterval = defaultStallThreshold / 6
	}
	if hbInterval < 10*time.Second {
		hbInterval = 10 * time.Second
	}
	util.SafeGo(func() {
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.touchTrackedTurnLastEvent(agentID)
			case <-heartbeatDone:
				return
			}
		}
	})

	// 先查找 proc — 后续的所有错误路径都需要通过 codexAdapter 回传错误。
	proc := s.mgr.Get(agentID)
	if proc == nil {
		logger.Error("app-server: dynamic_tool_call dropped — agent gone",
			logger.FieldAgentID, agentID)
		if event.RespondFunc != nil {
			if respondErr := event.RespondFunc(-32603, "agent not found: "+agentID); respondErr != nil {
				logger.Warn("app-server: RespondFunc failed on agent-gone",
					logger.FieldAgentID, agentID, logger.FieldError, respondErr)
			}
		}
		return
	}

	// codex 事件信封: {"id": "...", "msg": {DynamicToolCallParams}, "conversationId": "..."}
	// 先提取 msg 字段, 再解析工具调用参数。
	var envelope struct {
		Msg json.RawMessage `json:"msg"`
	}
	raw := event.Data
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Msg) > 0 {
		raw = envelope.Msg
	}

	var call agentcore.DynamicToolCallData
	if err := json.Unmarshal(raw, &call); err != nil {
		logger.Warn("app-server: bad dynamic_tool_call data", logger.FieldAgentID, agentID, logger.FieldError, err,
			"raw", string(event.Data))
		// 必须回复 error response，否则 codex turn 永挂。
		if event.RequestID != nil {
			if respErr := s.codexAdapter.RespondError(proc, *event.RequestID, -32602, "bad dynamic_tool_call data: "+err.Error()); respErr != nil {
				logger.Warn("app-server: respond error failed", logger.FieldAgentID, agentID, logger.FieldError, respErr)
			}
		}
		return
	}

	// ── 可观测性: 计数 + 日志 ──
	start := time.Now()
	s.toolCallMu.Lock()
	s.toolCallCount[call.Tool]++
	count := s.toolCallCount[call.Tool]
	s.toolCallMu.Unlock()

	logger.Info("dynamic-tool: called",
		logger.FieldAgentID, agentID,
		logger.FieldToolName, call.Tool,
		"call_id", call.CallID,
		"total_calls", count,
	)

	var result string

	if call.Tool == "orchestration_send_message" {
		result = s.orchestrationSendMessageFrom(agentID, call.Arguments)
	} else if call.Tool == "code_run" {
		// code_run / code_run_test: 需要 agentID + callID, 在此硬编码分支。
		resolvedCallID := resolveCodeRunCallID(call.CallID, event.RequestID)
		result = func() string {
			execCtx, execCancel := context.WithCancel(context.Background())
			runKey := s.registerCodeRunCancel(agentID, resolvedCallID, execCancel)
			defer func() {
				s.unregisterCodeRunCancel(agentID, runKey)
				execCancel()
			}()
			return s.codeRunWithAgent(execCtx, agentID, resolvedCallID, call.Arguments)
		}()
	} else if call.Tool == "code_run_test" {
		resolvedCallID := resolveCodeRunCallID(call.CallID, event.RequestID)
		result = func() string {
			execCtx, execCancel := context.WithCancel(context.Background())
			runKey := s.registerCodeRunCancel(agentID, resolvedCallID, execCancel)
			defer func() {
				s.unregisterCodeRunCancel(agentID, runKey)
				execCancel()
			}()
			return s.codeRunTestWithAgent(execCtx, agentID, resolvedCallID, call.Arguments)
		}()
	} else if handler, ok := s.dynTools[call.Tool]; ok {
		result = handler(call.Arguments)
	} else {
		result = fmt.Sprintf("unknown tool: %s", call.Tool)
	}

	elapsed := time.Since(start)
	success := toolResultSuccess(result)

	var argMap map[string]any
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &argMap); err != nil {
			logger.Debug("app-server: unmarshal tool arguments", logger.FieldToolName, call.Tool, logger.FieldError, err)
		}
	}
	filePath := extractToolFilePath(argMap)

	logger.Info("dynamic-tool: completed",
		logger.FieldSource, "codex",
		logger.FieldComponent, "tool_call",
		logger.FieldAgentID, agentID,
		logger.FieldToolName, call.Tool,
		logger.FieldDurationMS, elapsed.Milliseconds(),
		logger.FieldEventType, "dynamic_tool_call",
		"result_len", len(result),
		"success", success,
	)

	// 递增活动统计 (lsp_ 前缀工具会自动累加到 lspCalls)
	if s.uiRuntime != nil {
		s.uiRuntime.IncrActivityStat(agentID, "toolCall", call.Tool)
	}

	// 广播到前端 — 让 UI 可以显示 LSP 调用
	notifyPayload := buildToolNotifyPayload(agentID, call, argMap, filePath, success, count, elapsed, result)
	s.Notify("dynamic-tool/called", notifyPayload)

	// 回传结果: 使用 event.RequestID 发送 JSON-RPC response (codex 发的是 server request)
	if err := s.codexAdapter.SendDynamicToolResult(proc, call.CallID, result, event.RequestID); err != nil {
		logger.Warn("app-server: send tool result failed", logger.FieldAgentID, agentID, logger.FieldToolName, call.Tool, logger.FieldError, err)
	}
}

func resolveCodeRunCallID(callID string, requestID *int64) string {
	trimmed := strings.TrimSpace(callID)
	if trimmed != "" {
		return trimmed
	}
	if requestID != nil {
		return fmt.Sprintf("req-%d", *requestID)
	}
	return ""
}

func extractToolFilePath(args map[string]any) string {
	if args == nil {
		return ""
	}
	for _, key := range []string{"file_path", "path", "file"} {
		value, ok := args[key].(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func buildToolNotifyPayload(
	agentID string,
	call agentcore.DynamicToolCallData,
	argMap map[string]any,
	filePath string,
	success bool,
	count int64,
	elapsed time.Duration,
	result string,
) map[string]any {
	payload := map[string]any{
		"threadId":   agentID,
		"agent":      agentID,
		"tool":       call.Tool,
		"callId":     call.CallID,
		"arguments":  argMap,
		"file":       filePath,
		"success":    success,
		"totalCalls": count,
		"elapsedMs":  elapsed.Milliseconds(),
		"resultLen":  len(result),
	}
	if result == "" {
		return payload
	}
	if len(result) > 500 {
		payload["resultPreview"] = result[:500]
		return payload
	}
	payload["resultPreview"] = result
	return payload
}
