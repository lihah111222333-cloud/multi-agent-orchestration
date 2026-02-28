package uistate

import (
	"context"
	"sync"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

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

type NormalizedEvent struct {
	UIType   UIType   `json:"uiType"`
	Text     string   `json:"text,omitempty"`
	Command  string   `json:"command,omitempty"`
	File     string   `json:"file,omitempty"`
	Files    []string `json:"files,omitempty"`
	Ref      string   `json:"ref,omitempty"`
	Error    string   `json:"error,omitempty"`
	ExitCode *int     `json:"exitCode,omitempty"`
	RawType  string   `json:"-"`
	Method   string   `json:"-"`
}

// PreferenceManager handles UI preferences and uses memory fallback when store is nil.
type PreferenceManager struct {
	store    *store.UIPreferenceStore
	fallback sync.Map
}

func NewPreferenceManager(s *store.UIPreferenceStore) *PreferenceManager {
	return &PreferenceManager{store: s}
}

func (m *PreferenceManager) Get(ctx context.Context, key string) (any, error) {
	if m.store == nil {
		v, _ := m.fallback.Load(key)
		return v, nil
	}
	return m.store.Get(ctx, key)
}

func (m *PreferenceManager) Set(ctx context.Context, key string, value any) error {
	if m.store == nil {
		m.fallback.Store(key, value)
		return nil
	}
	return m.store.Set(ctx, key, value)
}

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
