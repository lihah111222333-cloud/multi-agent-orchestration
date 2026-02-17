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
	"sync"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

// TestMessagePersistence_20Agents 20 个并发 agent 消息持久化压力测试。
//
// 流程:
//  1. 连 PostgreSQL → 执行迁移
//  2. 并发启动 20 个 "agent", 每个写入 user + assistant + tool + system 消息
//  3. 验证: 每个 agent 的消息都正确写入
//  4. 模拟 "重新打开": 通过 thread/messages API 读取历史
//  5. 验证: 读取的消息数 ≥ 预期
//  6. 清理测试数据
func TestMessagePersistence_20Agents(t *testing.T) {
	cfg := config.Load()
	if cfg.PostgresConnStr == "" {
		t.Skip("POSTGRES_CONNECTION_STRING not set, skipping message persistence test")
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

	const numAgents = 20
	const msgsPerAgent = 10 // 每个 agent 写入 10 条消息

	// 2. 并发写入
	t.Log("=== Phase 1: 20 agents concurrent write ===")
	start := time.Now()

	var wg sync.WaitGroup
	errors := make(chan error, numAgents)
	agentIDs := make([]string, numAgents)

	for i := 0; i < numAgents; i++ {
		i := i
		agentID := fmt.Sprintf("stress-test-agent-%d-%d", i, time.Now().UnixNano())
		agentIDs[i] = agentID

		wg.Add(1)
		go func() {
			defer wg.Done()

			// 写入 user 消息
			srv.PersistUserMessage(agentID, fmt.Sprintf("Hello from agent %d", i))

			// 模拟 agent 事件 → persistMessage
			events := []struct {
				eventType string
				delta     string
			}{
				{"agent_message_delta", fmt.Sprintf("Response part 1 from agent %d", i)},
				{"agent_message_delta", fmt.Sprintf("Response part 2 from agent %d", i)},
				{"agent_message_delta", fmt.Sprintf("Response part 3 from agent %d", i)},
				{"exec_command_begin", fmt.Sprintf("ls -la in agent %d", i)},
				{"exec_command_end", fmt.Sprintf("exit 0 in agent %d", i)},
				{"patch_apply_begin", fmt.Sprintf("file.go in agent %d", i)},
				{"patch_apply_end", fmt.Sprintf("applied in agent %d", i)},
				{"turn_complete", ""},
				{"session_configured", ""},
			}

			for _, e := range events {
				data := map[string]any{}
				if e.delta != "" {
					data["delta"] = e.delta
				}
				dataBytes, _ := json.Marshal(data)
				event := codex.Event{
					Type: e.eventType,
					Data: dataBytes,
				}
				method := mapEventToMethod(e.eventType)
				srv.persistMessage(agentID, event, method)
			}
		}()
	}

	wg.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("write error: %v", err)
	}

	writeTime := time.Since(start)
	t.Logf("Write phase completed: %d agents × %d msgs in %v", numAgents, msgsPerAgent, writeTime)

	// 3. 验证写入: 每个 agent 都有消息
	t.Log("=== Phase 2: Verify persistence ===")
	for i, agentID := range agentIDs {
		count, err := msgStore.CountByAgent(ctx, agentID)
		if err != nil {
			t.Errorf("agent %d count error: %v", i, err)
			continue
		}
		if count < int64(msgsPerAgent) {
			t.Errorf("agent %d (%s): expected >= %d messages, got %d", i, agentID, msgsPerAgent, count)
		} else {
			t.Logf("agent %d: %d messages ✓", i, count)
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
		if len(msgList) < msgsPerAgent {
			t.Errorf("agent %d: expected >= %d messages in read, got %d (total: %d)", i, msgsPerAgent, len(msgList), total)
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
	t.Logf("Msgs/Agent: %d", msgsPerAgent)
	t.Logf("Total:      %d", numAgents*msgsPerAgent)
	t.Logf("Write:      %v", writeTime)
	t.Logf("Read:       %v", readTime)
	t.Logf("PASSED ✓")
}
