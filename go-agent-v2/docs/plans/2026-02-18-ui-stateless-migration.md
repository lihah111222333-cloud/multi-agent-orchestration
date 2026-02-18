# UI 无状态化迁移 实现计划

> **给 Claude:** 必须使用 @执行计划 逐任务实现此计划。

**目标:** 将前端 JS 从 ~4593 行有状态代码精简为 ~2800 行纯渲染层，业务状态迁移到 Go `internal/uistate/` 包。

**架构:** 新增 Go `internal/uistate/` 包负责事件归一化 + 偏好管理，apiserver 调用该包并注册新 JSON-RPC 方法。JS 保留纯 UI 暂态（composer、滚动位置）和轻量 timeline 渲染缓存。

**技术栈:** Go 1.22+, PostgreSQL (已有连接池), Wails v3 事件, Vue 3 ESM

---

## Phase 0: 契约定义与环境准备 (新增)

**目标:** 明确 Go/JS 交互的数据结构，建立测试基础。

**文件:**
- 创建: `internal/uistate/types.go`

**步骤 1: 定义 Go 结构体**

```go
package uistate

// NormalizedEvent 前端通用事件结构
type NormalizedEvent struct {
	UIType   UIType   `json:"uiType"`           // 归一化类型 (17种)
	UIStatus UIStatus `json:"uiStatus"`         // 归一化状态 (4种)
	Text     string   `json:"text"`             // 内容/增量
	Files    []string `json:"files,omitempty"`  // 涉及文件列表 (Go 提取)
	Ref      string   `json:"ref,omitempty"`    // 引用 ID (run_id/thread_id)
	Error    string   `json:"error,omitempty"`  // 错误信息
	ExitCode *int     `json:"exitCode,omitempty"` // 命令退出码
}

// 确保与 JS 消费侧一致:
// - Files: 始终为字符串数组，JS 不再做 diff 解析
// - Text: 始终为字符串
```

---

## Phase 1: 事件归一化 Logic Migration (PR1)

> 目标: Go 输出 17 种结构化 `uiType` 事件，JS `handleAgentEvent` 从 191 行减到 ~60 行。

---

### 任务 1: Go 事件归一化纯函数

**文件:**
- 创建: `internal/uistate/event_normalizer.go`
- 创建: `internal/uistate/event_normalizer_test.go`

**步骤 1: 写失败的测试**

