package uistate

import (
	"encoding/json"
	"time"
)

// ThreadSnapshot is UI-ready thread info.
type ThreadSnapshot struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// TimelineAttachment is a lightweight attachment reference.
type TimelineAttachment struct {
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Path       string `json:"path,omitempty"`
	PreviewURL string `json:"previewUrl,omitempty"`
}

// TimelineItem is the unified render item for chat timeline.
type TimelineItem struct {
	ID          string               `json:"id"`
	Ts          string               `json:"ts"`
	Kind        string               `json:"kind"`
	Text        string               `json:"text,omitempty"`
	Attachments []TimelineAttachment `json:"attachments,omitempty"`
	Done        bool                 `json:"done,omitempty"`
	Command     string               `json:"command,omitempty"`
	Output      string               `json:"output,omitempty"`
	Status      string               `json:"status,omitempty"`
	ExitCode    *int                 `json:"exitCode,omitempty"`
	File        string               `json:"file,omitempty"`
	Tool        string               `json:"tool,omitempty"`
	Preview     string               `json:"preview,omitempty"`
	ElapsedMS   *int                 `json:"elapsedMs,omitempty"`
}

// AgentMeta tracks runtime meta for thread cards.
type AgentMeta struct {
	Alias        string `json:"alias,omitempty"`
	LastActiveAt string `json:"lastActiveAt,omitempty"`
	IsMain       bool   `json:"isMain,omitempty"`
}

// TokenUsageSnapshot stores context-window token usage for UI.
type TokenUsageSnapshot struct {
	UsedTokens          int     `json:"usedTokens"`
	ContextWindowTokens int     `json:"contextWindowTokens,omitempty"`
	UsedPercent         float64 `json:"usedPercent,omitempty"`
	LeftPercent         float64 `json:"leftPercent,omitempty"`
	UpdatedAt           string  `json:"updatedAt,omitempty"`
}

// ActivityStats holds per-thread cumulative activity counters.
type ActivityStats struct {
	LSPCalls  int64            `json:"lspCalls"`
	Commands  int64            `json:"commands"`
	FileEdits int64            `json:"fileEdits"`
	ToolCalls map[string]int64 `json:"toolCalls"`
}

// AlertEntry is a single high-priority alert for the UI panel.
type AlertEntry struct {
	ID      string `json:"id"`
	Time    string `json:"time"`
	Level   string `json:"level"` // "error" | "warning" | "stall"
	Message string `json:"message"`
}

// RuntimeSnapshot is a full UI runtime state snapshot.
type RuntimeSnapshot struct {
	Threads                 []ThreadSnapshot              `json:"threads"`
	Statuses                map[string]string             `json:"statuses"`
	InterruptibleByThread   map[string]bool               `json:"interruptibleByThread"`
	StatusHeadersByThread   map[string]string             `json:"statusHeadersByThread"`
	StatusDetailsByThread   map[string]string             `json:"statusDetailsByThread"`
	TimelinesByThread       map[string][]TimelineItem     `json:"timelinesByThread"`
	DiffTextByThread        map[string]string             `json:"diffTextByThread"`
	TokenUsageByThread      map[string]TokenUsageSnapshot `json:"tokenUsageByThread"`
	WorkspaceRunsByKey      map[string]map[string]any     `json:"workspaceRunsByKey"`
	WorkspaceFeatureEnabled *bool                         `json:"workspaceFeatureEnabled"`
	WorkspaceLastError      string                        `json:"workspaceLastError"`
	AgentMetaByID           map[string]AgentMeta          `json:"agentMetaById"`
	ActivityStatsByThread   map[string]ActivityStats      `json:"activityStatsByThread"`
	AlertsByThread          map[string][]AlertEntry       `json:"alertsByThread"`
}

// HistoryRecord is a compact history message for timeline hydration.
type HistoryRecord struct {
	ID        int64
	Role      string
	EventType string
	Method    string
	Content   string
	Metadata  json.RawMessage
	CreatedAt time.Time
}

type threadRuntime struct {
	thinkingIndex  int
	assistantIndex int
	commandIndex   int
	planIndex      int
	editingFiles   map[string]struct{}

	turnDepth      int
	approvalDepth  int
	userInputDepth int
	commandDepth   int
	fileEditDepth  int
	toolCallDepth  int
	collabDepth    int

	terminalWaitOverlay bool
	terminalWaitLabel   string
	mcpStartupOverlay   bool
	mcpStartupLabel     string
	backgroundOverlay   bool
	backgroundLabel     string
	backgroundDetails   string
	streamErrorText     string
	streamErrorDetails  string
	statusHeader        string
	reasoningHeaderBuf  string
	hasDerivedState     bool
}

func newThreadRuntime() *threadRuntime {
	return &threadRuntime{
		thinkingIndex:  -1,
		assistantIndex: -1,
		commandIndex:   -1,
		planIndex:      -1,
		editingFiles:   map[string]struct{}{},
	}
}
