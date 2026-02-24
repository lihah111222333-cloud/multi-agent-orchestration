// server_dynamic_tools.go — LSP 动态工具: 注册、构建、调用 & 回传。
package apiserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
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

func (s *Server) toolAdapterProviders() tooladapter.Providers {
	return tooladapter.Providers{
		LSP:            s.lspTools,
		CodeRun:        s,
		Approvals:      s,
		Resource:       s,
		Orchestration:  s,
		AgentRuntime:   s,
		Schema:         s,
		Lookup:         s,
		Counter:        s,
		CodeRunTracker: s,
	}
}

func (s *Server) RegisterRuntimeTool(name string, handler tooladapter.RuntimeToolHandler) {
	if s == nil || strings.TrimSpace(name) == "" || handler == nil {
		return
	}
	if s.dynTools == nil {
		s.dynTools = make(map[string]tooladapter.RuntimeToolHandler)
	}
	tooladapter.SetRuntimeTool(s.dynTools, name, handler)
}

func (s *Server) LookupRuntimeTool(name string) (tooladapter.RuntimeToolHandler, bool) {
	if s == nil || s.dynTools == nil {
		return nil, false
	}
	return tooladapter.GetRuntimeTool(s.dynTools, name)
}

func (s *Server) IncrementToolCall(name string) int64 {
	if s == nil {
		return 0
	}
	toolName := strings.TrimSpace(name)
	if toolName == "" {
		return 0
	}
	s.toolCallMu.Lock()
	s.toolCallCount[toolName]++
	count := s.toolCallCount[toolName]
	s.toolCallMu.Unlock()
	return count
}

func (s *Server) RegisterCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string {
	return s.registerCodeRunCancel(agentID, callID, cancel)
}

func (s *Server) UnregisterCodeRunCancel(agentID, runKey string) {
	s.unregisterCodeRunCancel(agentID, runKey)
}

// registerDynamicTools 注册所有动态工具处理函数。
func (s *Server) registerDynamicTools() {
	if s == nil {
		return
	}
	if s.dynTools == nil {
		s.dynTools = make(map[string]tooladapter.RuntimeToolHandler)
	} else {
		for name := range s.dynTools {
			delete(s.dynTools, name)
		}
	}
	tooladapter.Register(s, s.toolAdapterProviders())
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

	start := time.Now()
	var totalCalls int64
	result, dispatchErr := tooladapter.Dispatch(tooladapter.DynamicToolCall{
		AgentID:    agentID,
		Tool:       call.Tool,
		CallID:     call.CallID,
		RequestID:  event.RequestID,
		Arguments:  call.Arguments,
		TotalCalls: &totalCalls,
	}, s.toolAdapterProviders())
	if dispatchErr != nil {
		result = dispatchErr.Error()
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
	notifyPayload := buildToolNotifyPayload(agentID, call, argMap, filePath, success, totalCalls, elapsed, result)
	s.Notify("dynamic-tool/called", notifyPayload)

	// 回传结果: 使用 event.RequestID 发送 JSON-RPC response (codex 发的是 server request)
	if err := s.codexAdapter.SendDynamicToolResult(proc, call.CallID, result, event.RequestID); err != nil {
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
