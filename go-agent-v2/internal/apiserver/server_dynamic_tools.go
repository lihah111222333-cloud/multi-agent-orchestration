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

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func (s *Server) skillsDirectory() string {
	dir := strings.TrimSpace(s.skillsDir)
	if dir == "" {
		return defaultSkillsCacheDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("skills directory: ensure custom dir failed, fallback to app cache",
			logger.FieldError, err,
			logger.FieldPath, dir,
		)
		return defaultSkillsCacheDir()
	}
	return dir
}

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
	s.dynTools["lsp_hover"] = s.lspHover
	s.dynTools["lsp_open_file"] = s.lspOpenFile
	s.dynTools["lsp_diagnostics"] = s.lspDiagnostics
	s.dynTools["lsp_definition"] = s.lspDefinition
	s.dynTools["lsp_references"] = s.lspReferences
	s.dynTools["lsp_document_symbol"] = s.lspDocumentSymbol
	s.dynTools["lsp_rename"] = s.lspRename
	s.dynTools["lsp_completion"] = s.lspCompletion
	s.dynTools["lsp_did_change"] = s.lspDidChange
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
		s.diagMu.Lock()
		if len(diagnostics) == 0 {
			delete(s.diagCache, uri)
		} else {
			s.diagCache[uri] = diagnostics
		}
		s.diagMu.Unlock()

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
func (s *Server) buildLSPDynamicTools() []codex.DynamicTool {
	if s.lsp == nil {
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
	tools := []codex.DynamicTool{
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
func (s *Server) handleDynamicToolCall(agentID string, event codex.Event) {
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

	// 先查找 proc — 后续的所有错误路径都需要 proc.Client.RespondError 回传。
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

	var call codex.DynamicToolCallData
	if err := json.Unmarshal(raw, &call); err != nil {
		logger.Warn("app-server: bad dynamic_tool_call data", logger.FieldAgentID, agentID, logger.FieldError, err,
			"raw", string(event.Data))
		// 必须回复 error response，否则 codex turn 永挂。
		if event.RequestID != nil {
			if respErr := proc.Client.RespondError(*event.RequestID, -32602, "bad dynamic_tool_call data: "+err.Error()); respErr != nil {
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
	if err := proc.Client.SendDynamicToolResult(call.CallID, result, event.RequestID); err != nil {
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
	call codex.DynamicToolCallData,
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

// lspHover 调用 LSP hover。
func (s *Server) lspHover(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	result, err := s.lsp.Hover(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if result == nil {
		return "no hover info available"
	}
	return result.Contents.Value
}

// lspOpenFile 打开文件触发 LSP 分析。
func (s *Server) lspOpenFile(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	p.FilePath = strings.TrimSpace(p.FilePath)
	if p.FilePath == "" {
		return "error: file_path is required"
	}
	content, err := os.ReadFile(p.FilePath)
	if err != nil {
		return "error: reading file: " + err.Error()
	}
	if err := s.lsp.OpenFile(p.FilePath, string(content)); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("opened %s (%d bytes)", p.FilePath, len(content))
}

// lspDiagnostics 返回文件诊断信息。
func (s *Server) lspDiagnostics(args json.RawMessage) string {
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: unmarshal diagnostics params: " + err.Error()
	}
	p.FilePath = strings.TrimSpace(p.FilePath)

	if p.FilePath != "" && s.lsp != nil {
		if err := s.lsp.BootstrapDocument(p.FilePath); err != nil {
			return "error: " + err.Error()
		}
		// 诊断由 server→client 通知异步到达，短暂等待一次以提高首查命中率。
		time.Sleep(120 * time.Millisecond)
	}

	s.diagMu.RLock()
	defer s.diagMu.RUnlock()

	if p.FilePath != "" {
		uri := p.FilePath
		if !strings.HasPrefix(uri, "file://") {
			abs, _ := filepath.Abs(uri)
			uri = "file://" + abs
		}
		diags, ok := s.diagCache[uri]
		if !ok || len(diags) == 0 {
			return "no diagnostics"
		}
		var sb strings.Builder
		for _, d := range diags {
			fmt.Fprintf(&sb, "%s:%d:%d %s\n", p.FilePath, d.Range.Start.Line+1, d.Range.Start.Character, d.Message)
		}
		return sb.String()
	}

	// 所有文件
	if len(s.diagCache) == 0 {
		return "no diagnostics"
	}
	var sb strings.Builder
	for uri, diags := range s.diagCache {
		for _, d := range diags {
			fmt.Fprintf(&sb, "%s:%d:%d %s\n", uri, d.Range.Start.Line+1, d.Range.Start.Character, d.Message)
		}
	}
	return sb.String()
}

// lspDefinition 跳转定义。
func (s *Server) lspDefinition(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	locs, err := s.lsp.Definition(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(locs) == 0 {
		return "no definition found"
	}
	data, _ := json.Marshal(locs)
	return string(data)
}

// lspReferences 查找引用。
func (s *Server) lspReferences(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath    string `json:"file_path"`
		Line        int    `json:"line"`
		Column      int    `json:"column"`
		IncludeDecl *bool  `json:"include_declaration"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	includeDecl := true
	if p.IncludeDecl != nil {
		includeDecl = *p.IncludeDecl
	}
	locs, err := s.lsp.References(p.FilePath, p.Line, p.Column, includeDecl)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(locs) == 0 {
		return "no references found"
	}
	data, _ := json.Marshal(locs)
	return string(data)
}

// lspDocumentSymbol 文件大纲。
func (s *Server) lspDocumentSymbol(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	symbols, err := s.lsp.DocumentSymbol(p.FilePath)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(symbols) == 0 {
		return "no symbols found"
	}
	data, _ := json.Marshal(symbols)
	return string(data)
}

// lspRename 重命名符号。
func (s *Server) lspRename(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
		NewName  string `json:"new_name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	if strings.TrimSpace(p.NewName) == "" {
		return "error: new_name is required"
	}
	edit, err := s.lsp.Rename(p.FilePath, p.Line, p.Column, p.NewName)
	if err != nil {
		return "error: " + err.Error()
	}
	if edit == nil || (len(edit.Changes) == 0 && len(edit.DocumentChanges) == 0) {
		return "no edits produced"
	}
	data, _ := json.Marshal(edit)
	return string(data)
}

// lspCompletion 代码补全。
func (s *Server) lspCompletion(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	items, err := s.lsp.Completion(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(items) == 0 {
		return "no completions"
	}
	// 限制返回数量避免 token 爆炸
	if len(items) > 50 {
		items = items[:50]
	}
	data, _ := json.Marshal(items)
	return string(data)
}

// lspDidChange 通知文件内容变更。
func (s *Server) lspDidChange(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath   string `json:"file_path"`
		NewContent string `json:"new_content"`
		Version    int    `json:"version"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	if p.Version == 0 {
		p.Version = 2
	}
	if err := s.lsp.ChangeFile(p.FilePath, p.Version, p.NewContent); err != nil {
		return "error: " + err.Error()
	}
	return "ok: file content updated"
}
