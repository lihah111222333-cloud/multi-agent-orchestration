package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/service"
	skillsruntime "github.com/multi-agent/go-agent-v2/internal/skills"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type commandExecParams struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

// commandBlocklist 禁止通过 command/exec 执行的危险命令。
var commandBlocklist = map[string]bool{
	"rm":       true,
	"rmdir":    true,
	"sudo":     true,
	"su":       true,
	"chmod":    true,
	"chown":    true,
	"mkfs":     true,
	"dd":       true,
	"kill":     true,
	"killall":  true,
	"pkill":    true,
	"shutdown": true,
	"reboot":   true,
	"passwd":   true,
	"useradd":  true,
	"userdel":  true,
	"mount":    true,
	"umount":   true,
	"fdisk":    true,
	"iptables": true,
	"curl":     true,
	"wget":     true,
}

const maxOutputSize = 1 << 20 // 1MB 输出限制

// readCommands 阅读类命令集合 — 检测到时注入 LSP 工具优先提示。
var readCommands = map[string]bool{
	"cat":  true,
	"head": true,
	"tail": true,
	"less": true,
	"more": true,
	"bat":  true,
	"grep": true,
	"rg":   true,
	"ag":   true,
	"find": true,
	"fd":   true,
	"tree": true,
	"wc":   true,
	"sed":  true,
	"awk":  true,
}

const lspPreferenceHint = "[LSP提示] 你有 19 个 LSP 工具可用于代码理解，请优先使用 LSP 工具而非命令行读取代码。\n---\n"

// commandExecResponse command/exec 响应。
type commandExecResponse struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func commandExecTyped(_ *Server, ctx context.Context, p commandExecParams) (any, error) {
	if len(p.Argv) == 0 {
		return nil, apperrors.New("Server.commandExec", "argv is required")
	}

	baseName := filepath.Base(p.Argv[0])
	if commandBlocklist[baseName] {
		return nil, apperrors.Newf("Server.commandExec", "command %q is blocked for security", baseName)
	}

	for _, arg := range p.Argv {
		if strings.ContainsAny(arg, "|;&$`") {
			return nil, apperrors.New("Server.commandExec", "shell metacharacters not allowed in arguments")
		}
	}

	logger.Info("command/exec: starting",
		logger.FieldCommand, baseName,
		logger.FieldCwd, p.Cwd,
		"argc", len(p.Argv),
	)

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, p.Argv[0], p.Argv[1:]...)
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}
	if len(p.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range p.Env {
			if !isAllowedEnvKey(k) {
				continue
			}
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// 限制输出大小, 防止内存耗尽
	var stdout, stderr strings.Builder
	stdout.Grow(4096)
	stderr.Grow(4096)
	cmd.Stdout = util.NewLimitedWriter(&stdout, maxOutputSize)
	cmd.Stderr = util.NewLimitedWriter(&stderr, maxOutputSize)

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			logger.Error("command/exec: run failed",
				logger.FieldCommand, baseName,
				logger.FieldError, err,
				logger.FieldDurationMS, elapsed.Milliseconds(),
			)
			return nil, apperrors.Wrap(err, "Server.commandExec", "run command")
		}
	}

	logger.Info("command/exec: completed",
		logger.FieldCommand, baseName,
		logger.FieldExitCode, exitCode,
		logger.FieldDurationMS, elapsed.Milliseconds(),
	)

	outStr := stdout.String()

	if readCommands[baseName] {
		logger.Info("command/exec: read command detected, injecting LSP hint",
			logger.FieldCommand, baseName,
		)
		outStr = fmt.Sprintf("%s%s", lspPreferenceHint, outStr)
	}

	return commandExecResponse{
		ExitCode: exitCode,
		Stdout:   outStr,
		Stderr:   stderr.String(),
	}, nil
}

type skillsLocalReadParams = skillsruntime.SkillsLocalReadParams
type skillsLocalImportDirParams = skillsruntime.SkillsLocalImportDirParams
type skillsLocalDeleteParams = skillsruntime.SkillsLocalDeleteParams
type skillsConfigWriteParams = skillsruntime.SkillsConfigWriteParams
type skillsSummaryWriteParams = skillsruntime.SkillsSummaryWriteParams
type skillsMatchPreviewParams = skillsruntime.SkillsMatchPreviewParams
type skillsConfigReadParams = skillsruntime.SkillsConfigReadParams
type skillsRemoteReadParams = skillsruntime.SkillsRemoteReadParams
type skillsRemoteWriteParams = skillsruntime.SkillsRemoteWriteParams

