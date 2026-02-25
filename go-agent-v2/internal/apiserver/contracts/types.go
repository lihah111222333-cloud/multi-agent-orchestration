package contracts

import (
	"encoding/json"
	"time"
)

// AutoMatchInput carries user input metadata used for skill auto-match.
type AutoMatchInput struct {
	Type string
	Name string
}

// SkillMatchCandidate describes one skill candidate for auto-match classification.
type SkillMatchCandidate struct {
	Name         string
	ForceWords   []string
	TriggerWords []string
}

// AutoMatchedSkillMatch stores one matched skill classification result.
type AutoMatchedSkillMatch struct {
	Name         string
	MatchedBy    string
	MatchedTerms []string
}

// AutoSkillMatchOptions controls how configured skills participate in auto-match.
type AutoSkillMatchOptions struct {
	IncludeConfiguredExplicit bool
	IncludeConfiguredForce    bool
}

// TurnInput is a protocol-level user input item for turn/start and turn/steer.
type TurnInput struct {
	Type    string
	Text    string
	URL     string
	Path    string
	Name    string
	Content string
}

// TurnStartRequest carries protocol params for turn/start.
type TurnStartRequest struct {
	ThreadID             string
	Cwd                  string
	Input                []TurnInput
	SelectedSkills       []string
	ManualSkillSelection bool
	OutputSchema         json.RawMessage
}

// TurnAppendUserTimelineOptions configures turn/start user timeline rendering.
type TurnAppendUserTimelineOptions struct {
	ThreadID     string
	Prompt       string
	SubmitPrompt string
	Images       []string
	Files        []string
}

// TurnStartEntryPrepareResult contains prepared submit payload for turn/start.
type TurnStartEntryPrepareResult struct {
	Prompt                string
	SubmitPrompt          string
	Images                []string
	Files                 []string
	SelectedSkillCount    int
	AutoMatchedSkillCount int
}

// TurnSteerEntryPrepareResult contains prepared submit payload for turn/steer.
type TurnSteerEntryPrepareResult struct {
	SubmitPrompt string
	Images       []string
	Files        []string
}

// TurnSteerRequest carries protocol params for turn/steer.
type TurnSteerRequest struct {
	ThreadID             string
	Input                []TurnInput
	SelectedSkills       []string
	ManualSkillSelection bool
}

// ThreadListItem models one thread list payload entry.
type ThreadListItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// TrackedTurn is the cross-package turn lifecycle tracking state.
type TrackedTurn struct {
	ID                   string
	ThreadID             string
	StartedAt            time.Time
	LastEventAt          time.Time
	InterruptRequested   bool
	InterruptRequestedAt time.Time
	StallHintLogged      bool
	StallGraceStarted    bool
	StallAutoInterrupted bool
	Done                 chan string
	Timer                *time.Timer
	StallTimer           *time.Timer
}

// TrackedTurnSummaryCacheEntry caches the latest summary for one tracked turn.
type TrackedTurnSummaryCacheEntry struct {
	TurnID    string
	Summary   string
	UpdatedAt time.Time
}
