package codexadapter

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultLSPUsagePromptHint = ""
	maxLSPUsagePromptHintLen  = 16000
)

func (a *Adapter) allDynamicToolSchemas() []agentcore.DynamicTool {
	if a == nil || a.ctx == nil || a.ctx.AllSchemas == nil {
		return nil
	}
	return a.ctx.AllSchemas()
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
	if a == nil || a.ctx == nil || a.ctx.SetAgentWorkDir == nil {
		return
	}
	a.ctx.SetAgentWorkDir(agentID, cwd)
}

func (a *Adapter) cancelCodeRuns(agentID string) int {
	if a == nil || a.ctx == nil || a.ctx.CancelCodeRuns == nil {
		return 0
	}
	return a.ctx.CancelCodeRuns(agentID)
}

func (a *Adapter) nowUnixMilli() int64 {
	if a == nil || a.ctx == nil || a.ctx.NowUnixMilli == nil {
		return time.Now().UnixMilli()
	}
	return a.ctx.NowUnixMilli()
}

func (a *Adapter) readSkillContent(skillName string) (string, error) {
	if strings.TrimSpace(skillName) == "" {
		return "", appErrors.New("codexadapter.readSkillContent", "skill name is required")
	}
	if a == nil || a.ctx == nil || a.ctx.ReadSkillContent == nil {
		return "", appErrors.New("codexadapter.readSkillContent", "server context is not configured")
	}
	return a.ctx.ReadSkillContent(skillName)
}

func (a *Adapter) listSkillNames() ([]string, error) {
	if a == nil || a.ctx == nil || a.ctx.ListSkillNames == nil {
		return nil, appErrors.New("codexadapter.listSkillNames", "server context is not configured")
	}
	return a.ctx.ListSkillNames()
}

func (a *Adapter) listSkillMatchCandidates() ([]SkillMatchCandidate, error) {
	if a == nil || a.ctx == nil || a.ctx.ListSkillMatchCandidates == nil {
		return nil, appErrors.New("codexadapter.listSkillMatchCandidates", "server context is not configured")
	}
	return a.ctx.ListSkillMatchCandidates()
}

func (a *Adapter) listAgentSkills(agentID string) []string {
	if a == nil || a.ctx == nil || a.ctx.GetAgentSkills == nil {
		return nil
	}
	return a.ctx.GetAgentSkills(agentID)
}
