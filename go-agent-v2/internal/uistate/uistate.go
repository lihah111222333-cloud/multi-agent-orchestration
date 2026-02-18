// uistate.go — UI 状态类型定义与偏好管理。
package uistate

import (
	"context"
	"sync"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

// ========================================
// UI 类型常量
// ========================================

// UIType 前端渲染事件类型 (17 种, 完整覆盖 codex/events.go 全部事件)。
type UIType string

const (
	UITypeAssistantDelta  UIType = "assistant_delta"
	UITypeAssistantDone   UIType = "assistant_done"
	UITypeReasoningDelta  UIType = "reasoning_delta"
	UITypeCommandStart    UIType = "command_start"
	UITypeCommandOutput   UIType = "command_output"
	UITypeCommandDone     UIType = "command_done"
	UITypeFileEditStart   UIType = "file_edit_start"
	UITypeFileEditDone    UIType = "file_edit_done"
	UITypeToolCall        UIType = "tool_call"
	UITypeApprovalRequest UIType = "approval_request"
	UITypePlanDelta       UIType = "plan_delta"
	UITypeTurnStarted     UIType = "turn_started"
	UITypeTurnComplete    UIType = "turn_complete"
	UITypeDiffUpdate      UIType = "diff_update"
	UITypeUserMessage     UIType = "user_message"
	UITypeError           UIType = "error"
	UITypeSystem          UIType = "system"
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
	Files    []string `json:"files,omitempty"` // 涉及文件列表 (Go 提取)
	Ref      string   `json:"ref,omitempty"`   // 引用 ID (run_id/thread_id)
	Error    string   `json:"error,omitempty"` // 错误信息
	ExitCode *int     `json:"exitCode,omitempty"`
}

// ========================================
// 偏好管理
// ========================================

// PreferenceManager handles UI preference logic.
// 当 store 为 nil 时，降级为内存存储。
type PreferenceManager struct {
	store    *store.UIPreferenceStore
	fallback sync.Map // nil-store 时的内存降级
}

// NewPreferenceManager 创建偏好管理器。
func NewPreferenceManager(s *store.UIPreferenceStore) *PreferenceManager {
	return &PreferenceManager{store: s}
}

// Get retrieves a single preference.
func (m *PreferenceManager) Get(ctx context.Context, key string) (any, error) {
	if m.store == nil {
		v, _ := m.fallback.Load(key)
		return v, nil
	}
	return m.store.Get(ctx, key)
}

// Set updates a preference.
func (m *PreferenceManager) Set(ctx context.Context, key string, value any) error {
	if m.store == nil {
		m.fallback.Store(key, value)
		return nil
	}
	return m.store.Set(ctx, key, value)
}

// GetAll retrieves all preferences.
func (m *PreferenceManager) GetAll(ctx context.Context) (map[string]any, error) {
	if m.store == nil {
		result := make(map[string]any)
		m.fallback.Range(func(k, v any) bool {
			result[k.(string)] = v
			return true
		})
		return result, nil
	}
	return m.store.GetAll(ctx)
}
