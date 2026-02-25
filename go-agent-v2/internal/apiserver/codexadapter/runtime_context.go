package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
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
	if a == nil || a.ctx == nil {
		return nil
	}
	provider, ok := any(a.ctx).(allSchemasProvider)
	if !ok {
		return nil
	}
	return provider.AllSchemas()
}

func (a *Adapter) resolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []agentcore.DynamicTool) string {
	hint := a.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	instructions, missing := a.PrependLSPAvailabilityWarning(hint, dynamicTools, commonadapter.MergePromptText)
	if len(missing) == 0 {
		return instructions
	}
	logger.Warn("lsp hint references unavailable tools during launch",
		"missing_lsp_tools", strings.Join(missing, ","),
	)
	return instructions
}

func (a *Adapter) setAgentWorkDir(agentID, cwd string) {
	if a == nil || a.ctx == nil {
		return
	}
	setter, ok := any(a.ctx).(agentWorkDirSetter)
	if !ok {
		return
	}
	setter.SetAgentWorkDir(agentID, cwd)
}

func (a *Adapter) cancelCodeRuns(agentID string) int {
	if a == nil || a.ctx == nil {
		return 0
	}
	canceler, ok := any(a.ctx).(codeRunCanceler)
	if !ok {
		return 0
	}
	return canceler.CancelCodeRuns(agentID)
}

func (a *Adapter) readSkillContent(skillName string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", apperrors.New("codexadapter.readSkillContent", "server context is not available")
	}
	reader, ok := any(a.ctx).(skillContentReader)
	if !ok {
		return "", apperrors.New("codexadapter.readSkillContent", "skill content reader is not available")
	}
	return reader.ReadSkillContent(skillName)
}

func (a *Adapter) listSkillNames() ([]string, error) {
	if a == nil || a.ctx == nil {
		return []string{}, nil
	}
	lister, ok := any(a.ctx).(skillNamesLister)
	if !ok {
		return []string{}, nil
	}
	names, err := lister.ListSkillNames()
	if err != nil {
		return nil, err
	}
	if names == nil {
		return []string{}, nil
	}
	return names, nil
}

func (a *Adapter) prepareTurnStartSubmission(threadID string, input []TurnInput, selectedSkills []string, manualSkillSelection bool) (TurnStartEntryPrepareResult, error) {
	if a == nil || a.ctx == nil {
		return TurnStartEntryPrepareResult{}, apperrors.New("codexadapter.prepareTurnStartSubmission", "server context is not available")
	}
	provider, ok := any(a.ctx).(turnStartSubmissionProvider)
	if !ok {
		return TurnStartEntryPrepareResult{}, apperrors.New("codexadapter.prepareTurnStartSubmission", "turn start submission provider is not available")
	}
	return provider.PrepareTurnStartSubmission(threadID, input, selectedSkills, manualSkillSelection)
}

func (a *Adapter) appendTurnStartUserTimeline(ctx context.Context, input []TurnInput, opt TurnAppendUserTimelineOptions) {
	if a == nil || a.ctx == nil {
		return
	}
	provider, ok := any(a.ctx).(turnStartTimelineProvider)
	if !ok {
		return
	}
	provider.AppendTurnStartUserTimeline(ctx, input, opt)
}

func (a *Adapter) prepareTurnSteerSubmission(threadID string, input []TurnInput, selectedSkills []string, manualSkillSelection bool) (TurnSteerEntryPrepareResult, error) {
	if a == nil || a.ctx == nil {
		return TurnSteerEntryPrepareResult{}, apperrors.New("codexadapter.prepareTurnSteerSubmission", "server context is not available")
	}
	provider, ok := any(a.ctx).(turnSteerSubmissionProvider)
	if !ok {
		return TurnSteerEntryPrepareResult{}, apperrors.New("codexadapter.prepareTurnSteerSubmission", "turn steer submission provider is not available")
	}
	return provider.PrepareTurnSteerSubmission(threadID, input, selectedSkills, manualSkillSelection)
}
