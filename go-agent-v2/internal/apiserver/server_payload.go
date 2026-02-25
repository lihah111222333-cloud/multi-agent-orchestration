// server_payload.go — payload 提取、通知广播、UI 状态同步。
package apiserver

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// uiStateThrottleEntry 全局节流状态。
type uiStateThrottleEntry struct {
	lastEmit time.Time      // 上次实际发送时间
	timer    *time.Timer    // trailing timer (保证最终一致)
	pending  map[string]any // 最新 payload (合并)
}

// SetNotifyHook 设置 Notify 事件钩子。
//
// 用于桌面端桥接: apiserver 事件 -> Wails runtime event。
func SetNotifyHook(s *Server, h func(method string, params any)) {
	if s == nil {
		return
	}
	setNotifyHookState(s, h)
}

// Notify 向所有连接广播 JSON-RPC 通知 (WebSocket + SSE)。
func Notify(s *Server, method string, params any) {
	if s == nil {
		return
	}
	notify(s, method, params)
}

func notify(s *Server, method string, params any) {
	payload := util.ToMapAny(params)
	syncUIRuntimeFromNotifyPayload(s, method, payload)
	broadcastNotification(s, method, payload)

	if shouldEmitUIStateChanged(method, payload) {
		statePayload := map[string]any{"source": method}
		if tid, _ := payload["threadId"].(string); tid != "" {
			statePayload["threadId"] = tid
		}
		if aid, _ := payload["agent_id"].(string); aid != "" {
			statePayload["agent_id"] = aid
		}
		throttledUIStateChanged(s, statePayload)
	}
}

func shouldEmitUIStateChanged(method string, payload map[string]any) bool {
	if method == "" || method == "ui/state/changed" {
		return false
	}
	if strings.HasPrefix(method, "workspace/run/") {
		return true
	}
	threadID, _ := payload["threadId"].(string)
	if strings.TrimSpace(threadID) != "" {
		return true
	}
	agentID, _ := payload["agent_id"].(string)
	return strings.TrimSpace(agentID) != ""
}

// throttledUIStateChanged 全局节流发送 ui/state/changed。
//
// 使用全局统一节流 (不再 per-thread): 多 agent 并行时也只发一条,
// 前端只需要一个信号触发 syncRuntimeState 拉取最新快照。
func throttledUIStateChanged(s *Server, payload map[string]any) {
	if s == nil {
		return
	}
	key := "_global"
	now := time.Now()
	interval := time.Duration(uiStateThrottleMs) * time.Millisecond
	pending, emitNow := stageUIStateChangedState(s,
		key,
		payload,
		now,
		interval,
		func() { flushUIStateChanged(s, key) },
	)
	if !emitNow {
		return
	}
	broadcastNotification(s, "ui/state/changed", pending)
}

// flushUIStateChanged trailing timer 回调: 发送最后一个 pending payload。
func flushUIStateChanged(s *Server, key string) {
	if s == nil {
		return
	}
	pending, ok := flushUIStateChangedState(s, key, time.Now())
	if !ok {
		return
	}
	broadcastNotification(s, "ui/state/changed", pending)
}

func syncUIRuntimeFromNotify(s *Server, method string, params any) {
	syncUIRuntimeFromNotifyPayload(s, method, util.ToMapAny(params))
}

func syncUIRuntimeFromNotifyPayload(s *Server, method string, payload map[string]any) {
	if s == nil {
		return
	}
	if s.uiRuntime == nil {
		return
	}
	switch method {
	case "workspace/run/created", "workspace/run/aborted":
		run := util.ToMapAny(payload["run"])
		if len(run) == 0 {
			return
		}
		s.uiRuntime.UpsertWorkspaceRun(run)
	case "workspace/run/merged":
		runKey, _ := payload["runKey"].(string)
		result := util.ToMapAny(payload["result"])
		if len(result) == 0 {
			return
		}
		s.uiRuntime.ApplyWorkspaceMergeResult(runKey, result)
	}
	if shouldReplayThreadNotifyToUIRuntime(method, payload) {
		threadID, _ := payload["threadId"].(string)
		normalized := uistate.NormalizeEventFromPayload(method, method, payload)
		s.uiRuntime.ApplyAgentEvent(strings.TrimSpace(threadID), normalized, payload)
	}
}

func shouldReplayThreadNotifyToUIRuntime(method string, payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if _, hasUIType := payload["uiType"]; hasUIType {
		return false
	}
	threadID, _ := payload["threadId"].(string)
	if strings.TrimSpace(threadID) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "turn/completed", "turn/aborted", "error", "codex/event/stream_error":
		return true
	default:
		return false
	}
}

