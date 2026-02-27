package prompt

import (
	promptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/prompt"
)

var (
	BuildSelectedSkillPrompt       = promptsvc.BuildSelectedSkillPrompt
	ResolveLSPUsagePromptHint      = promptsvc.ResolveLSPUsagePromptHint
	CollectDynamicToolNames        = promptsvc.CollectDynamicToolNames
	PrependLSPAvailabilityWarning  = promptsvc.PrependLSPAvailabilityWarning
	CollectReferencedLSPToolNames  = promptsvc.CollectReferencedLSPToolNames
	CollectAutoMatchedSkillMatches = promptsvc.CollectAutoMatchedSkillMatches
	RenderAutoMatchedSkillPrompt   = promptsvc.RenderAutoMatchedSkillPrompt
)
