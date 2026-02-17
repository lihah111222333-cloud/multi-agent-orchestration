// rpc_e2e_test.go — 端对端测试 JSON-RPC thread/start + turn/start。
//
// 运行: go test -v -run TestRPCE2E -timeout 60s ./cmd/rpc-test/
// 需要先启动 app-server: ./app-server --listen ws://127.0.0.1:4500
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"` // notification
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"` // notification
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// wsClient 简单的 JSON-RPC WebSocket 客户端。
type wsClient struct {
	conn          *websocket.Conn
	mu            sync.Mutex
	nextID        int
	responses     chan rpcResponse
	notifications chan rpcResponse
}

func dialWS(t *testing.T, addr string) *wsClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	c := &wsClient{
		conn:          conn,
		responses:     make(chan rpcResponse, 100),
		notifications: make(chan rpcResponse, 100),
	}
	go c.readLoop(t)
	return c
}

func (c *wsClient) readLoop(t *testing.T) {
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var resp rpcResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			t.Logf("bad json: %s", msg)
			continue
		}
		if resp.ID > 0 {
			c.responses <- resp
		} else {
			// notification (no ID)
			t.Logf("<<< NOTIFICATION: method=%s params=%s", resp.Method, resp.Params)
			c.notifications <- resp
		}
	}
}

func (c *wsClient) call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, _ := json.Marshal(req)
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return nil, err
	}

	// 等待对应 ID 的响应 (最多 25 秒，因为 codex spawn 可能要 15 秒)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	for {
		select {
		case resp := <-c.responses:
			if resp.ID == id {
				if resp.Error != nil {
					return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
				}
				return resp.Result, nil
			}
			// 不是我们的 ID，放回去
			c.responses <- resp
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for response to %s (id=%d)", method, id)
		}
	}
}

func (c *wsClient) close() {
	c.conn.Close()
}

// waitNotification 等待指定方法的通知。
func (c *wsClient) waitNotification(t *testing.T, method string, timeout time.Duration) *rpcResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		select {
		case notif := <-c.notifications:
			if notif.Method == method {
				return &notif
			}
			// 不匹配的通知放回
			c.notifications <- notif
		case <-ctx.Done():
			return nil
		}
	}
}

// TestRPCE2E_ThreadStart 测试 thread/start 能正常启动 codex 进程。
func TestRPCE2E_ThreadStart(t *testing.T) {
	c := dialWS(t, "ws://127.0.0.1:4500")
	defer c.close()

	t.Log("=== thread/start ===")
	result, err := c.call("thread/start", map[string]any{"cwd": "."})
	if err != nil {
		t.Fatalf("thread/start failed: %v", err)
	}

	var threadResp struct {
		Thread struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &threadResp); err != nil {
		t.Fatalf("parse thread/start result: %v", err)
	}
	t.Logf("thread/start OK: id=%s status=%s", threadResp.Thread.ID, threadResp.Thread.Status)

	if threadResp.Thread.ID == "" {
		t.Fatal("thread/start returned empty thread ID")
	}

	threadID := threadResp.Thread.ID

	// 等待 session_configured 通知 (codex 启动后应发送)
	t.Log("waiting for thread/started notification...")
	notif := c.waitNotification(t, "thread/started", 10*time.Second)
	if notif != nil {
		t.Logf("got thread/started: %s", notif.Params)
	} else {
		t.Log("no thread/started notification within 10s (may be OK if codex is slow)")
	}

	// === turn/start: 发送一条消息 ===
	t.Log("=== turn/start ===")
	turnResult, err := c.call("turn/start", map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": "say hello"}},
	})
	if err != nil {
		t.Fatalf("turn/start failed: %v", err)
	}
	t.Logf("turn/start OK: %s", turnResult)

	// 等待 agent 回复通知
	t.Log("waiting for agent message notifications (15s)...")
	deadline := time.Now().Add(15 * time.Second)
	gotReply := false
	for time.Now().Before(deadline) {
		select {
		case notif := <-c.notifications:
			t.Logf("  notification: method=%s", notif.Method)
			if notif.Method == "item/agentMessage/delta" || notif.Method == "item/started" {
				gotReply = true
				t.Logf("  >>> GOT AGENT REPLY: %s", notif.Params)
			}
		case <-time.After(1 * time.Second):
		}
		if gotReply {
			break
		}
	}

	if gotReply {
		t.Log("SUCCESS: received agent reply via JSON-RPC notification")
	} else {
		t.Error("FAIL: no agent reply notifications received within 15s")
	}

	// 收集剩余通知
	time.Sleep(2 * time.Second)
	remaining := 0
	for {
		select {
		case n := <-c.notifications:
			remaining++
			t.Logf("  remaining notification: method=%s", n.Method)
		default:
			goto done
		}
	}
done:
	t.Logf("collected %d remaining notifications", remaining)
}

// TestRPCE2E_ThreadList 测试 thread/list。
func TestRPCE2E_ThreadList(t *testing.T) {
	c := dialWS(t, "ws://127.0.0.1:4500")
	defer c.close()

	result, err := c.call("thread/list", map[string]any{})
	if err != nil {
		t.Fatalf("thread/list failed: %v", err)
	}
	t.Logf("thread/list: %s", result)
}

