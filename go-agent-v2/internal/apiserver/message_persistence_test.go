// message_persistence_test.go — 消息持久化压力测试。
//
// 20 个并发 agent, 各自写入消息 → 验证持久化 → 模拟"重新打开"读取历史。
//
// 需要 PostgreSQL: POSTGRES_CONNECTION_STRING 环境变量。
// 运行: go test -v -count=1 -run TestMessagePersistence -timeout 120s ./internal/apiserver/
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

type persistenceEvent struct {
	eventType string
	payload   map[string]any
	rawData   json.RawMessage
	delay     time.Duration
}

// TestMessagePersistence_20Agents 并发 agent 消息持久化压力测试 (默认 20，可配置)。
//
// 流程:
//  1. 连 PostgreSQL → 执行迁移
//  2. 并发启动 N 个 "agent"，按不同场景写入不同事件流
//  3. 验证: 每个 agent 的消息都正确写入
//  4. 模拟 "重新打开": 通过 thread/messages API 读取历史
//  5. 验证: 读取的消息数 ≥ 预期
//  6. 清理测试数据
//
// 可选环境变量:
//   - E2E_MESSAGE_PERSIST_AGENTS: agent 数量 (默认 20，建议压测可设为 50)
func TestMessagePersistence_20Agents(t *testing.T) {
	cfg := config.Load()
	if cfg.PostgresConnStr == "" {
		t.Skip("POSTGRES_CONNECTION_STRING not set, skipping message persistence test")
	}

	numAgents := 20
	if raw := os.Getenv("E2E_MESSAGE_PERSIST_AGENTS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid E2E_MESSAGE_PERSIST_AGENTS=%q", raw)
		}
		numAgents = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. 连 PostgreSQL + 迁移
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool, "../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	msgStore := store.NewAgentMessageStore(pool)

	// 创建 apiserver (带 DB)
	mgr := runner.NewAgentManager()
	srv := New(Deps{
		Manager: mgr,
		Config:  cfg,
		DB:      pool,
	})

	// 2. 并发写入
	t.Logf("=== Phase 1: %d agents concurrent write (mixed scenarios) ===", numAgents)
	start := time.Now()

	var wg sync.WaitGroup
	writeErrs := make(chan error, numAgents*4)
	agentIDs := make([]string, numAgents)
	scenarioByAgent := make([]string, numAgents)
	expectedByAgent := make([]int, numAgents)

	for i := 0; i < numAgents; i++ {
		i := i
		agentID := fmt.Sprintf("stress-test-agent-%d-%d", i, time.Now().UnixNano())
		agentIDs[i] = agentID
		events, scenario := persistenceScenarioEvents(i)
		scenarioByAgent[i] = scenario
		expectedByAgent[i] = 1 + len(events) // user message + scenario events

		wg.Add(1)
		go func(events []persistenceEvent) {
			defer wg.Done()

			// 写入 user 消息
			srv.PersistUserMessage(agentID, fmt.Sprintf("Hello from agent %d", i))

			for _, e := range events {
				if e.delay > 0 {
					time.Sleep(e.delay)
				}

				dataBytes := e.rawData
				if len(dataBytes) == 0 {
					payload := e.payload
					if payload == nil {
						payload = map[string]any{}
					}
					dataBytes, _ = json.Marshal(payload)
				}

				event := codex.Event{
					Type: e.eventType,
					Data: dataBytes,
				}
				method, err := resolveEventMethodStrict(e.eventType, nil)
				if err != nil {
					writeErrs <- fmt.Errorf("agent=%d scenario=%s event=%s: %w", i, scenarioByAgent[i], e.eventType, err)
					continue
				}
				srv.persistMessage(agentID, event, method)
			}
		}(events)
	}

	wg.Wait()
	close(writeErrs)
	for err := range writeErrs {
		t.Error(err)
	}

	writeTime := time.Since(start)
	t.Logf("Write phase completed: agents=%d in %v", numAgents, writeTime)

	// 3. 验证写入: 每个 agent 都有消息
	t.Log("=== Phase 2: Verify persistence ===")
	for i, agentID := range agentIDs {
		count, err := msgStore.CountByAgent(ctx, agentID)
		if err != nil {
			t.Errorf("agent %d count error: %v", i, err)
			continue
		}
		expected := expectedByAgent[i]
		if count < int64(expected) {
			t.Errorf("agent %d (%s): scenario=%s expected >= %d messages, got %d", i, agentID, scenarioByAgent[i], expected, count)
		} else {
			t.Logf("agent %d: scenario=%s %d messages ✓", i, scenarioByAgent[i], count)
		}
	}

	// 4. 模拟 "重新打开页面": 通过 thread/messages API 读取历史
	t.Log("=== Phase 3: Simulate reopen (thread/messages) ===")
	readStart := time.Now()

	for i, agentID := range agentIDs {
		params, _ := json.Marshal(map[string]any{
			"threadId": agentID,
			"limit":    100,
		})

		result, err := srv.InvokeMethod(ctx, "thread/messages", json.RawMessage(params))
		if err != nil {
			t.Errorf("agent %d thread/messages error: %v", i, err)
			continue
		}

		resp, ok := result.(map[string]any)
		if !ok {
			t.Errorf("agent %d: unexpected response type: %T", i, result)
			continue
		}

		msgs, ok := resp["messages"]
		if !ok {
			t.Errorf("agent %d: no messages in response", i)
			continue
		}

		msgList, ok := msgs.([]store.AgentMessage)
		if !ok {
			t.Errorf("agent %d: unexpected messages type: %T", i, msgs)
			continue
		}

		total := resp["total"].(int64)
		expected := expectedByAgent[i]
		if len(msgList) < expected {
			t.Errorf("agent %d: scenario=%s expected >= %d messages in read, got %d (total: %d)", i, scenarioByAgent[i], expected, len(msgList), total)
		}

		// 验证消息 role 分布
		roleCounts := map[string]int{}
		for _, m := range msgList {
			roleCounts[m.Role]++
		}
		t.Logf("agent %d: read %d msgs (total: %d), roles: user=%d assistant=%d tool=%d system=%d ✓",
			i, len(msgList), total,
			roleCounts["user"], roleCounts["assistant"],
			roleCounts["tool"], roleCounts["system"])
	}

	readTime := time.Since(readStart)
	t.Logf("Read phase completed: %d agents in %v", numAgents, readTime)

	// 5. 验证游标分页
	t.Log("=== Phase 4: Cursor pagination ===")
	firstAgent := agentIDs[0]
	params, _ := json.Marshal(map[string]any{
		"threadId": firstAgent,
		"limit":    3,
	})
	result, err := srv.InvokeMethod(ctx, "thread/messages", json.RawMessage(params))
	if err != nil {
		t.Fatalf("pagination test error: %v", err)
	}
	resp := result.(map[string]any)
	page1 := resp["messages"].([]store.AgentMessage)
	t.Logf("Page 1: %d messages (first id=%d, last id=%d)", len(page1), page1[0].ID, page1[len(page1)-1].ID)

	if len(page1) > 0 {
		// 用最后一条的 id 作为 before 游标
		lastID := page1[len(page1)-1].ID
		params, _ = json.Marshal(map[string]any{
			"threadId": firstAgent,
			"limit":    3,
			"before":   lastID,
		})
		result, err = srv.InvokeMethod(ctx, "thread/messages", json.RawMessage(params))
		if err != nil {
			t.Fatalf("pagination page 2 error: %v", err)
		}
		resp = result.(map[string]any)
		page2 := resp["messages"].([]store.AgentMessage)
		t.Logf("Page 2 (before=%d): %d messages", lastID, len(page2))

		// 确保没有重复
		page1IDs := map[int64]bool{}
		for _, m := range page1 {
			page1IDs[m.ID] = true
		}
		for _, m := range page2 {
			if page1IDs[m.ID] {
				t.Errorf("pagination overlap: id %d in both page1 and page2", m.ID)
			}
		}
	}

	// 6. 清理
	t.Log("=== Phase 5: Cleanup ===")
	for _, agentID := range agentIDs {
		if err := msgStore.DeleteByAgent(ctx, agentID); err != nil {
			t.Errorf("cleanup error: %v", err)
		}
	}
	t.Log("Cleanup complete ✓")

	t.Logf("\n=== SUMMARY ===")
	t.Logf("Agents:     %d", numAgents)
	scenarioCounts := map[string]int{}
	for _, scenario := range scenarioByAgent {
		scenarioCounts[scenario]++
	}
	t.Logf("Scenarios:  %+v", scenarioCounts)
	t.Logf("Write:      %v", writeTime)
	t.Logf("Read:       %v", readTime)
	t.Logf("PASSED ✓")
}

