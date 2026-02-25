package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultLSPUsagePromptHint = ""
	maxLSPUsagePromptHintLen  = 16000
)

type allSchemasProvider interface {
	AllSchemas() []agentcore.DynamicTool
}

type agentWorkDirSetter interface {
	SetAgentWorkDir(agentID, cwd string)
}

type codeRunCanceler interface {
	CancelCodeRuns(agentID string) int
}

type skillContentReader interface {
	ReadSkillContent(skillName string) (string, error)
}

type skillNamesLister interface {
	ListSkillNames() ([]string, error)
}

type skillMatchCandidatesLister interface {
	ListSkillMatchCandidates() ([]SkillMatchCandidate, error)
}

type agentSkillsGetter interface {
	GetAgentSkills(agentID string) []string
}

type turnStartSubmissionProvider interface {
	PrepareTurnStartSubmission(threadID string, input []TurnInput, selectedSkills []string, manualSkillSelection bool) (TurnStartEntryPrepareResult, error)
}

type turnStartTimelineProvider interface {
	AppendTurnStartUserTimeline(ctx context.Context, input []TurnInput, opt TurnAppendUserTimelineOptions)
}

type turnSteerSubmissionProvider interface {
	PrepareTurnSteerSubmission(threadID string, input []TurnInput, selectedSkills []string, manualSkillSelection bool) (TurnSteerEntryPrepareResult, error)
}

func (a *Adapter) allDynamicToolSchemas() []agentcore.DynamicTool {
	provider, ok := any(a.ctx).(allSchemasProvider)
	if !ok {
		return nil
	}
	return provider.AllSchemas()
}

func (a *Adapter) resolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []agentcore.DynamicTool) string {
	hint := a.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	startInstructions, warnings := a.PrependLSPAvailabilityWarning(hint, dynamicTools, commonadapter.MergePromptText)
	if len(warnings) > 0 {
		logger.Warn("codexadapter: start instructions warnings: " + strings.Join(warnings, "; "))
	}
	return startInstructions
}

func (a *Adapter) setAgentWorkDir(agentID string, cwd string) {
	setter, ok := any(a.ctx).(agentWorkDirSetter)
	if !ok {
		return
	}
	setter.SetAgentWorkDir(agentID, cwd)
}

func (a *Adapter) cancelCodeRuns(agentID string) int {
	canceler, ok := any(a.ctx).(codeRunCanceler)
	if !ok {
		return 0
	}
	return canceler.CancelCodeRuns(agentID)
}

func (a *Adapter) readSkillContent(skillName string) (string, error) {
	if strings.TrimSpace(skillName) == "" {
		return "", appErrors.New("codexadapter.readSkillContent", "skill name is required")
	}
	reader, ok := any(a.ctx).(skillContentReader)
	if !ok {
		return "", appErrors.New("codexadapter.readSkillContent", "server context does not support reading skill content")
	}
	return reader.ReadSkillContent(skillName)
}

func (a *Adapter) listSkillNames() ([]string, error) {
	lister, ok := any(a.ctx).(skillNamesLister)
	if !ok {
		return nil, appErrors.New("codexadapter.listSkillNames", "server context does not support listing skill names")
	}
	return lister.ListSkillNames()
}

func (a *Adapter) listSkillMatchCandidates() ([]SkillMatchCandidate, error) {
	lister, ok := any(a.ctx).(skillMatchCandidatesLister)
	if !ok {
		return nil, appErrors.New("codexadapter.listSkillMatchCandidates", "server context does not support listing skill match candidates")
	}
	return lister.ListSkillMatchCandidates()
}

func (a *Adapter) listAgentSkills(agentID string) []string {
	getter, ok := any(a.ctx).(agentSkillsGetter)
	if !ok {
		return nil
	}
	return getter.GetAgentSkills(agentID)
}