```go
// internal/uistate/event_normalizer_test.go
package uistate

import (
	"encoding/json"
	"testing"
)

func TestNormalizeEvent_AssistantDelta(t *testing.T) {
	raw := json.RawMessage(`{"delta":"hello"}`)
	result := NormalizeEvent("agent_message_delta", "item/agentMessage/delta", raw)

	if result.UIType != UITypeAssistantDelta {
		t.Errorf("want UIType=%q, got %q", UITypeAssistantDelta, result.UIType)
	}
	if result.UIStatus != UIStatusThinking {
		t.Errorf("want UIStatus=%q, got %q", UIStatusThinking, result.UIStatus)
	}
	if result.Text != "hello" {
		t.Errorf("want Text=%q, got %q", "hello", result.Text)
	}
}

func TestNormalizeEvent_TurnComplete(t *testing.T) {
	raw := json.RawMessage(`{}`)
	result := NormalizeEvent("turn_complete", "turn/completed", raw)

	if result.UIType != UITypeTurnComplete {
		t.Errorf("want UIType=%q, got %q", UITypeTurnComplete, result.UIType)
	}
	if result.UIStatus != UIStatusIdle {
		t.Errorf("want UIStatus=%q, got %q", UIStatusIdle, result.UIStatus)
	}
}

func TestNormalizeEvent_TurnStarted(t *testing.T) {
	raw := json.RawMessage(`{}`)
	result := NormalizeEvent("turn_started", "turn/started", raw)

	if result.UIType != UITypeTurnStarted {
		t.Errorf("want UIType=%q, got %q", UITypeTurnStarted, result.UIType)
	}
	if result.UIStatus != UIStatusThinking {
		t.Errorf("want UIStatus=%q, got %q", UIStatusThinking, result.UIStatus)
	}
}

func TestNormalizeEvent_CommandStart(t *testing.T) {
	raw := json.RawMessage(`{"command":"ls -la","name":"shell"}`)
	result := NormalizeEvent("exec_command_begin", "item/started", raw)

	if result.UIType != UITypeCommandStart {
		t.Errorf("want UIType=%q, got %q", UITypeCommandStart, result.UIType)
	}
	if result.Command != "ls -la" {
		t.Errorf("want Command=%q, got %q", "ls -la", result.Command)
	}
}

func TestNormalizeEvent_FileEditStart(t *testing.T) {
	raw := json.RawMessage(`{"file":"main.go"}`)
	result := NormalizeEvent("patch_apply_begin", "item/fileChange/started", raw)

	if result.UIType != UITypeFileEditStart {
		t.Errorf("want UIType=%q, got %q", UITypeFileEditStart, result.UIType)
	}
	if result.File != "main.go" {
		t.Errorf("want File=%q, got %q", "main.go", result.File)
	}
}

func TestNormalizeEvent_ApprovalRequest(t *testing.T) {
	raw := json.RawMessage(`{"command":"rm -rf /"}`)
	result := NormalizeEvent("exec_approval_request", "item/commandExecution/requestApproval", raw)

	if result.UIType != UITypeApprovalRequest {
		t.Errorf("want UIType=%q, got %q", UITypeApprovalRequest, result.UIType)
	}
}

func TestNormalizeEvent_ShutdownComplete(t *testing.T) {
	result := NormalizeEvent("shutdown_complete", "", json.RawMessage(`{}`))
	if result.UIType != UITypeSystem {
		t.Errorf("want UIType=%q, got %q", UITypeSystem, result.UIType)
	}
	if result.UIStatus != UIStatusIdle {
		t.Errorf("want UIStatus=%q, got %q", UIStatusIdle, result.UIStatus)
	}
}

func TestNormalizeEvent_ExitCodeExtracted(t *testing.T) {
	raw := json.RawMessage(`{"exit_code":1}`)
	result := NormalizeEvent("exec_command_end", "item/completed", raw)

	if result.UIType != UITypeCommandDone {
		t.Errorf("want UIType=%q, got %q", UITypeCommandDone, result.UIType)
	}
	if result.ExitCode == nil || *result.ExitCode != 1 {
		t.Errorf("want ExitCode=1, got %v", result.ExitCode)
	}
}

func TestNormalizeEvent_NilData(t *testing.T) {
	// nil data should not panic
	result := NormalizeEvent("turn_complete", "", nil)
	if result.UIType != UITypeTurnComplete {
		t.Errorf("want UIType=%q, got %q", UITypeTurnComplete, result.UIType)
	}
}

func TestNormalizeEvent_TableDriven(t *testing.T) {
	tests := []struct {
		codexType string
		method    string
		wantUI    UIType
		wantSt    UIStatus
	}{
		{"agent_message_delta", "", UITypeAssistantDelta, UIStatusThinking},
		{"agent_message_content_delta", "", UITypeAssistantDelta, UIStatusThinking},
		{"agent_message_completed", "", UITypeAssistantDone, UIStatusThinking},
		{"agent_message", "", UITypeAssistantDone, UIStatusThinking},
		{"agent_reasoning_delta", "", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning", "", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning_raw", "", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning_raw_delta", "", UITypeReasoningDelta, UIStatusThinking},
		{"exec_command_begin", "", UITypeCommandStart, UIStatusRunning},
		{"exec_output_delta", "", UITypeCommandOutput, UIStatusRunning},
		{"exec_command_output_delta", "", UITypeCommandOutput, UIStatusRunning},
		{"exec_command_end", "", UITypeCommandDone, UIStatusRunning},
		{"turn_started", "", UITypeTurnStarted, UIStatusThinking},
		{"turn_complete", "", UITypeTurnComplete, UIStatusIdle},
		{"idle", "", UITypeTurnComplete, UIStatusIdle},
		{"patch_apply_begin", "", UITypeFileEditStart, UIStatusRunning},
		{"patch_apply_end", "", UITypeFileEditDone, UIStatusRunning},
		{"mcp_tool_call_begin", "", UITypeToolCall, UIStatusRunning},
		{"mcp_tool_call_end", "", UITypeCommandDone, UIStatusRunning},
		{"exec_approval_request", "", UITypeApprovalRequest, UIStatusRunning},
		{"plan_delta", "", UITypePlanDelta, UIStatusThinking},
		{"turn_diff", "", UITypeDiffUpdate, UIStatusIdle},
		{"error", "", UITypeError, UIStatusError},
		{"stream_error", "", UITypeError, UIStatusError},
		{"shutdown_complete", "", UITypeSystem, UIStatusIdle},
		{"dynamic_tool_call", "", UITypeToolCall, UIStatusRunning},
		{"session_configured", "", UITypeSystem, ""},
		{"warning", "", UITypeSystem, ""},
		{"some_unknown_thing", "", UITypeSystem, ""},
	}
	for _, tt := range tests {
		t.Run(tt.codexType, func(t *testing.T) {
			result := NormalizeEvent(tt.codexType, tt.method, json.RawMessage(`{}`))
			if result.UIType != tt.wantUI {
				t.Errorf("UIType: want %q, got %q", tt.wantUI, result.UIType)
			}
			if result.UIStatus != tt.wantSt {
				t.Errorf("UIStatus: want %q, got %q", tt.wantSt, result.UIStatus)
			}
		})
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/uistate/ -run TestNormalizeEvent -v`
预期: FAIL (package/types not defined)

**步骤 3: 写最小实现**

