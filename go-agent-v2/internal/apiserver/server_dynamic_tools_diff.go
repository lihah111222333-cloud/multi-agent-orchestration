package apiserver

import (
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/diffsdk/difftracker"
)

var shouldCaptureDynamicToolDiff = difftracker.ShouldCaptureDynamicToolDiff
var listRepoDirtyPaths = difftracker.ListRepoDirtyPaths
var captureWorkingTreeFileSnapshots = difftracker.CaptureWorkingTreeFileSnapshots
var buildIncrementalDiffText = difftracker.BuildIncrementalDiffText

func resolveDynamicToolDiffRepoRoot(s *Server, agentID string, args map[string]any) string {
	return difftracker.ResolveDynamicToolDiffRepoRoot(agentID, args, func(id string) string { return getAgentWorkDirState(s, id) })
}

func beginDynamicToolDiffTracker(s *Server, agentID, tool string, args map[string]any) difftracker.Tracker {
	return difftracker.BeginTracker(agentID, tool, args, func(id string) string { return getAgentWorkDirState(s, id) })
}

func maybeEmitDynamicToolDiffUpdate(s *Server, threadID, codexThreadID, tool string, tracker difftracker.Tracker) {
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
		if ui := s.uiRuntime; ui != nil {
			ui.ApplyAgentEvent(result.ThreadID, normalized, payload)
		}
		notify(s, "turn/diff/updated", payload)
	})
}
