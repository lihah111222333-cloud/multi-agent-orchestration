package apiserver

import (
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/diffsdk/difftracker"
)

type dynamicToolDiffTracker = difftracker.Tracker
type fileContentSnapshot = difftracker.FileContentSnapshot

func shouldCaptureDynamicToolDiff(tool string, args map[string]any) bool {
	return difftracker.ShouldCaptureDynamicToolDiff(tool, args)
}

func resolveDynamicToolDiffRepoRoot(s *Server, agentID string, args map[string]any) string {
	return difftracker.ResolveDynamicToolDiffRepoRoot(agentID, args, func(id string) string {
		return getAgentWorkDirState(s, id)
	})
}

func listRepoDirtyPaths(repoRoot string) ([]string, error) {
	return difftracker.ListRepoDirtyPaths(repoRoot)
}

func captureWorkingTreeFileSnapshots(repoRoot string, paths []string) map[string]fileContentSnapshot {
	return difftracker.CaptureWorkingTreeFileSnapshots(repoRoot, paths)
}

func buildIncrementalDiffText(
	repoRoot string,
	beforeFileSnapshots map[string]fileContentSnapshot,
	afterPaths []string,
) (string, error) {
	return difftracker.BuildIncrementalDiffText(repoRoot, beforeFileSnapshots, afterPaths)
}

func beginDynamicToolDiffTracker(s *Server, agentID, tool string, args map[string]any) dynamicToolDiffTracker {
	return difftracker.BeginTracker(agentID, tool, args, func(id string) string {
		return getAgentWorkDirState(s, id)
	})
}

func maybeEmitDynamicToolDiffUpdate(s *Server, threadID, codexThreadID, tool string, tracker dynamicToolDiffTracker) {
	if s == nil {
		return
	}

	tracker.EmitDiffUpdate(threadID, codexThreadID, tool, func(result difftracker.DiffResult) {
		payload := map[string]any{
			"diff":   result.DiffText,
			"uiText": result.DiffText,
			"tool":   result.Tool,
		}
		if result.CodexThreadID != "" {
			payload["codexThreadId"] = result.CodexThreadID
		}

		files := result.Files
		if len(files) == 0 {
			files = parseFilesFromPatchDelta(result.DiffText)
		}
		if len(files) > 0 {
			payload["files"] = files
			payload["file"] = files[0]
		}

		normalized := uistate.NormalizeEventFromPayload(agentcore.EventTurnDiff, "turn/diff/updated", payload)
		payload["uiType"] = string(normalized.UIType)
		if s.uiRuntime != nil {
			s.uiRuntime.ApplyAgentEvent(result.ThreadID, normalized, payload)
		}
		notify(s, "turn/diff/updated", payload)
	})
}