```go
// internal/uistate/event_normalizer.go
package uistate

import "encoding/json"

// UIType 前端渲染事件类型 (17 种, 完整覆盖 codex/events.go 全部事件)。
type UIType string

const (
	UITypeAssistantDelta UIType = "assistant_delta"
	UITypeAssistantDone  UIType = "assistant_done"
	UITypeReasoningDelta UIType = "reasoning_delta"
	UITypeCommandStart   UIType = "command_start"
	UITypeCommandOutput  UIType = "command_output"
	UITypeCommandDone    UIType = "command_done"
	UITypeFileEditStart  UIType = "file_edit_start"
	UITypeFileEditDone   UIType = "file_edit_done"
	UITypeToolCall       UIType = "tool_call"
	UITypeApprovalRequest UIType = "approval_request"
	UITypePlanDelta      UIType = "plan_delta"
	UITypeTurnStarted    UIType = "turn_started"
	UITypeTurnComplete   UIType = "turn_complete"
	UITypeDiffUpdate     UIType = "diff_update"
	UITypeUserMessage    UIType = "user_message"
	UITypeError          UIType = "error"
	UITypeSystem         UIType = "system"
)

// UIStatus 前端状态标签 (4 种)。
type UIStatus string

const (
	UIStatusIdle     UIStatus = "idle"
	UIStatusThinking UIStatus = "thinking"
	UIStatusRunning  UIStatus = "running"
	UIStatusError    UIStatus = "error"
)

// NormalizedEvent 归一化后的 UI 事件。
type NormalizedEvent struct {
	UIType   UIType   `json:"uiType"`
	UIStatus UIStatus `json:"uiStatus"`
	Text     string   `json:"text,omitempty"`
	Command  string   `json:"command,omitempty"`
	File     string   `json:"file,omitempty"`
	ExitCode *int     `json:"exitCode,omitempty"`
}

// NormalizeEvent 将 codex 事件归一化为前端可渲染的结构化事件。
//
// 纯函数, 无状态, 无锁, 热路径安全。
func NormalizeEvent(codexType, method string, data json.RawMessage) NormalizedEvent {
	var payload map[string]any
	if len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	if payload == nil {
		payload = map[string]any{} // 防止后续字段提取 panic
	}

	uiType, uiStatus := classifyEvent(codexType, method)

	result := NormalizedEvent{
		UIType:   uiType,
		UIStatus: uiStatus,
	}

	// 1. 提取 Text (优先顺序: delta > text > content > output > message)
	if v, ok := payload["delta"].(string); ok {
		result.Text = v
	} else if v, ok := payload["text"].(string); ok {
		result.Text = v
	} else if v, ok := payload["content"].(string); ok {
		result.Text = v
	} else if v, ok := payload["output"].(string); ok {
		result.Text = v
	} else if v, ok := payload["message"].(string); ok {
		result.Text = v
	}

	// 2. 提取 Command
	if v, ok := payload["command"].(string); ok {
		result.Command = v
	}

	// 3. 提取 Files (移除了 JS 的 extractFilesFromPatchDelta 逻辑)
	if event.Type == "patch_apply_begin" {
		if f, ok := payload["file"].(string); ok {
			result.Files = []string{f}
		} else if d, ok := payload["delta"].(string); ok {
			// 兼容旧格式: 从 diff header 解析文件名 (可选实现, 若 codex 保证 file 字段则不需要)
			// result.Files = parseGitDiffHeader(d) 
		}
	} else if event.Type == "item/fileChange/started" {
		if f, ok := payload["file"].(string); ok {
			result.Files = []string{f}
		}
	} else if v, ok := payload["file"].(string); ok {
		// Generic file field fallback
		result.Files = []string{v}
	}

	// 4. 提取 ExitCode
	if event.Type == "exec_command_end" {
		if code, ok := payload["exit_code"].(float64); ok { // JSON number is float64
			c := int(code)
			result.ExitCode = &c
		}
	}

	return result
}

// classifyEvent 按 codex 原始事件类型分类。
//
// 事件名有 3 种格式:
//   - codex 原始: "exec_command_begin"
//   - app-server 映射: "item/started"
//   - 带前缀全路径: "agent/event/item/fileChange/started"
//
// 此函数优先匹配 codex 原始类型 (由 runner 传入),
// 映射后的名称由 JS 端根据 evt.type 字段兜底匹配。
func classifyEvent(codexType, method string) (UIType, UIStatus) {
	switch codexType {
	// ── 助手消息 ──
	case "agent_message_delta", "agent_message_content_delta":
		return UITypeAssistantDelta, UIStatusThinking
	case "agent_message_completed", "agent_message":
		return UITypeAssistantDone, UIStatusThinking

	// ── 推理 ──
	case "agent_reasoning", "agent_reasoning_delta",
		"agent_reasoning_raw", "agent_reasoning_raw_delta",
		"agent_reasoning_section_break":
		return UITypeReasoningDelta, UIStatusThinking

	// ── 命令执行 ──
	case "exec_command_begin":
		return UITypeCommandStart, UIStatusRunning
	case "exec_output_delta", "exec_command_output_delta":
		return UITypeCommandOutput, UIStatusRunning
	case "exec_command_end":
		return UITypeCommandDone, UIStatusRunning

	// ── 文件编辑 (独立于 command, 保留文件名追踪) ──
	case "patch_apply_begin", "file_read":
		return UITypeFileEditStart, UIStatusRunning
	case "patch_apply", "patch_apply_delta":
		return UITypeCommandOutput, UIStatusRunning
	case "patch_apply_end", "file_updated":
		return UITypeFileEditDone, UIStatusRunning

	// ── 工具调用 ──
	case "mcp_tool_call_begin", "mcp_tool_call", "dynamic_tool_call":
		return UITypeToolCall, UIStatusRunning
	case "mcp_tool_call_end":
		return UITypeCommandDone, UIStatusRunning

	// ── 审批请求 ──
	case "exec_approval_request", "file_change_approval_request":
		return UITypeApprovalRequest, UIStatusRunning

	// ── 对话轮次生命周期 ──
	case "turn_started":
		return UITypeTurnStarted, UIStatusThinking
	case "turn_complete", "idle":
		return UITypeTurnComplete, UIStatusIdle

	// ── Plan / Diff ──
	case "plan_delta", "plan_update":
		return UITypePlanDelta, UIStatusThinking
	case "turn_diff":
		return UITypeDiffUpdate, UIStatusIdle

	// ── 用户消息 ──
	case "user_message":
		return UITypeUserMessage, UIStatusThinking

	// ── 错误 ──
	case "error", "stream_error":
		return UITypeError, UIStatusError

	// ── 警告 (非 error, 不改 runner 状态) ──
	case "warning":
		return UITypeSystem, ""

	// ── 系统/生命周期 ──
	case "shutdown_complete":
		return UITypeSystem, UIStatusIdle
	case "session_configured", "mcp_startup_complete",
		"mcp_list_tools_response", "list_skills_response",
		"token_count", "context_compacted",
		"thread_name_updated", "thread_rolled_back",
		"undo_started", "undo_completed",
		"entered_review_mode", "exited_review_mode",
		"background_event":
		return UITypeSystem, ""

	// ── 协作 Agent ──
	case "collab_agent_spawn_begin", "collab_agent_interaction_begin",
		"collab_waiting_begin":
		return UITypeSystem, UIStatusRunning
	case "collab_agent_spawn_end", "collab_agent_interaction_end",
		"collab_waiting_end":
		return UITypeSystem, UIStatusRunning
	}

	// 兜底: 未知事件 — 返回空 UIStatus, runner 不改状态 (保持现有行为)
	return UITypeSystem, ""
}
```

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/uistate/ -run TestNormalizeEvent -v`
预期: PASS (所有 28 个子测试)

**步骤 5: 提交**

```bash
git add internal/uistate/event_normalizer.go internal/uistate/event_normalizer_test.go
git commit -m "feat(uistate): add event normalizer with 17 UI types covering all codex events"
```

---

### 任务 2: apiserver 集成事件归一化

**文件:**
- 修改: `internal/apiserver/server.go:927` (在 `enrichFileChangePayload` 之后)

**步骤 1: 运行已有的 uistate 测试确认通过**

运行: `go test ./internal/uistate/ -v`
预期: PASS

**步骤 2: 修改 apiserver AgentEventHandler**

在 `server.go` 第 927 行 `s.enrichFileChangePayload(agentID, event.Type, method, payload)` 之后追加:

```go
// server.go 文件顶部 import 追加:
// "github.com/multi-agent/go-agent-v2/internal/uistate"

