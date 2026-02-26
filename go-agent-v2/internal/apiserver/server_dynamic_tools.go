// server_dynamic_tools.go — LSP 动态工具: 注册、构建、调用 & 回传。
package apiserver

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func toolAdapterProviders(s *Server) tooladapter.Providers {
	return tooladapter.Providers{
		LSP:            s.lspTools,
		CodeRun:        codeRunProvider{s: s},
		Approvals:      approvalProvider{s: s},
		Resource:       resourceProvider{s: s},
		Orchestration:  orchestrationProvider{s: s},
		AgentRuntime:   agentRuntimeProvider{s: s},
		Schema:         schemaProvider{s: s},
		Lookup:         runtimeLookupProvider{s: s},
		Counter:        toolCallCounterProvider{s: s},
		CodeRunTracker: codeRunTrackerProvider{s: s},
	}
}

// registerDynamicTools 注册所有动态工具处理函数。
func registerDynamicTools(s *Server) {
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
	tooladapter.Register(runtimeRegistryProvider{s: s}, toolAdapterProviders(s))
}

// SetupLSP 初始化 LSP 事件转发: 诊断缓存 + 广播。
func SetupLSP(s *Server, rootDir string) {
	if s == nil {
		return
	}
	if s.lsp == nil {
		return
	}
	if rootDir != "" {
		s.lsp.SetRootURI("file://" + rootDir)
	}
	s.lsp.SetDiagnosticHandler(func(uri string, diagnostics []lsp.Diagnostic) {
		setDiagnostics(s, uri, diagnostics)

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
		notify(s, "lsp/diagnostics/published", map[string]any{
			"uri":         uri,
			"diagnostics": items,
		})
	})
}

func resolveDynamicToolThreadIDs(agentID, rawThreadID string) (threadID, codexThreadID string) {
	agentThreadID := strings.TrimSpace(agentID)
	codexThreadID = strings.TrimSpace(rawThreadID)
	if agentThreadID != "" {
		threadID = agentThreadID
	} else {
		threadID = codexThreadID
	}
	if codexThreadID == "" || codexThreadID == threadID {
		codexThreadID = ""
	}
	return threadID, codexThreadID
}

// handleDynamicToolCall 处理 codex 发回的动态工具调用 — 调 LSP 并回传结果。
func handleDynamicToolCall(s *Server, agentID string, event agentcore.Event) {
	if s == nil {
		return
	}
	// 心跳: 防止 stall 检测在等待 tool 执行期间误杀。
	stopHeartbeat := s.codexAdapter.StartDynamicToolStallHeartbeat(agentID)
	defer stopHeartbeat()

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
		if event.RespondFunc != nil {
			if respErr := event.RespondFunc(-32602, "bad dynamic_tool_call data: "+err.Error()); respErr != nil {
				logger.Warn("app-server: respond error failed", logger.FieldAgentID, agentID, logger.FieldError, respErr)
			}
		} else if event.RequestID != nil {
			if respErr := s.codexAdapter.RespondError(proc, *event.RequestID, -32602, "bad dynamic_tool_call data: "+err.Error()); respErr != nil {
				logger.Warn("app-server: respond error failed", logger.FieldAgentID, agentID, logger.FieldError, respErr)
			}
		}
		return
	}
	threadID, codexThreadID := resolveDynamicToolThreadIDs(agentID, call.ThreadID)

	var argMap map[string]any
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &argMap); err != nil {
			logger.Debug("app-server: unmarshal tool arguments", logger.FieldToolName, call.Tool, logger.FieldError, err)
		}
	}
	filePath := extractToolFilePath(argMap)
	diffTracker := beginDynamicToolDiffTracker(s, agentID, call.Tool, argMap)

	start := time.Now()
	var totalCalls int64
	result, dispatchErr := tooladapter.Dispatch(tooladapter.DynamicToolCall{
		AgentID:    agentID,
		Tool:       call.Tool,
		CallID:     call.CallID,
		RequestID:  event.RequestID,
		Arguments:  call.Arguments,
		TotalCalls: &totalCalls,
	}, toolAdapterProviders(s))
	if dispatchErr != nil {
		result = dispatchErr.Error()
	}

	elapsed := time.Since(start)
	success := toolResultSuccess(result)

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
		s.uiRuntime.IncrActivityStat(threadID, "toolCall", call.Tool)
	}

	// 广播到前端 — 让 UI 可以显示 LSP 调用
	notifyPayload := buildToolNotifyPayload(threadID, agentID, call, argMap, filePath, success, totalCalls, elapsed, result)
	if codexThreadID != "" {
		notifyPayload["codexThreadId"] = codexThreadID
	}
	notify(s, "dynamic-tool/called", notifyPayload)

	if success {
		maybeEmitDynamicToolDiffUpdate(s, threadID, codexThreadID, call.Tool, diffTracker)
	}

	// 回传结果: 优先使用 event 绑定的响应回调，兼容 string/int 两种 JSON-RPC id。
	if event.RespondResultFunc != nil {
		if err := event.RespondResultFunc(dynamicToolCallResultPayload(result)); err != nil {
			logger.Warn("app-server: send tool result failed", logger.FieldAgentID, agentID, logger.FieldToolName, call.Tool, logger.FieldError, err)
		}
		return
	}
	if err := s.codexAdapter.SendDynamicToolResult(proc, call.CallID, result, event.RequestID); err != nil {
		logger.Warn("app-server: send tool result failed", logger.FieldAgentID, agentID, logger.FieldToolName, call.Tool, logger.FieldError, err)
	}
}

func extractToolFilePath(args map[string]any) string {
	return extractStringArg(args, "file_path", "path", "file")
}

func buildToolNotifyPayload(
	threadID string,
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
		"threadId":   threadID,
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

func dynamicToolCallResultPayload(output string) map[string]any {
	return map[string]any{
		"contentItems": []map[string]any{{
			"type": "inputText",
			"text": output,
		}},
		"success": true,
	}
}
