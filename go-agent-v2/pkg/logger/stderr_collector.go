package logger

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"strings"
)

type StderrCollector struct {
	pr      *io.PipeReader
	pw      *io.PipeWriter
	agentID string
	done    chan struct{}
}

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

func (c *StderrCollector) Write(p []byte) (int, error) {
	return c.pw.Write(p)
}

func (c *StderrCollector) Close() error { _ = c.pw.Close(); <-c.done; return nil }

func (c *StderrCollector) scan() {
	defer close(c.done)
	defer func() { _ = c.pr.Close() }()

	scanner := bufio.NewScanner(c.pr)
	ctx := context.Background()
	baseArgs := []any{
		FieldSource, "codex",
		FieldComponent, "stderr",
		FieldAgentID, c.agentID,
		"logger", "codex.stderr",
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		level := slog.LevelInfo
		if containsErrorKeyword(line) {
			level = slog.LevelError
		}
		getLogger().Log(ctx, level, line, baseArgs...)
	}

	if err := scanner.Err(); err != nil {
		getLogger().Log(ctx, slog.LevelError, "stderr collector scan failed",
			append(append([]any(nil), baseArgs...), "error", err.Error())...,
		)
	}
}

func containsErrorKeyword(line string) bool {
	line = strings.ToLower(line)
	return strings.Contains(line, "error") ||
		strings.Contains(line, "panic") ||
		strings.Contains(line, "fatal")
}