// 在 s.enrichFileChangePayload(...) 之后, 审批事件 switch 之前:
normalized := uistate.NormalizeEvent(event.Type, method, event.Data)
payload["uiType"] = string(normalized.UIType)
payload["uiStatus"] = string(normalized.UIStatus)
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
```

**步骤 3: 编译验证**

运行: `go build ./cmd/agent-terminal/`
预期: SUCCESS

**步骤 4: 提交**

```bash
git add internal/apiserver/server.go
git commit -m "feat(apiserver): inject uiType/uiStatus into event payloads"
```

---

### 任务 3: JS handleAgentEvent 精简

**文件:**
- 修改: `cmd/agent-terminal/frontend/vue-app/stores/threads.js:1171-1361`

**重要: 现有 JS 函数名对照表** (必须使用正确的函数名):

| UIType | 调用的实际 JS 函数 |
|---|---|
| `turn_started` | `completeTurn(threadId)` + `startThinking(threadId)` |
| `turn_complete` | `completeTurn(threadId)` |
| `assistant_delta` | `appendAssistant(threadId, text)` |
| `assistant_done` | `finishAssistant(threadId)` |
| `reasoning_delta` | `appendThinking(threadId, text)` |
| `command_start` | `startCommand(threadId, command)` |
| `command_output` | `appendCommandOutput(threadId, text)` |
| `command_done` | `finishCommand(threadId, exitCode)` |
| `file_edit_start` | `fileEditing(threadId, file)` + `rememberEditingFiles(threadId, [file])` |
| `file_edit_done` | `fileSaved(threadId, file)` |
| `tool_call` | `appendToolCall(threadId, payload)` |
| `approval_request` | `showApproval(threadId, command)` |
| `plan_delta` | `appendPlan(threadId, text)` |
| `diff_update` | `setDiff(threadId, payload.diff)` |
| `user_message` | `appendUser(threadId, text)` |
| `error` | `addError(threadId, text)` |

**步骤 1: 替换 handleAgentEvent (1171-1361) 为 ~65 行 switch**

```javascript
function handleAgentEvent(evt) {
  const threadId = evt?.agent_id || evt?.threadId || '';
  const eventType = (evt?.type || '').toString();
  if (!threadId) return;

  const seq = ++agentEventSeq;
  const sampled = seq % AGENT_EVENT_LOG_SAMPLE === 0 || !eventType.toLowerCase().includes('delta');
  if (sampled) logDebug('event', 'agent.received', { seq, thread_id: threadId, type: eventType });

  ensureThreadState(threadId);
  markAgentActive(threadId);

  const payload = parsePayload(evt?.data);

  // — 优先使用 Go 归一化字段, 降级到 eventType 原始分支 —
  const uiType = payload?.uiType;
  const uiStatus = payload?.uiStatus;

  if (uiStatus) {
    const prev = state.statuses[threadId] || 'idle';
    updateThreadState(threadId, uiStatus);
    if (prev !== uiStatus) {
      logInfo('thread', 'status.changed', { thread_id: threadId, from: prev, to: uiStatus, by_event: eventType });
    }
  }

  // 如果 Go 没有注入 uiType (兼容期), 走旧路径
  if (!uiType) {
    handleAgentEventLegacy(threadId, eventType, payload);
    return;
  }

  const text = payload.uiText || payload.delta || payload.text || payload.content || '';
  const command = payload.uiCommand || payload.command || '';
  const file = payload.uiFile || payload.file || '';

  switch (uiType) {
    case 'turn_started':
      completeTurn(threadId);
      startThinking(threadId);
      break;
    case 'turn_complete':
      completeTurn(threadId);
      break;
    case 'assistant_delta':
      appendAssistant(threadId, text);
      break;
    case 'assistant_done':
      finishAssistant(threadId);
      break;
    case 'reasoning_delta':
      appendThinking(threadId, text);
      break;
    case 'command_start':
      if (command) startCommand(threadId, command);
      break;
    case 'command_output':
      appendCommandOutput(threadId, text);
      break;
    case 'command_done':
      finishCommand(threadId, payload.uiExitCode ?? payload.exit_code);
      break;
    case 'file_edit_start': {
      const files = file ? [file] : normalizeFiles(payload.files);
      for (const f of files) fileEditing(threadId, f);
      rememberEditingFiles(threadId, files);
      break;
    }
    case 'file_edit_done': {
      let files = file ? [file] : normalizeFiles(payload.files);
      if (files.length === 0) files = consumeEditingFiles(threadId);
      for (const f of files) fileSaved(threadId, f);
      break;
    }
    case 'tool_call':
      appendToolCall(threadId, payload);
      break;
    case 'approval_request':
      showApproval(threadId, command);
      break;
    case 'plan_delta':
      appendPlan(threadId, text);
      break;
    case 'diff_update':
      if (payload.diff) setDiff(threadId, payload.diff);
      break;
    case 'user_message':
      appendUser(threadId, text);
      break;
    case 'error':
      addError(threadId, text);
      break;
    default:
      // system events — no-op
      break;
  }
}
```

**步骤 2: 保留旧 handler 为 `handleAgentEventLegacy`**

将现有 1204-1361 行的 switch 逻辑重命名为 `handleAgentEventLegacy(threadId, eventType, payload)`, 作为兼容降级路径。待全量验证后在 Phase 4 删除。

**步骤 3: 手动验证**

- 启动应用, 发送消息, 验证流式输出正常 (assistant_delta 流畅无延迟)
- 验证命令执行显示正常, exit_code 正确
- 验证文件编辑事件显示 (fileEditing/fileSaved)
- 验证审批请求弹窗
- 验证状态标签 (thinking/running/idle) 正确切换

**步骤 4: 提交**

```bash
git add cmd/agent-terminal/frontend/vue-app/stores/threads.js
git commit -m "refactor(frontend): add uiType-based handler with legacy fallback"
```

---

## Phase 2: 偏好/元数据迁移 (PR2)

> 目标: activeThreadId, mainAgentId, agentMetaById 从 localStorage 迁移到 Go + PG。

---

### 任务 4: PG 迁移脚本

**文件:**
- 创建: `migrations/0010_ui_preferences.sql`

**步骤 1: 写迁移脚本**

```sql
-- 0010_ui_preferences.sql — UI 偏好持久化。
-- 取代前端 localStorage, 支持多实例共享。

