package difftracker

// DiffResult carries emitted diff payload for UI/state consumers.
type DiffResult struct {
	ThreadID      string
	CodexThreadID string
	Tool          string
	DiffText      string
	Files         []string
}

// DiffEmitter emits a diff result.
type DiffEmitter func(result DiffResult)

// WorkDirResolver resolves agent workdir by agent ID.
type WorkDirResolver func(agentID string) string