func persistenceScenarioEvents(agentIndex int) ([]persistenceEvent, string) {
	switch agentIndex % 8 {
	case 0:
		return []persistenceEvent{
			{eventType: "agent_message_delta", payload: map[string]any{"delta": fmt.Sprintf("Response part A from agent %d", agentIndex)}},
			{eventType: "agent_message_delta", payload: map[string]any{"delta": fmt.Sprintf("Response part B from agent %d", agentIndex)}},
			{eventType: "exec_command_begin", payload: map[string]any{"command": fmt.Sprintf("ls -la /tmp/agent-%d", agentIndex)}},
			{eventType: "exec_command_output_delta", payload: map[string]any{"output": fmt.Sprintf("drwxr-xr-x agent-%d", agentIndex)}},
			{eventType: "exec_command_end", payload: map[string]any{"output": "exit 0"}},
			{eventType: "patch_apply_begin", payload: map[string]any{"file": fmt.Sprintf("agent_%d.go", agentIndex)}},
			{eventType: "patch_apply", payload: map[string]any{"delta": fmt.Sprintf("--- a/agent_%d.go\n+++ b/agent_%d.go\n", agentIndex, agentIndex)}},
			{eventType: "patch_apply_end", payload: map[string]any{"output": "patch applied"}},
			{eventType: "turn_complete", payload: map[string]any{}},
			{eventType: "session_configured", payload: map[string]any{}},
		}, "assistant_exec_patch"
	case 1:
		return []persistenceEvent{
			{eventType: "turn_started", payload: map[string]any{}},
			{eventType: "reasoning_delta", payload: map[string]any{"text": fmt.Sprintf("thinking-%d", agentIndex)}},
			{eventType: "reasoning_summary", payload: map[string]any{"summary": fmt.Sprintf("summary-%d", agentIndex)}},
			{eventType: "plan_delta", payload: map[string]any{"content": fmt.Sprintf("plan step from agent %d", agentIndex)}},
			{eventType: "plan_update", payload: map[string]any{"content": "plan updated"}},
			{eventType: "agent_message_delta", payload: map[string]any{"delta": "final answer"}},
			{eventType: "agent_message_completed", payload: map[string]any{"message": "done"}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "reasoning_plan"
	case 2:
		return []persistenceEvent{
			{eventType: "exec_approval_request", payload: map[string]any{"command": fmt.Sprintf("rm -rf /tmp/unsafe-%d", agentIndex)}},
			{eventType: "file_change_approval_request", payload: map[string]any{"message": "need approval to patch"}},
			{eventType: "item/commandExecution/requestApproval", payload: map[string]any{"command": "npm install"}},
			{eventType: "item/fileChange/requestApproval", payload: map[string]any{"message": "approve file change"}},
			{eventType: "item/fileChange/outputDelta", payload: map[string]any{"delta": fmt.Sprintf("--- a/main_%d.go\n+++ b/main_%d.go\n", agentIndex, agentIndex)}},
			{eventType: "item/completed", payload: map[string]any{"type": "fileChange"}},
			{eventType: "warning", payload: map[string]any{"message": "non-fatal warning"}},
			{eventType: "error", payload: map[string]any{"message": "simulated transient error"}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "approval_and_errors"
	case 3:
		return []persistenceEvent{
			{eventType: "codex/event/task_started", payload: map[string]any{}},
			{eventType: "codex/event/agent_message_content_delta", payload: map[string]any{"delta": "streaming answer"}},
			{eventType: "codex/event/reasoning_content_delta", payload: map[string]any{"delta": "reasoning stream"}},
			{eventType: "codex/event/exec_command_begin", payload: map[string]any{"command": "echo hello"}},
			{eventType: "codex/event/exec_command_output_delta", payload: map[string]any{"delta": "hello"}},
			{eventType: "codex/event/exec_command_end", payload: map[string]any{"output": "exit 0"}},
			{eventType: "codex/event/item_completed", payload: map[string]any{"msg": map[string]any{"message": "done"}}},
			{eventType: "codex/event/token_count", payload: map[string]any{"input": 32, "output": 18}},
			{eventType: "codex/event/task_complete", payload: map[string]any{}},
		}, "codex_event_passthrough"
	case 4:
		return []persistenceEvent{
			{eventType: "thread/started", payload: map[string]any{}},
			{eventType: "turn/started", payload: map[string]any{}},
			{eventType: "item/started", payload: map[string]any{"type": "fileChange", "file": fmt.Sprintf("v2_%d.go", agentIndex)}},
			{eventType: "item/fileChange/outputDelta", payload: map[string]any{"delta": fmt.Sprintf("--- a/v2_%d.go\n+++ b/v2_%d.go\n", agentIndex, agentIndex)}},
			{eventType: "item/completed", payload: map[string]any{"type": "fileChange"}},
			{eventType: "item/agentMessage/delta", payload: map[string]any{"delta": "v2 assistant message"}},
			{eventType: "turn/diff/updated", payload: map[string]any{"diff": fmt.Sprintf("diff-%d", agentIndex)}},
			{eventType: "thread/tokenUsage/updated", payload: map[string]any{"input": 10, "output": 20}},
			{eventType: "turn/completed", payload: map[string]any{}},
		}, "v2_item_thread_passthrough"
	case 5:
		largeOutput := strings.Repeat(fmt.Sprintf("line-%02d ", agentIndex), 450)
		return []persistenceEvent{
			{eventType: "turn_started", payload: map[string]any{}},
			{eventType: "exec_command_begin", payload: map[string]any{"command": fmt.Sprintf("tail -n 2000 /var/log/agent-%d.log", agentIndex)}},
			{eventType: "exec_command_output_delta", payload: map[string]any{"output": largeOutput + "[chunk-1]"}, delay: 2 * time.Millisecond},
			{eventType: "exec_command_output_delta", payload: map[string]any{"output": largeOutput + "[chunk-2]"}, delay: 4 * time.Millisecond},
			{eventType: "exec_command_output_delta", payload: map[string]any{"output": largeOutput + "[chunk-3]"}, delay: 6 * time.Millisecond},
			{eventType: "exec_command_end", payload: map[string]any{"output": "exit 0"}, delay: 2 * time.Millisecond},
			{eventType: "agent_message_delta", payload: map[string]any{"delta": fmt.Sprintf("long-output summarized by agent %d", agentIndex)}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "slow_long_output"
	case 6:
		return []persistenceEvent{
			{eventType: "turn_started", payload: map[string]any{}},
			{eventType: "warning", payload: map[string]any{"message": "preflight mismatch; retry suggested"}},
			{eventType: "error", payload: map[string]any{"message": "simulated db timeout"}},
			{eventType: "error", payload: map[string]any{"message": "simulated upstream 502"}},
			{eventType: "agent_message_delta", payload: map[string]any{"delta": "retrying with fallback route"}},
			{eventType: "exec_command_begin", payload: map[string]any{"command": "echo fallback run"}},
			{eventType: "exec_command_output_delta", payload: map[string]any{"output": "fallback run ok"}},
			{eventType: "exec_command_end", payload: map[string]any{"output": "exit 0"}},
			{eventType: "agent_message_completed", payload: map[string]any{"message": "recovered"}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "fault_injection_recovery"
	default:
		return []persistenceEvent{
			{eventType: "dag/run_start", payload: map[string]any{"dag_key": fmt.Sprintf("e2e-dag-%d", agentIndex), "run_id": fmt.Sprintf("run-%d", agentIndex)}},
			{eventType: "dag/node_start", payload: map[string]any{"node_key": "design"}},
			{eventType: "command_card/exec", payload: map[string]any{"card_key": "lint-and-test", "status": "running"}},
			{eventType: "command_card/result", payload: map[string]any{"card_key": "lint-and-test", "status": "ok"}},
			{eventType: "skill/loaded", payload: map[string]any{"name": "review-pro"}},
			{eventType: "skill/exec", payload: map[string]any{"name": "review-pro", "target": "agent/main.go"}},
			{eventType: "skill/result", payload: map[string]any{"name": "review-pro", "status": "ok"}},
			{eventType: "dag/node_complete", payload: map[string]any{"node_key": "design", "status": "completed"}},
			{eventType: "dag/run_complete", payload: map[string]any{"dag_key": fmt.Sprintf("e2e-dag-%d", agentIndex), "status": "completed"}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "dag_command_skill_mix"
	}
}

// TestMessagePersistence_ChaosReadWrite_RealWorld 更贴近真实生产流量:
// - 50+ agents 并发写
// - 并发读取 thread/messages (含分页游标)
// - 乱序/重复/慢事件/长输出/无效 metadata
// - DAG / command_card / skill 复合事件
func TestMessagePersistence_ChaosReadWrite_RealWorld(t *testing.T) {
	cfg := config.Load()
	if cfg.PostgresConnStr == "" {
		t.Skip("POSTGRES_CONNECTION_STRING not set, skipping chaos persistence test")
	}

	numAgents := envIntWithDefault(t, "E2E_MESSAGE_PERSIST_CHAOS_AGENTS", 50)
	rounds := envIntWithDefault(t, "E2E_MESSAGE_PERSIST_CHAOS_ROUNDS", 4)
	readerWorkers := envIntWithDefault(t, "E2E_MESSAGE_PERSIST_CHAOS_READERS", 12)
	injectUnknown := envBoolWithDefault("E2E_MESSAGE_PERSIST_CHAOS_INJECT_UNKNOWN", false)
	seed := int64(20260218)
	if raw := strings.TrimSpace(os.Getenv("E2E_MESSAGE_PERSIST_CHAOS_SEED")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			t.Fatalf("invalid E2E_MESSAGE_PERSIST_CHAOS_SEED=%q", raw)
		}
		seed = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool, "../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	msgStore := store.NewAgentMessageStore(pool)
	mgr := runner.NewAgentManager()
	srv := New(Deps{
		Manager: mgr,
		Config:  cfg,
		DB:      pool,
	})

	agentIDs := make([]string, numAgents)
	expectedByAgent := make([]int, numAgents)
	for i := 0; i < numAgents; i++ {
		agentIDs[i] = fmt.Sprintf("chaos-agent-%d-%d", i, time.Now().UnixNano())
	}

	t.Logf("=== Chaos E2E start: agents=%d rounds=%d readers=%d seed=%d ===", numAgents, rounds, readerWorkers, seed)
	if injectUnknown {
		t.Log("chaos unknown event injection is enabled")
	}

	var scenarioMu sync.Mutex
	scenarioCounts := map[string]int{}
	doneReaders := make(chan struct{})
	readErrs := make(chan error, readerWorkers*32)
	writeErrs := make(chan error, numAgents*rounds*8)
	var readOps atomic.Int64

	var readersWG sync.WaitGroup
	for r := 0; r < readerWorkers; r++ {
		readerIdx := r
		readersWG.Add(1)
		go func() {
			defer readersWG.Done()
			rng := rand.New(rand.NewSource(seed + int64(10_000+readerIdx)))

			for {
				select {
				case <-doneReaders:
					return
				default:
				}

				agentID := agentIDs[rng.Intn(len(agentIDs))]
				limit := 1 + rng.Intn(25)

				page1, total, err := invokeThreadMessages(ctx, srv, agentID, limit, 0)
				if err != nil {
					select {
					case readErrs <- fmt.Errorf("reader-%d page1: %w", readerIdx, err):
					default:
					}
					continue
				}
				if int64(len(page1)) > total {
					select {
					case readErrs <- fmt.Errorf("reader-%d invalid total: len=%d total=%d", readerIdx, len(page1), total):
					default:
					}
				}
				for i := 1; i < len(page1); i++ {
					if page1[i-1].ID <= page1[i].ID {
						select {
						case readErrs <- fmt.Errorf("reader-%d non-desc order on page1: %d <= %d", readerIdx, page1[i-1].ID, page1[i].ID):
						default:
						}
						break
					}
				}

				if len(page1) > 0 && rng.Intn(100) < 45 {
					before := page1[len(page1)-1].ID
					page2, _, err := invokeThreadMessages(ctx, srv, agentID, limit, before)
					if err != nil {
						select {
						case readErrs <- fmt.Errorf("reader-%d page2: %w", readerIdx, err):
						default:
						}
						continue
					}
					page1IDs := map[int64]struct{}{}
					for _, m := range page1 {
						page1IDs[m.ID] = struct{}{}
					}
					for _, m := range page2 {
						if m.ID >= before {
							select {
							case readErrs <- fmt.Errorf("reader-%d before violation: id=%d before=%d", readerIdx, m.ID, before):
							default:
							}
							break
						}
						if _, dup := page1IDs[m.ID]; dup {
							select {
							case readErrs <- fmt.Errorf("reader-%d pagination overlap id=%d", readerIdx, m.ID):
							default:
							}
							break
						}
					}
				}

				readOps.Add(1)
				if rng.Intn(100) < 20 {
					time.Sleep(time.Duration(1+rng.Intn(4)) * time.Millisecond)
				}
			}
		}()
	}

	writeStart := time.Now()
	var writersWG sync.WaitGroup
	for i := 0; i < numAgents; i++ {
		i := i
		writersWG.Add(1)
		go func() {
			defer writersWG.Done()
			agentID := agentIDs[i]
			rng := rand.New(rand.NewSource(seed + int64(7_919*i)))
			expected := 0

			srv.PersistUserMessage(agentID, fmt.Sprintf("Chaos hello from agent %d", i))
			expected++

			for round := 0; round < rounds; round++ {
				events, scenario := chaosScenarioEvents(i, round, rng)

				scenarioMu.Lock()
				scenarioCounts[scenario]++
				scenarioMu.Unlock()

				if rng.Intn(100) < 35 {
					shufflePersistenceEvents(events, rng)
				}

				for _, e := range events {
					if e.delay > 0 {
						time.Sleep(e.delay)
					}
					dataBytes := e.rawData
					if len(dataBytes) == 0 {
						payload := e.payload
						if payload == nil {
							payload = map[string]any{}
						}
						dataBytes, _ = json.Marshal(payload)
					}

					event := codex.Event{
						Type: e.eventType,
						Data: dataBytes,
					}
					method, err := resolveEventMethodStrict(e.eventType, nil)
					if err != nil {
						select {
						case writeErrs <- fmt.Errorf("writer agent=%d round=%d scenario=%s event=%s: %w", i, round, scenario, e.eventType, err):
						default:
						}
						continue
					}
					srv.persistMessage(agentID, event, method)
					expected++

					if rng.Intn(100) < 20 {
						srv.persistMessage(agentID, event, method)
						expected++
					}
				}

				if injectUnknown && rng.Intn(100) < 45 {
					unknownType := fmt.Sprintf("chaos_unknown_%d_%d", i, round)
					method, err := resolveEventMethodStrict(unknownType, []string{"chaos_unknown_"})
					if err != nil {
						select {
						case writeErrs <- fmt.Errorf("writer unknown agent=%d round=%d event=%s: %w", i, round, unknownType, err):
						default:
						}
						continue
					}
					srv.persistMessage(agentID, codex.Event{
						Type: unknownType,
						Data: json.RawMessage(`{"note":"fallback mapping path"}`),
					}, method)
					expected++
				}
			}

			expectedByAgent[i] = expected
		}()
	}

	writersWG.Wait()
	writeTime := time.Since(writeStart)
	close(doneReaders)
	readersWG.Wait()
	close(readErrs)
	close(writeErrs)

	var readValidationErrs int
	for err := range readErrs {
		readValidationErrs++
		t.Errorf("read validation: %v", err)
	}
	var writeValidationErrs int
	for err := range writeErrs {
		writeValidationErrs++
		t.Errorf("write validation: %v", err)
	}

	t.Logf("Chaos write done: %v, reader ops=%d, read validation errors=%d, write validation errors=%d",
		writeTime, readOps.Load(), readValidationErrs, writeValidationErrs)

	t.Log("=== Chaos verify persisted counts ===")
	for i, agentID := range agentIDs {
		count, err := msgStore.CountByAgent(ctx, agentID)
		if err != nil {
			t.Errorf("agent %d count error: %v", i, err)
			continue
		}
		if count < int64(expectedByAgent[i]) {
			t.Errorf("agent %d expected >= %d messages, got %d", i, expectedByAgent[i], count)
		}
	}

	t.Log("=== Chaos verify deep pagination consistency ===")
	sample := 12
	if numAgents < sample {
		sample = numAgents
	}
	for i := 0; i < sample; i++ {
		agentID := agentIDs[(i*7)%numAgents]
		before := int64(0)
		collected := 0
		expectedTotal := int64(-1)
		seenIDs := map[int64]struct{}{}

		for page := 0; page < 2_000; page++ {
			msgs, total, err := invokeThreadMessages(ctx, srv, agentID, 17, before)
			if err != nil {
				t.Errorf("agent %s pagination read error: %v", agentID, err)
				break
			}
			if expectedTotal < 0 {
				expectedTotal = total
			}
			if len(msgs) == 0 {
				break
			}
			for _, m := range msgs {
				if before > 0 && m.ID >= before {
					t.Errorf("agent %s pagination before violated: id=%d before=%d", agentID, m.ID, before)
					break
				}
				if _, exists := seenIDs[m.ID]; exists {
					t.Errorf("agent %s pagination duplicate id=%d", agentID, m.ID)
					break
				}
				seenIDs[m.ID] = struct{}{}
			}
			collected += len(msgs)
			before = msgs[len(msgs)-1].ID
		}

		if expectedTotal >= 0 && int64(collected) != expectedTotal {
			t.Errorf("agent %s pagination total mismatch: collected=%d total=%d", agentID, collected, expectedTotal)
		}
	}

	t.Log("=== Chaos cleanup ===")
	for _, agentID := range agentIDs {
		if err := msgStore.DeleteByAgent(ctx, agentID); err != nil {
			t.Errorf("cleanup error for %s: %v", agentID, err)
		}
	}

	t.Logf("Chaos scenarios summary: %+v", scenarioCounts)
	t.Logf("Chaos E2E PASSED ✓")
}

func invokeThreadMessages(ctx context.Context, srv *Server, agentID string, limit int, before int64) ([]store.AgentMessage, int64, error) {
	params := map[string]any{
		"threadId": agentID,
		"limit":    limit,
	}
	if before > 0 {
		params["before"] = before
	}
	raw, _ := json.Marshal(params)
	result, err := srv.InvokeMethod(ctx, "thread/messages", json.RawMessage(raw))
	if err != nil {
		return nil, 0, err
	}

	resp, ok := result.(map[string]any)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected response type: %T", result)
	}

	msgsAny, ok := resp["messages"]
	if !ok {
		return nil, 0, fmt.Errorf("messages missing in response")
	}
	msgs, ok := msgsAny.([]store.AgentMessage)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected messages type: %T", msgsAny)
	}

	total, ok := asInt64(resp["total"])
	if !ok {
		return nil, 0, fmt.Errorf("unexpected total type: %T", resp["total"])
	}
	return msgs, total, nil
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

func envIntWithDefault(t *testing.T, key string, defaultValue int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		t.Fatalf("invalid %s=%q", key, raw)
	}
	return v
}

func envBoolWithDefault(key string, defaultValue bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return defaultValue
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func resolveEventMethodStrict(eventType string, fallbackAllowPrefixes []string) (string, error) {
	if method, ok := eventMethodMap[eventType]; ok {
		return method, nil
	}
	for _, prefix := range passthroughEventPrefixes {
		if strings.HasPrefix(eventType, prefix) {
			return eventType, nil
		}
	}
	if strings.Contains(eventType, "/") {
		return eventType, nil
	}
	for _, prefix := range fallbackAllowPrefixes {
		if strings.HasPrefix(eventType, prefix) {
			return "agent/event/" + eventType, nil
		}
	}
	return "", fmt.Errorf("unmapped event type (strict mode): %s", eventType)
}

func shufflePersistenceEvents(events []persistenceEvent, rng *rand.Rand) {
	for i := len(events) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		events[i], events[j] = events[j], events[i]
	}
}

func chaosScenarioEvents(agentIndex, round int, rng *rand.Rand) ([]persistenceEvent, string) {
	switch (agentIndex + round) % 6 {
	case 0:
		return []persistenceEvent{
			{eventType: "turn_started", payload: map[string]any{}},
			{eventType: "agent_message_delta", payload: map[string]any{"delta": fmt.Sprintf("analysis-%d-%d", agentIndex, round)}},
			{eventType: "exec_command_begin", payload: map[string]any{"command": fmt.Sprintf("grep -R \"TODO\" ./agent-%d", agentIndex)}},
			{eventType: "exec_command_output_delta", payload: map[string]any{"output": fmt.Sprintf("found TODO in file-%d-%d.go", agentIndex, round)}, delay: 1 * time.Millisecond},
			{eventType: "patch_apply_begin", payload: map[string]any{"file": fmt.Sprintf("agent_%d_round_%d.go", agentIndex, round)}},
			{eventType: "patch_apply", payload: map[string]any{"delta": fmt.Sprintf("--- a/a_%d.go\n+++ b/a_%d.go\n+fix round %d\n", agentIndex, agentIndex, round)}},
			{eventType: "patch_apply_end", payload: map[string]any{"output": "patch ok"}},
			{eventType: "agent_message_completed", payload: map[string]any{"message": "done"}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "interactive_edit_stream"
	case 1:
		return []persistenceEvent{
			{eventType: "exec_approval_request", payload: map[string]any{"command": fmt.Sprintf("kubectl rollout restart service-%d", agentIndex)}},
			{eventType: "item/commandExecution/requestApproval", payload: map[string]any{"command": "kubectl apply -f prod.yaml"}},
			{eventType: "warning", payload: map[string]any{"message": "policy check requires approval"}},
			{eventType: "error", payload: map[string]any{"message": "approval denied once; retrying"}},
			{eventType: "agent_message_delta", payload: map[string]any{"delta": "switching to safe fallback"}},
			{eventType: "exec_command_begin", payload: map[string]any{"command": "kubectl apply -f staging.yaml"}},
			{eventType: "exec_command_output_delta", payload: map[string]any{"output": "deployment.apps updated"}},
			{eventType: "exec_command_end", payload: map[string]any{"output": "exit 0"}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "approval_retry_path"
	case 2:
		return []persistenceEvent{
			{eventType: "thread/started", payload: map[string]any{}},
			{eventType: "turn/started", payload: map[string]any{}},
			{eventType: "item/started", payload: map[string]any{"type": "commandExecution", "id": fmt.Sprintf("ce-%d-%d", agentIndex, round)}},
			{eventType: "item/commandExecution/outputDelta", payload: map[string]any{"delta": "building..."}},
			{eventType: "item/completed", payload: map[string]any{"type": "commandExecution", "status": "ok"}},
			{eventType: "item/agentMessage/delta", payload: map[string]any{"delta": "v2 bridge update"}},
			{eventType: "turn/diff/updated", payload: map[string]any{"diff": fmt.Sprintf("bridge-diff-%d-%d", agentIndex, round)}},
			{eventType: "thread/tokenUsage/updated", payload: map[string]any{"input": 64 + round, "output": 32 + agentIndex%7}},
			{eventType: "turn/completed", payload: map[string]any{}},
		}, "v2_thread_item_mix"
	case 3:
		badJSON := json.RawMessage(fmt.Sprintf(`{"broken":"agent-%d-round-%d"`, agentIndex, round))
		return []persistenceEvent{
			{eventType: "codex/event/task_started", payload: map[string]any{}},
			{eventType: "codex/event/agent_message_content_delta", payload: map[string]any{"delta": "streaming partial answer"}},
			{eventType: "codex/event/item_completed", rawData: badJSON},
			{eventType: "codex/event/token_count", payload: map[string]any{"input": 120 + round, "output": 80 + agentIndex%11}},
			{eventType: "codex/event/task_complete", payload: map[string]any{}},
		}, "metadata_sanitize_path"
	case 4:
		return []persistenceEvent{
			{eventType: "dag/run_start", payload: map[string]any{"dag_key": fmt.Sprintf("chaos-dag-%d", agentIndex), "run_id": fmt.Sprintf("run-%d-%d", agentIndex, round)}},
			{eventType: "dag/node_start", payload: map[string]any{"node_key": "plan"}},
			{eventType: "command_card/exec", payload: map[string]any{"card_key": "lint-and-test", "status": "running"}},
			{eventType: "command_card/result", payload: map[string]any{"card_key": "lint-and-test", "status": "ok"}},
			{eventType: "skill/loaded", payload: map[string]any{"name": "review-pro"}},
			{eventType: "skill/exec", payload: map[string]any{"name": "review-pro", "target": fmt.Sprintf("svc/%d", agentIndex)}},
			{eventType: "skill/result", payload: map[string]any{"name": "review-pro", "status": "ok"}},
			{eventType: "dag/node_complete", payload: map[string]any{"node_key": "plan", "status": "completed"}},
			{eventType: "dag/run_complete", payload: map[string]any{"dag_key": fmt.Sprintf("chaos-dag-%d", agentIndex), "status": "completed"}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "dag_skill_command_chain"
	default:
		diffLine := fmt.Sprintf("+updated line for agent=%d round=%d rnd=%d\n", agentIndex, round, rng.Intn(9_999))
		longDiff := strings.Repeat(diffLine, 900)
		return []persistenceEvent{
			{eventType: "turn_started", payload: map[string]any{}},
			{eventType: "patch_apply_begin", payload: map[string]any{"file": fmt.Sprintf("large_%d_%d.go", agentIndex, round)}},
			{eventType: "patch_apply", payload: map[string]any{"delta": longDiff + "[chunk-1]"}, delay: 2 * time.Millisecond},
			{eventType: "patch_apply", payload: map[string]any{"delta": longDiff + "[chunk-2]"}, delay: 4 * time.Millisecond},
			{eventType: "patch_apply_end", payload: map[string]any{"output": "large patch applied"}},
			{eventType: "agent_message_delta", payload: map[string]any{"delta": "large diff summarized"}},
			{eventType: "turn_complete", payload: map[string]any{}},
		}, "long_diff_slow_stream"
	}
}