CREATE TABLE IF NOT EXISTS ui_preferences (
    key         TEXT        PRIMARY KEY,
    value       JSONB       NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 预置默认偏好
INSERT INTO ui_preferences (key, value) VALUES
    ('activeThreadId', '""'::jsonb),
    ('activeCmdThreadId', '""'::jsonb),
    ('mainAgentId', '""'::jsonb),
    ('agentMeta', '{}'::jsonb),
    ('viewPrefs.chat', '{"layout":"focus","splitRatio":64}'::jsonb),
    ('viewPrefs.cmd', '{"layout":"focus","splitRatio":56,"cardCols":3}'::jsonb)
ON CONFLICT (key) DO NOTHING;
```

**步骤 2: 验证迁移**

运行: `psql $POSTGRES_CONNECTION_STRING -f migrations/0010_ui_preferences.sql`
预期: CREATE TABLE, INSERT 6 rows

**步骤 3: 提交**

```bash
git add migrations/0010_ui_preferences.sql
git commit -m "feat(db): add ui_preferences table for stateless frontend"
```

---

### 任务 5: Go Store

**文件:**
- 创建: `internal/store/ui_preference.go`
- 创建: `internal/store/ui_preference_test.go`

**步骤 1: 写失败的测试**

```go
// internal/store/ui_preference_test.go
package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// getTestPool 获取测试 DB 连接池, 无 DB 时直接 skip。
// getTestPool 获取测试 DB 连接池, 无 DB 时直接 skip。
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	connStr := os.Getenv("TEST_POSTGRES_CONNECTION_STRING")
	if connStr == "" {
		connStr = os.Getenv("POSTGRES_CONNECTION_STRING")
	}
	if connStr == "" {
		t.Skip("skipping db test: TEST_POSTGRES_CONNECTION_STRING not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}

	// 清理表
	_, err = pool.Exec(ctx, "TRUNCATE TABLE ui_preferences")
	if err != nil {
		pool.Close()
		t.Fatalf("failed to truncate table: %v", err)
	}
	
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestUIPreferenceStore_GetSet(t *testing.T) {
	pool := getTestPool(t)
	s := NewUIPreferenceStore(pool)
	ctx := context.Background()

	// Set
	err := s.Set(ctx, "test_ui_pref_key", `"test_value"`)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get
	val, err := s.Get(ctx, "test_ui_pref_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != `"test_value"` {
		t.Errorf("want %q, got %q", `"test_value"`, val)
	}

	// GetAll
	all, err := s.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if all["test_ui_pref_key"] != `"test_value"` {
		t.Errorf("GetAll missing key")
	}

	// Cleanup
	_ = s.Delete(ctx, "test_ui_pref_key")
}
```

**步骤 2: 写最小实现**

```go
// internal/store/ui_preference.go
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UIPreferenceStore ui_preferences 表 CRUD。
type UIPreferenceStore struct{ BaseStore }

// NewUIPreferenceStore 创建。
func NewUIPreferenceStore(pool *pgxpool.Pool) *UIPreferenceStore {
	return &UIPreferenceStore{NewBaseStore(pool)}
}

// Get 获取偏好值 (JSON string)。key 不存在返回空字符串。
// 仅 pgx.ErrNoRows 兜底返回空，其他 DB 错误必须返回 err。
func (s *UIPreferenceStore) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx,
		"SELECT value::text FROM ui_preferences WHERE key = $1", key).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil // key 不存在 → 返回空
		}
		return "", err
	}
	return value, nil
}

// Set 设置偏好值 (upsert)。
func (s *UIPreferenceStore) Set(ctx context.Context, key, valueJSON string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ui_preferences (key, value, updated_at)
		 VALUES ($1, $2::jsonb, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		key, valueJSON)
	return err
}

// GetAll 获取全部偏好 (map)。
func (s *UIPreferenceStore) GetAll(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT key, value::text FROM ui_preferences")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		result[k] = v
	}
	return result, rows.Err()
}

// Delete 删除偏好。
func (s *UIPreferenceStore) Delete(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM ui_preferences WHERE key = $1", key)
	return err
}
```

**步骤 3: 运行测试**

运行: `go test ./internal/store/ -run TestUIPreference -v`
预期: PASS (with DB) or SKIP (without DB)

**步骤 4: 提交**

```bash
git add internal/store/ui_preference.go internal/store/ui_preference_test.go
git commit -m "feat(store): add UIPreferenceStore for frontend preferences"
```

---

### 任务 6: JSON-RPC 偏好方法 + apiserver 接入

**文件:**
- 创建: `internal/uistate/preferences.go`
- 修改: `internal/apiserver/server.go` (追加 `prefMgr` 字段 + `New()` 初始化)
- 修改: `internal/apiserver/methods.go` (注册 3 个新方法)

**步骤 1: uistate preferences 协调器**

```go
// internal/uistate/preferences.go
package uistate

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