// TestRPCE2E_LSPCodeReview E2E 测试: agent 使用 LSP 动态工具审查代码。
//
// 验证:
//  1. thread/start 注入了 dynamicTools (lsp_hover, lsp_open_file, lsp_diagnostics)
//  2. agent 在审查代码时实际调用了 LSP 工具
//  3. handleDynamicToolCall 正确处理了调用并回传结果
//
// 成功标志: 收到至少 1 个 lsp/tool/called 通知。
func TestRPCE2E_LSPCodeReview(t *testing.T) {
	c := dialWS(t, "ws://127.0.0.1:4500")
	defer c.close()

	// ── Step 1: thread/start ──
	t.Log("=== thread/start (LSP code review) ===")
	cwd := "/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2"
	result, err := c.call("thread/start", map[string]any{
		"cwd": cwd,
	})
	if err != nil {
		t.Fatalf("thread/start failed: %v", err)
	}

	var threadResp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &threadResp); err != nil {
		t.Fatalf("parse thread/start result: %v", err)
	}
	threadID := threadResp.Thread.ID
	t.Logf("thread/start OK: id=%s", threadID)

	if threadID == "" {
		t.Fatal("thread/start returned empty thread ID")
	}

	// 等待 thread 就绪
	time.Sleep(3 * time.Second)

	// ── Step 2: turn/start — 要求 agent 用 LSP 工具审查代码 ──
	t.Log("=== turn/start (LSP code review prompt) ===")
	prompt := `Review the Go source file at internal/codex/client_appserver.go in this project.

You MUST use the following tools in this order:
1. Call lsp_open_file with file_path="` + cwd + `/internal/codex/client_appserver.go"
2. Call lsp_diagnostics with file_path="` + cwd + `/internal/codex/client_appserver.go"
3. Call lsp_hover on the Initialize function (approximately line 225, column 30)

After using these LSP tools, provide a brief code review summary based on the results.
Do NOT skip any of the tool calls above. This is a test to verify dynamic tool injection works.`

	turnResult, err := c.call("turn/start", map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": prompt}},
	})
	if err != nil {
		t.Fatalf("turn/start failed: %v", err)
	}
	t.Logf("turn/start OK: %s", turnResult)

	// ── Step 3: 收集通知 — 等待 LSP 工具调用 ──
	t.Log("waiting for LSP tool calls and agent reply (60s max)...")

	var (
		lspToolCalls []string // 收到的 lsp/tool/called 通知
		agentDeltas  int      // 收到的 agent message delta 数
		allNotifs    int      // 所有通知总数
	)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case notif := <-c.notifications:
			allNotifs++

			// 使用 strings.HasSuffix 匹配: 通知方法可能有 agent/event/ 前缀
			switch {
			case strings.HasSuffix(notif.Method, "lsp/tool/called"):
				// 解析工具名
				var payload struct {
					Tool string `json:"tool"`
				}
				if json.Unmarshal(notif.Params, &payload) == nil {
					lspToolCalls = append(lspToolCalls, payload.Tool)
					t.Logf("  ✅ LSP TOOL CALLED: %s (params: %s)", payload.Tool, notif.Params)
				}
			case strings.HasSuffix(notif.Method, "item/agentMessage/delta"):
				agentDeltas++
				if agentDeltas <= 3 { // 只打前几条 delta
					t.Logf("  📝 agent delta: %s", truncate(string(notif.Params), 200))
				}
			case strings.HasSuffix(notif.Method, "turn/completed"):
				t.Log("  ⏹️  turn/completed — agent finished full turn")
				goto collect_done
			case strings.HasSuffix(notif.Method, "item/completed"):
				t.Logf("  📦 item/completed (continuing, waiting for turn/completed)")
			case strings.Contains(notif.Method, "dynamic_tool_call"):
				t.Logf("  🔧 dynamic tool event: method=%s params=%s",
					notif.Method, truncate(string(notif.Params), 200))
			default:
				t.Logf("  notification: method=%s", notif.Method)
			}

		case <-time.After(2 * time.Second):
			// 如果已经收到了 LSP 调用 + agent 回复，可以提早退出
			if len(lspToolCalls) > 0 && agentDeltas > 0 {
				t.Log("  → got LSP calls + agent reply, waiting 5s more for completion...")
				time.Sleep(5 * time.Second)
				goto collect_done
			}
		}
	}

collect_done:
	// 排空剩余通知
	for {
		select {
		case n := <-c.notifications:
			allNotifs++
			if strings.HasSuffix(n.Method, "lsp/tool/called") {
				var payload struct {
					Tool string `json:"tool"`
				}
				if json.Unmarshal(n.Params, &payload) == nil {
					lspToolCalls = append(lspToolCalls, payload.Tool)
				}
			}
		default:
			goto report
		}
	}

report:
	// ── Step 4: 报告结果 ──
	t.Logf("\n========== LSP E2E RESULTS ==========")
	t.Logf("Total notifications received: %d", allNotifs)
	t.Logf("LSP tool calls: %d %v", len(lspToolCalls), lspToolCalls)
	t.Logf("Agent message deltas: %d", agentDeltas)

	// 关键断言: 至少收到 1 个 LSP 工具调用
	if len(lspToolCalls) == 0 {
		t.Error("FAIL: no lsp/tool/called notifications received — dynamic tool injection may have failed")
		t.Log("Possible causes:")
		t.Log("  1. Initialize() missing experimentalApi: true")
		t.Log("  2. thread/start dynamicTools not passed correctly")
		t.Log("  3. codex agent ignored the dynamic tools")
		t.Log("  4. EventDynamicToolCall event mapping incorrect")
	} else {
		t.Logf("SUCCESS: agent used %d LSP tools: %v", len(lspToolCalls), lspToolCalls)
	}

	if agentDeltas == 0 {
		t.Error("FAIL: no agent message deltas received — agent may not have responded")
	}
}

// truncate 截断字符串到指定长度。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
