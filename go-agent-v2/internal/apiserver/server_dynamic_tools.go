// server_dynamic_tools.go — LSP 动态工具: 注册、构建、调用 & 回传。
package apiserver

import (
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
	return []codex.DynamicTool{
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
			Description: "Get current diagnostics (errors, warnings) for a file. The file should be opened with lsp_open_file first.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file. Empty = all files."},
				},
			},
		},
	}
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
