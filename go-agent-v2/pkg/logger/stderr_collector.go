package logger

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"strings"
)

// StderrCollector 将 codex 进程的 stderr 逐行转为 slog 日志。
//
// 实现 io.Writer 接口，可直接赋给 exec.Cmd.Stderr。
// 内部使用 goroutine + bufio.Scanner 逐行读取。
type StderrCollector struct {
	pr      *io.PipeReader
	pw      *io.PipeWriter
	agentID string
	done    chan struct{}
}

// NewStderrCollector 创建 StderrCollector。agentID 关联日志行。
func NewStderrCollector(agentID string) *StderrCollector {
	pr, pw := io.Pipe()
	c := &StderrCollector{
		pr:      pr,
		pw:      pw,
		agentID: agentID,
		done:    make(chan struct{}),
	}
	go c.scan()
	return c
}

// Write 实现 io.Writer — exec.Cmd.Stderr 直接写入。
func (c *StderrCollector) Write(p []byte) (int, error) {
	return c.pw.Write(p)
}

// Close 关闭 writer 端，等待 scanner 完成。
func (c *StderrCollector) Close() error {
	_ = c.pw.Close()
	<-c.done
	return nil
}

// scan 后台逐行读取 stderr → slog。
func (c *StderrCollector) scan() {
	defer close(c.done)
	defer func() { _ = c.pr.Close() }()

	scanner := bufio.NewScanner(c.pr)
	// 默认 64KB 行缓冲已足够 codex stderr

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// 简单启发式: 含 error/panic/fatal 视为 ERROR 级别
		level := slog.LevelInfo
		if containsErrorKeyword(line) {
			level = slog.LevelError
		}

		getLogger().Log(context.Background(), level, line,
			FieldSource, "codex",
			FieldComponent, "stderr",
			FieldAgentID, c.agentID,
			"logger", "codex.stderr",
		)
	}

	if err := scanner.Err(); err != nil {
		getLogger().Log(context.Background(), slog.LevelError, "stderr collector scan failed",
			FieldSource, "codex",
			FieldComponent, "stderr",
			FieldAgentID, c.agentID,
			"logger", "codex.stderr",
			"error", err.Error(),
		)
	}
}

// containsErrorKeyword 判断 stderr 行中是否包含错误关键词 (大小写不敏感)。
func containsErrorKeyword(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "panic") ||
		strings.Contains(lower, "fatal")
}