// PreferenceManager 偏好读写协调器。
type PreferenceManager struct {
	store *store.UIPreferenceStore
}

// NewPreferenceManager 创建。
func NewPreferenceManager(s *store.UIPreferenceStore) *PreferenceManager {
	return &PreferenceManager{store: s}
}

// GetAll 获取全部偏好。
func (m *PreferenceManager) GetAll(ctx context.Context) (map[string]string, error) {
	return m.store.GetAll(ctx)
}

// Get 获取单个偏好。
func (m *PreferenceManager) Get(ctx context.Context, key string) (string, error) {
	return m.store.Get(ctx, key)
}

// Set 设置单个偏好。
func (m *PreferenceManager) Set(ctx context.Context, key, valueJSON string) error {
	return m.store.Set(ctx, key, valueJSON)
}
```

**步骤 2: server.go 追加字段 + 初始化**

```go
// Server struct 追加字段 (在 skillSvc 之后):
prefMgr  *uistate.PreferenceManager

// New() 函数: 在 deps.DB != nil 块内 (约第 165 行 taskTraceStore 之后) 追加:
uiPrefStore := store.NewUIPreferenceStore(deps.DB)
s.prefMgr = uistate.NewPreferenceManager(uiPrefStore)

// server.go import 追加:
// "github.com/multi-agent/go-agent-v2/internal/uistate"
```

**步骤 3: 注册 JSON-RPC 方法**

在 `methods.go:registerMethods()` 追加:

```go
// § 14. UI 偏好 (前端无状态化)
s.methods["ui/preferences/get"] = s.uiPreferencesGet
s.methods["ui/preferences/set"] = s.uiPreferencesSet
s.methods["ui/preferences/getAll"] = s.uiPreferencesGetAll
```

实现 (3 个方法全部实现):

```go
func (s *Server) uiPreferencesGetAll(ctx context.Context, _ json.RawMessage) (any, error) {
	if s.prefMgr == nil {
		return map[string]any{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	prefs, err := s.prefMgr.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return prefs, nil
}

func (s *Server) uiPreferencesGet(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if s.prefMgr == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	val, err := s.prefMgr.Get(ctx, p.Key)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (s *Server) uiPreferencesSet(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if s.prefMgr == nil {
		return map[string]any{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.prefMgr.Set(ctx, p.Key, p.Value); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}
```

**步骤 4: 编译验证**

运行: `go build ./cmd/agent-terminal/`
预期: SUCCESS

**步骤 5: 提交**

```bash
git add internal/uistate/preferences.go internal/apiserver/server.go internal/apiserver/methods.go
git commit -m "feat(apiserver): add ui/preferences JSON-RPC methods with prefMgr wiring"
```

---

### 任务 7: JS 偏好迁移

**文件:**
- 修改: `cmd/agent-terminal/frontend/vue-app/stores/threads.js`

**当前 localStorage 调用点** (11 处, 全部替换):
- L45: `localStorage.getItem(ACTIVE_THREAD_KEY)`
- L46: `localStorage.getItem(ACTIVE_CMD_THREAD_KEY)`
- L47: `localStorage.getItem(MAIN_AGENT_KEY)`
- L98: `localStorage.getItem(key)` (viewPrefs)
- L113: `localStorage.getItem(AGENT_META_KEY)`
- L116: `localStorage.removeItem(AGENT_META_KEY)`
- L154: `localStorage.removeItem(AGENT_META_KEY)`
- L162: `localStorage.setItem(key, JSON.stringify(...))`
- L184: `localStorage.setItem(ACTIVE_THREAD_KEY, ...)`
- L196: `localStorage.setItem(ACTIVE_CMD_THREAD_KEY, ...)`
- L210: `localStorage.setItem(MAIN_AGENT_KEY, ...)`

**步骤 1: 新增 `loadPreferences()` 并替换初始化**

```javascript
// 替换 L45-47 的同步 localStorage.getItem 为异步加载
// state 初始值改为空默认:
//   activeThreadId: '',
//   activeCmdThreadId: '',
//   mainAgentId: '',

async function loadPreferences() {
  try {
    const result = await callAPI('ui/preferences/getAll', {});
    const prefs = result || {};
    if (prefs.activeThreadId) state.activeThreadId = JSON.parse(prefs.activeThreadId);
    if (prefs.activeCmdThreadId) state.activeCmdThreadId = JSON.parse(prefs.activeCmdThreadId);
    if (prefs.mainAgentId) state.mainAgentId = JSON.parse(prefs.mainAgentId);
    if (prefs.agentMeta) state.agentMetaById = JSON.parse(prefs.agentMeta);
    // viewPrefs 使用嵌套结构: {chat: {...}, cmd: {...}}
    if (prefs['viewPrefs.chat']) {
      Object.assign(state.viewPrefs.chat, JSON.parse(prefs['viewPrefs.chat']));
    }
    if (prefs['viewPrefs.cmd']) {
      Object.assign(state.viewPrefs.cmd, JSON.parse(prefs['viewPrefs.cmd']));
    }
  } catch (e) {
    console.warn('loadPreferences failed, using defaults', e);
  }
}
```

**步骤 2: 在 threads.js 的 `init()` 或 `export` 的初始化流程中调用 `loadPreferences()`**

```javascript
// 在 useThreadStore() 或 store 初始化末尾追加:
// (确保在 Vue mount 之后立即调用)
loadPreferences(); // 异步, 不阻塞首次渲染
```

> **关键:** `loadPreferences()` 是异步的, 首次渲染用默认空值, 加载完成后 Vue 响应式自动刷新。

**步骤 3: 替换 save 函数为「本地先更新 + 异步持久化」**

```javascript
// saveActiveThread 保留 id 参数 — 调用点 (app.js:120, UnifiedChatPage.js:87) 传参不变
function saveActiveThread(id) {
  state.activeThreadId = id || '';
  callAPI('ui/preferences/set', {
    key: 'activeThreadId',
    value: JSON.stringify(state.activeThreadId),
  }).catch((e) => logWarn('prefs', 'save.failed', { key: 'activeThreadId', error: e }));
}

function saveActiveCmdThread(id) {
  state.activeCmdThreadId = id || '';
  callAPI('ui/preferences/set', {
    key: 'activeCmdThreadId',
    value: JSON.stringify(state.activeCmdThreadId),
  }).catch((e) => logWarn('prefs', 'save.failed', { key: 'activeCmdThreadId', error: e }));
}

function setMainAgent(id) {
  state.mainAgentId = id;
  callAPI('ui/preferences/set', {
    key: 'mainAgentId',
    value: JSON.stringify(id),
  }).catch((e) => logWarn('prefs', 'save.failed', { key: 'mainAgentId', error: e }));
}
```

> **模式:** 本地先更新 state → fire-and-forget 持久化 → 失败降级日志。不 await, 不阻塞 UI。

**步骤 4: 删除所有 localStorage 引用** (11 处)

**步骤 5: 手动验证**

- 启动应用, 确认偏好从 PG 正确加载
- 切换活跃线程, 重启应用, 确认恢复正确
- DB 不可用时启动, 确认降级到默认值 (空字符串)

**步骤 6: 提交**

```bash
git add cmd/agent-terminal/frontend/vue-app/stores/threads.js
git commit -m "refactor(frontend): migrate preferences from localStorage to Go API"
```

---

## Phase 3: 项目列表迁移 (PR3)

---

### 任务 8: 项目偏好 via ui/preferences

**文件:**
- 修改: `cmd/agent-terminal/frontend/vue-app/stores/projects.js`

**当前 localStorage 调用点** (4 处):
- L19: `localStorage.getItem(STORAGE_KEY)`
- L29: `localStorage.getItem(ACTIVE_KEY)`
- L37: `localStorage.setItem(STORAGE_KEY, ...)`
- L38: `localStorage.setItem(ACTIVE_KEY, ...)`

**步骤 1: 替换 localStorage**

```javascript
async function persist() {
  callAPI('ui/preferences/set', {
    key: 'projects',
    value: JSON.stringify({ active: state.active, projects: state.projects }),
  }).catch(() => {});
}

async function loadProjects() {
  try {
    const raw = await callAPI('ui/preferences/get', { key: 'projects' });
    if (raw) {
      const data = JSON.parse(raw);
      state.active = data.active || '.';
      state.projects = data.projects || [];
    }
  } catch (e) {
    console.warn('loadProjects failed', e);
  }
}
```

**步骤 2: 在 projects.js 初始化中调用 `loadProjects()`**

```javascript
// 在 projects store 初始化末尾追加:
loadProjects(); // 异步, 首次渲染用默认值
```

**步骤 3: 删除 localStorage 引用** (4 处)

**步骤 4: 手动验证**

- 添加项目目录, 重启确认保留
- 切换项目, 确认正确

**步骤 5: 提交**

```bash
git add cmd/agent-terminal/frontend/vue-app/stores/projects.js
git commit -m "refactor(frontend): migrate project list from localStorage to Go API"
```

---

## Phase 4: 状态映射统一 + 清理 (PR4)

---

### 任务 9: 合并 Go 事件映射

**文件:**
- 修改: `internal/runner/manager.go:202-212`
- 修改: `internal/runner/manager_test.go` (更新 `TestEventStateMap_Completeness`)

**步骤 1: runner.eventStateMap 复用 uistate**

```go
// runner/manager.go — 改用 uistate 归一化
import "github.com/multi-agent/go-agent-v2/internal/uistate"

// 删除 eventStateMap (L202-212)

// 替换 handleEvent (L215-233):
func (m *AgentManager) handleEvent(proc *AgentProcess, event codex.Event) {
	normalized := uistate.NormalizeEvent(event.Type, "", event.Data)

	proc.mu.Lock()
	// 仅在 UIStatus 非空时更新状态 — 未知事件和 system 事件不改状态 (保持现有行为)
	if normalized.UIStatus != "" {
		switch normalized.UIStatus {
		case uistate.UIStatusIdle:
			proc.State = StateIdle
		case uistate.UIStatusThinking:
			proc.State = StateThinking
		case uistate.UIStatusRunning:
			proc.State = StateRunning
		case uistate.UIStatusError:
			proc.State = StateError
		}
	}
	// shutdown_complete → stopped (uistate 当前返回 system+idle, 显式特判更稳妥)
	if event.Type == "shutdown_complete" {
		proc.State = StateStopped
	}
	proc.mu.Unlock()

	logger.Debug("runner: state transition",
		logger.FieldAgentID, proc.ID,
		logger.FieldEventType, event.Type,
		logger.FieldState, string(proc.State),
	)

	m.mu.RLock()
	handler := m.onEvent
	m.mu.RUnlock()
	if handler != nil {
		handler(proc.ID, event)
	}
}
```

**步骤 2: 更新 manager_test.go**

将 `TestEventStateMap_Completeness` 替换为基于 `uistate.NormalizeEvent` 的测试:

```go
func TestHandleEvent_StateTransitions(t *testing.T) {
	tests := []struct {
		eventType string
		wantState AgentState
	}{
		{"turn_started", StateThinking},
		{"idle", StateIdle},
		{"turn_complete", StateIdle},
		{"exec_command_begin", StateRunning},
		{"error", StateError},
		{"shutdown_complete", StateStopped},
	}
	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			mgr := NewAgentManager()
			proc := &AgentProcess{ID: "test", State: StateIdle}
			mgr.agents["test"] = proc
			mgr.handleEvent(proc, codex.Event{Type: tt.eventType})
			if proc.State != tt.wantState {
				t.Errorf("event %q: want %q, got %q", tt.eventType, tt.wantState, proc.State)
			}
		})
	}
}
```

**步骤 3: 运行全量测试**

运行: `go test ./internal/runner/ ./internal/uistate/ -v`
预期: PASS

**步骤 4: 提交**

```bash
git add internal/runner/manager.go internal/runner/manager_test.go
git commit -m "refactor(runner): unify event state mapping via uistate package"
```

---

### 任务 10: JS 最终清理

**文件:**
- 修改: `cmd/agent-terminal/frontend/vue-app/stores/threads.js` (删除 `handleAgentEventLegacy`)
- 修改: `cmd/agent-terminal/frontend/vue-app/services/status.js` (精简)

**步骤 1: 删除 `handleAgentEventLegacy`** (~160 行)

在 Phase 1 验证完毕后, 删除整个 legacy handler。

**步骤 2: 精简 `status.js` & `threads.js`**

**保留** (UnifiedChatPage.js:12 和 threads.js:21 依赖):
- `STATUS_LABEL_ZH` 常量
- `normalizeStatus()` — UI 状态别名归一 (stopped→idle 等)
- `statusLabel()` — 状态→中文标签
- `extractEventText()` — payload 文本提取 (通用工具)
- `ensureModePrefs` — UI 约束逻辑

**删除** (被 Go `uiType`/`uiStatus`/`Files` 取代):
- `statusFromEventType()` (~40 行) — Go 已做
- `isAssistantDeltaEvent()` / `isReasoningDeltaEvent()` (~15 行) — Go 已做
- `inferItemStatus()` (~12 行) — Go 已做
- `extractFilesFromPatchDelta()` (~20 行) — Go `files` 字段取代
- `normalizeFiles()` (~10 行) — Go 保证返回数组

**修改**:
- `rememberEditingFiles(threadId, files)` 直接使用 payload 传入的 `files` 数组, 不再解析 delta。

**步骤 3: 行数统计验证**

运行: `find cmd/agent-terminal/frontend/vue-app -name '*.js' | xargs wc -l`
预期: 总行数较迁移前减少 40%+

**步骤 4: 提交**

```bash
git add -A
git commit -m "refactor(frontend): remove legacy handler and status.js business logic"
```

---

## 验证清单

| 验证项 | 命令 | 预期 |
|---|---|---|
| Go 单元测试 | `go test ./internal/uistate/ -v` | PASS (28+ tests) |
| Go Store 测试 | `go test ./internal/store/ -run TestUIPreference -v` | PASS (with DB) |
| Runner 测试 | `go test ./internal/runner/ -v` | PASS |
| Go 编译 | `go build ./cmd/agent-terminal/` | SUCCESS |
| `go vet` | `go vet ./...` | no issues |
| 全量 Go 测试 | `go test ./... -count=1` | PASS |
| 流式输出延迟 | 手动: 发送消息观察 delta 流畅度 | 与迁移前无感知差异 |
| 偏好持久化 | 手动: 改偏好→重启→检查恢复 | 正确恢复 |
| DB 降级 | 手动: 断开 PG→启动→验证默认值 | 不崩溃, 使用默认值 |
| 兼容降级 | 手动: 注释 Go uiType 注入→JS 走 legacy→功能正常 | 旧路径正常 |

---

## 自审查修复记录

本计划已修复初版的 17 个问题:

| # | 类型 | 问题 | 修复 |
|---|---|---|---|
| 1 | 🔴 | SQL `'"" '::jsonb` 多余空格 | 已改为 `'""'::jsonb` |
| 2 | 🔴 | `turn_started` 映射为 TurnComplete | 新增 `UITypeTurnStarted` |
| 3 | 🔴 | `ui/preferences/get` 注册但无实现 | 已补上 3 个方法的完整实现 |
| 4 | 🔴 | `context.Background()` 误用 | 改为使用传入的 `ctx` |
| 5 | 🟡 | 缺少 20+ 事件类型 | classifyEvent 覆盖全部 40+ 种 codex 事件 |
| 6 | 🟡 | 行号引用 `920-943` 不准 | 修正为 `927 行 enrichFileChangePayload 之后` |
| 7 | 🟡 | `testPool` 未定义 | 新增 `getTestPool()` helper with skip |
| 8 | 🟡 | nil data 致 panic | 添加 `if payload == nil { payload = map... }` |
| 9 | 🟢 | ExitCode 未提取 | 已添加 `exit_code` float64→int 提取 |
| 10 | 🟢 | 删除 eventStateMap 会破坏测试 | 同步更新 manager_test.go |
| 11 | 🔴 | Server 缺 prefMgr 字段和初始化 | 任务 6 显式列出 struct + New() 修改 |
| 12 | 🔴 | JS 函数名全错 | 添加完整对照表, 使用正确函数名 |
| 13 | 🔴 | turn_started 丢失 startThinking | 新 switch 中 turn_started 调两个函数 |
| 14 | 🟡 | fileChange 事件链遗漏 | 新增 file_edit_start/done UIType |
| 15 | 🟡 | 工具/审批/plan 事件遗漏 | 新增 tool_call/approval_request/plan_delta 等 |
| 16 | 🟡 | 事件别名多格式未处理 | 保留 legacy 降级 + Go 按 codex 原始类型分类 |
| 17 | 🟡 | ui/preferences/get 被调用但无实现 | 已在任务 6 补全 |
