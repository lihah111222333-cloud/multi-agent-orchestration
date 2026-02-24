// methods_command.go — command/exec JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	"curl":     true, // 防止外部请求
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

func (s *Server) commandExecTyped(ctx context.Context, p commandExecParams) (any, error) {
	if len(p.Argv) == 0 {
		return nil, apperrors.New("Server.commandExec", "argv is required")
	}

	// 安全检查: 提取基础命令名 (去掉路径)
	baseName := filepath.Base(p.Argv[0])
	if commandBlocklist[baseName] {
		return nil, apperrors.Newf("Server.commandExec", "command %q is blocked for security", baseName)
	}

	// 禁止管道/shell 注入: 检查参数中是否有 shell 元字符
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
				continue // 跳过不允许的环境变量
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

	// 阅读类命令: 注入 LSP 工具优先提示 (不阻止执行)
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
