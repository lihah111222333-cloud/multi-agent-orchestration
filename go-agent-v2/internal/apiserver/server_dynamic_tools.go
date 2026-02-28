package apiserver

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/diffsdk/difftracker"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tooladapter"
)

func toolAdapterProviders(s *Server) tooladapter.Providers {
	return tooladapter.Providers{
		LSP:            s.lspTools,
		CodeRun:        s,
		Approvals:      approvalProvider{s: s},
		Resource:       s,
		Orchestration:  s,
		AgentRuntime:   s,
		Schema:         s,
		Lookup:         s,
		Counter:        s,
		CodeRunTracker: s,
	}
}

func registerDynamicTools(s *Server) {
	if s == nil {
		return
	}
	if s.dynTools == nil {
		s.dynTools = make(map[string]tooladapter.RuntimeToolHandler)
	}
	clear(s.dynTools)
	tooladapter.Register(s, toolAdapterProviders(s))
}

func SetupLSP(s *Server, rootDir string) {
	if s == nil || s.lsp == nil {
		return
	}
	if rootDir != "" {
		s.lsp.SetRootURI("file://" + rootDir)
	}
	s.lsp.SetDiagnosticHandler(func(uri string, diagnostics []lsp.Diagnostic) {
		setDiagnostics(s, uri, diagnostics)

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
	threadID = strings.TrimSpace(agentID)
	codexThreadID = strings.TrimSpace(rawThreadID)
	if threadID == "" {
		return codexThreadID, ""
	}
	if codexThreadID == threadID {
		codexThreadID = ""
	}
	return threadID, codexThreadID
}

func handleDynamicToolCall(s *Server, agentID string, event agentcore.Event) {
	if s == nil {
		return
	}
	stopHeartbeat := s.codexAdapter.StartDynamicToolStallHeartbeat(agentID)
	defer stopHeartbeat()

	proc := s.mgr.Get(agentID)
	if proc == nil {
		logger.Error("app-server: dynamic_tool_call dropped — agent gone", logger.FieldAgentID, agentID)
		if event.RespondFunc != nil {
			if respondErr := event.RespondFunc(-32603, "agent not found: "+agentID); respondErr != nil {
				logger.Warn("app-server: RespondFunc failed on agent-gone",
					logger.FieldAgentID, agentID, logger.FieldError, respondErr)
			}
		}
		return
	}

	var envelope struct {
		Msg json.RawMessage `json:"msg"`
	}
	raw := event.Data
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Msg) > 0 {
		raw = envelope.Msg
	}

	var call agentcore.DynamicToolCallData
	if err := json.Unmarshal(raw, &call); err != nil {
		errMsg := "bad dynamic_tool_call data: " + err.Error()
		logger.Warn("app-server: bad dynamic_tool_call data", logger.FieldAgentID, agentID, logger.FieldError, err,
			"raw", string(event.Data))
		switch {
		case event.RespondFunc != nil:
			if respErr := event.RespondFunc(-32602, errMsg); respErr != nil {
				logger.Warn("app-server: respond error failed", logger.FieldAgentID, agentID, logger.FieldError, respErr)
			}
		case event.RequestID != nil:
			if respErr := s.codexAdapter.RespondError(proc, *event.RequestID, -32602, errMsg); respErr != nil {
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
	filePath := difftracker.ExtractStringArg(argMap, "file_path", "path", "file")
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

	if s.uiRuntime != nil {
		s.uiRuntime.IncrActivityStat(threadID, "toolCall", call.Tool)
	}

	notifyPayload := buildToolNotifyPayload(threadID, agentID, call, argMap, filePath, success, totalCalls, elapsed, result)
	if codexThreadID != "" {
		notifyPayload["codexThreadId"] = codexThreadID
	}
	notify(s, "dynamic-tool/called", notifyPayload)

	if success {
		maybeEmitDynamicToolDiffUpdate(s, threadID, codexThreadID, call.Tool, diffTracker)
	}

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
	if len(result) > 500 {
		result = result[:500]
	}
	if result != "" {
		payload["resultPreview"] = result
	}
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
