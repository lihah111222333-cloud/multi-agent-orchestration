package difftracker

type DiffResult struct {
	ThreadID      string
	CodexThreadID string
	Tool          string
	DiffText      string
	Files         []string
}

type (
	DiffEmitter     func(DiffResult)
	WorkDirResolver func(agentID string) string
)
