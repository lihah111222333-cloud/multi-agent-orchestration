// server_event_handler.go — Agent 事件转发与事件级 payload 增强。
package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// AgentEventHandler 返回一个 agentcore.EventHandler，将 Agent 事件转为 JSON-RPC 通知/请求。
//
// 普通事件: 广播为通知 (无需客户端回复)。
// 审批事件: 发送 Server→Client 请求, 等待客户端回复, 回传 codex (§ 二)。
func AgentEventHandler(s *Server, agentID string) agentcore.EventHandler {
	return func(event agentcore.Event) {
		method := mapEventToMethod(event.Type)

		// 统一日志: 记录所有 codex 事件
		threadID := ""
		if proc := s.mgr.Get(agentID); proc != nil {
			threadID = s.codexAdapter.GetThreadID(proc)
		}
		logger.Debug("codex event",
			logger.FieldSource, "codex",
			logger.FieldComponent, "event",
			logger.FieldAgentID, agentID,
			logger.FieldThreadID, threadID,
			logger.FieldEventType, event.Type,
		)

		// 构建通知参数: threadId 始终在顶层以便前端路由
		payload := map[string]any{
			"threadId": agentID,
		}

		// 从 event.Data 提取前端常用字段到顶层 (含嵌套 msg/data/payload)。
		mergePayloadFields(payload, event.Data)

		// mergePayloadFields 可能用 Codex 原始 threadId (UUID) 覆盖了 agentID,
		// 前端 ConversationManager 使用 Go agentID (thread-*) 作为 key, 必须还原。
		if rawTID, _ := payload["threadId"].(string); rawTID != "" && rawTID != agentID {
			payload["codexThreadId"] = rawTID
		}
		payload["threadId"] = agentID
		enrichFileChangePayload(s, agentID, event.Type, method, payload)
		enrichReadCommandPayload(s, event.Type, payload)
		s.codexAdapter.CaptureAndInjectTurnSummary(agentID, event.Type, method, payload)
		if method == "error" {
			willRetry, hasWillRetry := extractBoolFromPayload(payload, "willRetry", "will_retry", "recoverable")
			if !hasWillRetry {
				willRetry = strings.EqualFold(strings.TrimSpace(event.Type), agentcore.EventStreamError)
			}
			payload["willRetry"] = willRetry
			payload["will_retry"] = willRetry
			if _, exists := payload["error"]; !exists {
				payload["error"] = map[string]any{
					"message":           util.ExtractFirstString(payload, "message", "reason"),
					"additionalDetails": util.ExtractFirstString(payload, "additional_details", "additionalDetails"),
				}
			}
		}

		// Normalize event for UI
		normalized := uistate.NormalizeEventFromPayload(event.Type, method, payload)
		payload["uiType"] = string(normalized.UIType)
		if normalized.Text != "" {
			payload["uiText"] = normalized.Text
		}
		if normalized.Command != "" {
			payload["uiCommand"] = normalized.Command
		}
		if len(normalized.Files) > 0 {
			payload["uiFiles"] = normalized.Files
		}
		if normalized.ExitCode != nil {
			payload["uiExitCode"] = *normalized.ExitCode
		}
		if normalized.UIType == uistate.UITypeTurnStarted {
			emitTurnStartDiffReset(s, agentID, payload)
		}
		if s.uiRuntime != nil {
			s.uiRuntime.ApplyAgentEvent(agentID, normalized, payload)
		}

		s.codexAdapter.FinalizeTrackedTurnEvent(agentID, event.Type, method, payload)
		maybeAutoReportOrchestrationCompletion(s, agentID, event.Type, method, payload)

		switch event.Type {
		case agentcore.EventDynamicToolCall:
			util.SafeGo(func() { handleDynamicToolCall(s, agentID, event) })
			return
		case agentcore.EventExecApprovalRequest, agentcore.EventFileChangeApprovalRequest, approvalMethodSkillRequest:
			method := approvalMethodSkillRequest
			if event.Type == agentcore.EventExecApprovalRequest {
				method = approvalMethodCommandExecution
			} else if event.Type == agentcore.EventFileChangeApprovalRequest {
				method = approvalMethodFileChange
			}
			util.SafeGo(func() { handleApprovalRequest(s, agentID, method, payload, event) })
			return
		}

		// 普通事件: 广播通知
		notify(s, method, payload)
	}
}

func emitTurnStartDiffReset(s *Server, threadID string, payload map[string]any) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	resetPayload := map[string]any{
		"threadId": id,
		"diff":     "",
		"uiText":   "",
	}
	if codexThreadID := strings.TrimSpace(util.ExtractFirstString(payload, "codexThreadId")); codexThreadID != "" {
		resetPayload["codexThreadId"] = codexThreadID
	}

	normalized := uistate.NormalizeEventFromPayload(agentcore.EventTurnDiff, "turn/diff/updated", resetPayload)
	resetPayload["uiType"] = string(normalized.UIType)
	if s.uiRuntime != nil {
		s.uiRuntime.ApplyAgentEvent(id, normalized, resetPayload)
	}
	notify(s, "turn/diff/updated", resetPayload)
}

// enrichReadCommandPayload 检测 exec_command_begin 事件中的阅读类命令。
//
// 当 Agent 通过 Codex 内置 shell 执行 cat/grep/find 等命令时:
//   - payload 追加 isReadCommand: true + lspHint 字段 (前端可展示警告)
//   - 调用 IncrementToolCall("shell_read:<cmd>") 统计使用次数
func enrichReadCommandPayload(s *Server, eventType string, payload map[string]any) {
	if s == nil || payload == nil || eventType != agentcore.EventExecCommandBegin {
		return
	}
	cmd := extractCommandBaseName(payload)
	if cmd == "" || !readCommands[cmd] {
		return
	}
	payload["isReadCommand"] = true
	payload["lspHint"] = lspPreferenceHint
	incrementToolCallState(s, "shell_read:"+cmd)
	logger.Info("codex shell: read command detected",
		logger.FieldCommand, cmd,
	)
}

// extractCommandBaseName 从 payload 的 command 字段提取基础命令名。
//
// Codex exec_command_begin payload 中 command 可能是:
//   - "cat main.go"         → "cat"
//   - "/usr/bin/grep foo"   → "grep"
//   - "grep -rn 'pattern'"  → "grep"
func extractCommandBaseName(payload map[string]any) string {
	raw := strings.TrimSpace(util.ExtractFirstString(payload, "command", "displayCommand", "command_display", "cmd"))
	if raw == "" {
		return ""
	}
	fields := strings.Fields(raw)
	base := fields[0]
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}
	return base
}