func newSkillsManager(s *Server) *skillsruntime.Manager {
	if s == nil {
		return skillsruntime.NewManager(nil, nil)
	}

	provider := skillsruntime.SkillServiceProviderFunc(func() *service.SkillService {
		return s.skillSvc
	})
	collector := skillsruntime.AutoMatchCollectorFunc(func(
		threadID string,
		prompt string,
		input []skillsruntime.UserInput,
		options skillsruntime.AutoSkillMatchOptions,
	) []skillsruntime.AutoMatchedSkillMatch {
		if s == nil || s.codexAdapter == nil {
			return nil
		}
		turnInput := make([]contracts.TurnInput, 0, len(input))
		for _, item := range input {
			turnInput = append(turnInput, contracts.TurnInput{
				Type:    item.Type,
				Text:    item.Text,
				URL:     item.URL,
				Path:    item.Path,
				Name:    item.Name,
				Content: item.Content,
			})
		}
		matches := s.codexAdapter.CollectAutoMatchedSkillMatchesForThread(threadID, prompt, turnInput, contracts.AutoSkillMatchOptions{
			IncludeConfiguredExplicit: options.IncludeConfiguredExplicit,
			IncludeConfiguredForce:    options.IncludeConfiguredForce,
		})
		out := make([]skillsruntime.AutoMatchedSkillMatch, 0, len(matches))
		for _, match := range matches {
			out = append(out, skillsruntime.AutoMatchedSkillMatch{
				Name:         match.Name,
				MatchedBy:    match.MatchedBy,
				MatchedTerms: append([]string(nil), match.MatchedTerms...),
			})
		}
		return out
	})
	return skillsruntime.NewManager(provider, collector)
}

func skillsManagerDelegate(s *Server) *skillsruntime.Manager {
	if s != nil && s.skillsMgr != nil {
		return s.skillsMgr
	}
	return newSkillsManager(s)
}

func skillsList(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	return skillsManagerDelegate(s).SkillsList(ctx)
}

func appList(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	return skillsManagerDelegate(s).AppList(ctx)
}

func skillsLocalReadTyped(s *Server, ctx context.Context, p skillsLocalReadParams) (any, error) {
	return skillsManagerDelegate(s).SkillsLocalRead(ctx, skillsruntime.SkillsLocalReadParams(p))
}

func skillsLocalImportDirTyped(s *Server, ctx context.Context, p skillsLocalImportDirParams) (any, error) {
	return skillsManagerDelegate(s).SkillsLocalImportDir(ctx, skillsruntime.SkillsLocalImportDirParams(p))
}

func skillsLocalDeleteTyped(s *Server, ctx context.Context, p skillsLocalDeleteParams) (any, error) {
	return skillsManagerDelegate(s).SkillsLocalDelete(ctx, skillsruntime.SkillsLocalDeleteParams(p))
}

func skillsMatchPreviewTyped(s *Server, ctx context.Context, p skillsMatchPreviewParams) (any, error) {
	return skillsManagerDelegate(s).SkillsMatchPreview(ctx, skillsruntime.SkillsMatchPreviewParams(p))
}

func skillsConfigReadTyped(s *Server, ctx context.Context, p skillsConfigReadParams) (any, error) {
	return skillsManagerDelegate(s).SkillsConfigRead(ctx, skillsruntime.SkillsConfigReadParams(p))
}

func skillsConfigWriteTyped(s *Server, ctx context.Context, p skillsConfigWriteParams) (any, error) {
	return skillsManagerDelegate(s).SkillsConfigWrite(ctx, skillsruntime.SkillsConfigWriteParams(p))
}

func skillsSummaryWriteTyped(s *Server, ctx context.Context, p skillsSummaryWriteParams) (any, error) {
	return skillsManagerDelegate(s).SkillsSummaryWrite(ctx, skillsruntime.SkillsSummaryWriteParams(p))
}

func skillsRemoteReadTyped(s *Server, ctx context.Context, p skillsRemoteReadParams) (any, error) {
	return skillsManagerDelegate(s).SkillsRemoteRead(ctx, skillsruntime.SkillsRemoteReadParams(p))
}

func skillsRemoteWriteTyped(s *Server, ctx context.Context, p skillsRemoteWriteParams) (any, error) {
	return skillsManagerDelegate(s).SkillsRemoteWrite(ctx, skillsruntime.SkillsRemoteWriteParams(p))
}

// getAgentSkills 返回指定 agent 配置的技能列表。
func getAgentSkills(s *Server, agentID string) []string {
	return skillsManagerDelegate(s).GetAgentSkills(agentID)
}

// listSkillMatchCandidates returns normalized candidates for adapter auto-match.
func listSkillMatchCandidates(s *Server) ([]contracts.SkillMatchCandidate, error) {
	if s == nil || s.skillSvc == nil {
		return nil, nil
	}
	allSkills, err := s.skillSvc.ListSkills()
	if err != nil {
		return nil, err
	}
	candidates := make([]contracts.SkillMatchCandidate, 0, len(allSkills))
	for _, skill := range allSkills {
		skillName := strings.TrimSpace(skill.Name)
		if skillName == "" {
			continue
		}
		candidates = append(candidates, contracts.SkillMatchCandidate{
			Name:         skillName,
			ForceWords:   append([]string(nil), skill.ForceWords...),
			TriggerWords: append([]string(nil), skill.TriggerWords...),
		})
	}
	return candidates, nil
}
