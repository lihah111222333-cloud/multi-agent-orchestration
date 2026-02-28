package difftracker

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type Tracker struct {
	enabled             bool
	repoRoot            string
	beforeFileSnapshots map[string]FileContentSnapshot
}

func (t Tracker) Enabled() bool { return t.enabled }

func BeginTracker(agentID, tool string, args map[string]any, resolveWorkDir WorkDirResolver) Tracker {
	if !ShouldCaptureDynamicToolDiff(tool, args) {
		return Tracker{}
	}
	if repoRoot := ResolveDynamicToolDiffRepoRoot(agentID, args, resolveWorkDir); repoRoot == "" {
		return Tracker{}
	} else {
		paths, err := ListRepoDirtyPaths(repoRoot)
		if err != nil {
			logger.Debug("dynamic-tool: capture pre-dispatch dirty paths failed",
				logger.FieldAgentID, agentID,
				logger.FieldToolName, tool,
				logger.FieldPath, repoRoot,
				logger.FieldError, err,
			)
			return Tracker{}
		}
		return Tracker{
			enabled:             true,
			repoRoot:            repoRoot,
			beforeFileSnapshots: CaptureWorkingTreeFileSnapshots(repoRoot, paths),
		}
	}
}

func (t Tracker) EmitDiffUpdate(threadID, codexThreadID, tool string, emit DiffEmitter) {
	if !t.enabled || emit == nil {
		return
	}

	if threadID = strings.TrimSpace(threadID); threadID == "" {
		return
	}
	logDebug := func(message string, err error) {
		logger.Debug(message,
			logger.FieldThreadID, threadID,
			logger.FieldToolName, tool,
			logger.FieldPath, t.repoRoot,
			logger.FieldError, err,
		)
	}

	afterPaths, err := ListRepoDirtyPaths(t.repoRoot)
	if err != nil {
		logDebug("dynamic-tool: capture post-dispatch dirty paths failed", err)
		return
	}

	if incrementalDiff, err := BuildIncrementalDiffText(t.repoRoot, t.beforeFileSnapshots, afterPaths); err != nil {
		logDebug("dynamic-tool: build incremental diff failed", err)
		return
	} else if incrementalDiff != "" {
		emit(DiffResult{
			ThreadID:      threadID,
			CodexThreadID: codexThreadID,
			Tool:          tool,
			DiffText:      incrementalDiff,
		})
		logger.Info("dynamic-tool: turn diff updated",
			logger.FieldThreadID, threadID,
			logger.FieldToolName, tool,
			"repo_root", t.repoRoot,
			"before_paths", len(t.beforeFileSnapshots),
			"after_paths", len(afterPaths),
			"new_len", len(incrementalDiff),
		)
	}
}
