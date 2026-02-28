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

type PreferenceManager struct {
	store    *store.UIPreferenceStore
	fallback sync.Map
}

func NewPreferenceManager(s *store.UIPreferenceStore) *PreferenceManager {
	return &PreferenceManager{store: s}
}

func (m *PreferenceManager) Get(ctx context.Context, key string) (any, error) {
	if store := m.store; store != nil {
		return store.Get(ctx, key)
	}
	v, _ := m.fallback.Load(key)
	return v, nil
}

func (m *PreferenceManager) Set(ctx context.Context, key string, value any) error {
	if store := m.store; store != nil {
		return store.Set(ctx, key, value)
	}
	m.fallback.Store(key, value)
	return nil
}

func (m *PreferenceManager) GetAll(ctx context.Context) (map[string]any, error) {
	if store := m.store; store != nil {
		return store.GetAll(ctx)
	}
	result := map[string]any{}
	m.fallback.Range(func(key, value any) bool {
		result[key.(string)] = value
		return true
	})
	return result, nil
}
