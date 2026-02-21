package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestE2E_TurnAbortBehavior 端到端验证: codex app-server 是否在 turn
// 正常执行期间就发出 turn_aborted / turn_complete, 导致 agent 过早回到 idle。
//
// 前置条件:
//   - codex CLI 已在 PATH (codex --version 可用)
//   - 有效 API key (OPENAI_API_KEY 或 codex 默认凭证)
//
// 运行: E2E=1 go test -v -race -run TestE2E_TurnAbortBehavior -timeout 180s ./internal/codex/
func TestE2E_TurnAbortBehavior(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("skip E2E: set E2E=1 to run real codex integration test")
	}

	// --- 找一个空闲端口 ---
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Logf("using port %d", port)

	// --- 收集事件 ---
	type eventRecord struct {
		Type string
		At   time.Time
		Data json.RawMessage
	}
	var (
		mu       sync.Mutex
		events   []eventRecord
		turnDone = make(chan struct{}, 1)
	)

	client := NewAppServerClient(port, "e2e-turn-abort-test")
	client.SetEventHandler(func(e Event) {
		mu.Lock()
		rec := eventRecord{Type: e.Type, At: time.Now(), Data: e.Data}
		events = append(events, rec)
		mu.Unlock()

		t.Logf("[EVENT] type=%-30s data_len=%d", e.Type, len(e.Data))

		// 检测终态事件
		lower := strings.ToLower(e.Type)
		if lower == "turn_complete" || lower == "turn_aborted" || lower == "idle" {
			select {
			case turnDone <- struct{}{}:
			default:
			}
		}
	})

	// --- 启动 codex app-server ---
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 使用项目根目录而不是 test 目录
	cwd, _ := os.Getwd()
	projectRoot := filepath.Join(cwd, "..", "..")
	if abs, err := filepath.Abs(projectRoot); err == nil {
		projectRoot = abs
	}
	t.Logf("spawning codex app-server... cwd=%s port=%d", projectRoot, port)

	err = client.SpawnAndConnect(ctx, "", projectRoot, "", "", nil)
	if err != nil {
		t.Fatalf("SpawnAndConnect failed: %v", err)
	}
	defer func() {
		t.Log("shutting down codex...")
		_ = client.Shutdown()
		time.Sleep(500 * time.Millisecond)
		_ = client.Kill()
	}()

	t.Logf("codex started: port=%d, threadID=%s", client.GetPort(), client.GetThreadID())

	// --- 发送一个需要多步骤的 prompt ---
	prompt := "请列出当前目录下的所有 .go 文件，然后统计总行数。分步执行，先 ls，再 wc -l。"
	t.Logf("submitting prompt: %s", prompt)

	submitStart := time.Now()
	if err := client.Submit(prompt, nil, nil, nil); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	t.Logf("submit returned in %dms", time.Since(submitStart).Milliseconds())

	// --- 等待 turn 结束 (最多 90s) ---
	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()

	select {
	case <-turnDone:
		elapsed := time.Since(submitStart)
		t.Logf("turn ended after %dms", elapsed.Milliseconds())
	case <-timer.C:
		t.Logf("timeout: turn did not end within 90s")
	}

	// --- 分析结果 ---
	mu.Lock()
	defer mu.Unlock()

	t.Logf("\n========== EVENT TIMELINE (%d events) ==========", len(events))

	var (
		turnStartedAt  time.Time
		turnEndedAt    time.Time
		turnEndType    string
		hasAborted     bool
		hasComplete    bool
		hasToolCall    bool
		hasCmdBegin    bool
		hasCmdEnd      bool
		hasAgentMsg    bool
		totalDeltaEvts int
	)

	for i, e := range events {
		age := ""
		if !turnStartedAt.IsZero() {
			age = fmt.Sprintf("+%dms", e.At.Sub(turnStartedAt).Milliseconds())
		}
		t.Logf("  [%3d] %-8s %-35s data=%d bytes", i, age, e.Type, len(e.Data))

		switch strings.ToLower(e.Type) {
		case "turn_started":
			turnStartedAt = e.At
		case "turn_complete":
			turnEndedAt = e.At
			turnEndType = "turn_complete"
			hasComplete = true
		case "turn_aborted":
			turnEndedAt = e.At
			turnEndType = "turn_aborted"
			hasAborted = true
		case "exec_command_begin":
			hasCmdBegin = true
		case "exec_command_end":
			hasCmdEnd = true
		case "dynamic_tool_call":
			hasToolCall = true
		case "agent_message":
			hasAgentMsg = true
		}
		if strings.Contains(strings.ToLower(e.Type), "delta") {
			totalDeltaEvts++
		}
	}

	t.Logf("\n========== DIAGNOSIS ==========")
	t.Logf("Total events:     %d", len(events))
	t.Logf("Turn end type:    %s", turnEndType)
	t.Logf("Has turn_aborted: %v", hasAborted)
	t.Logf("Has turn_complete:%v", hasComplete)
	t.Logf("Has cmd begin:    %v", hasCmdBegin)
	t.Logf("Has cmd end:      %v", hasCmdEnd)
	t.Logf("Has tool call:    %v", hasToolCall)
	t.Logf("Has agent msg:    %v", hasAgentMsg)
	t.Logf("Delta events:     %d", totalDeltaEvts)

	if !turnStartedAt.IsZero() && !turnEndedAt.IsZero() {
		duration := turnEndedAt.Sub(turnStartedAt)
		t.Logf("Turn duration:    %dms", duration.Milliseconds())

		// 核心断言: 如果 turn 不到 60s 就被 abort/complete,
		// 且只执行了 0-1 个命令, 说明 codex 过早终止。
		if hasAborted && duration < 60*time.Second {
			t.Errorf("🔴 CONFIRMED: codex aborted turn after only %dms (< 60s). "+
				"This is the premature abort bug.", duration.Milliseconds())
		}

		// 额外检查: turn_complete 但没有命令执行完成
		if hasComplete && !hasCmdEnd && !hasToolCall && duration < 30*time.Second {
			t.Errorf("🟡 SUSPICIOUS: turn completed in %dms without cmd/tool execution. "+
				"codex may not have actually done anything.", duration.Milliseconds())
		}
	} else {
		t.Error("could not determine turn start/end times from events")
	}
}
