// server_test.go — E2E 测试: WebSocket 连接 → 所有 JSON-RPC 方法。
//
// 测试策略:
//   - 启动真实 WebSocket 服务器 (随机端口)
//   - 通过 gorilla/websocket 客户端连接
//   - 逐一调用所有 47 个 JSON-RPC 方法
//   - 验证: 无 parse error, 无 method_not_found, 有有效 result
//   - 测试通知广播
package apiserver

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
)

// testEnv 测试环境。
type testEnv struct {
	srv    *Server
	addr   string
	cancel context.CancelFunc
}

// setupTestServer 启动测试服务器。
func setupTestServer(t *testing.T) *testEnv {
	t.Helper()

	mgr := runner.NewAgentManager()
	lspMgr := lsp.NewManager(nil)
	cfg := &config.Config{}

	srv := New(Deps{
		Manager: mgr,
		LSP:     lspMgr,
		Config:  cfg,
	})

	// 随机端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = srv.ListenAndServe(ctx, addr)
	}()

	// 等待服务器就绪
	time.Sleep(100 * time.Millisecond)

	return &testEnv{srv: srv, addr: addr, cancel: cancel}
}

// dial 连接 WebSocket。
func dial(t *testing.T, addr string) *websocket.Conn {
	t.Helper()
	url := "ws://" + addr + "/"
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	return ws
}

// rpcCall 发送 JSON-RPC 请求并读取响应 (3s 超时)。
func rpcCall(t *testing.T, ws *websocket.Conn, id int, method string, params any) Response {
	t.Helper()

	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
	} else {
		rawParams = json.RawMessage("{}")
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  json.RawMessage(rawParams),
	}

	data, _ := json.Marshal(req)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}

	// 3s 超时: 防止 thread/start 等需要真实 agent 的方法阻塞
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, respData, err := ws.ReadMessage()
	ws.SetReadDeadline(time.Time{}) // 重置
	if err != nil {
		t.Fatalf("read %s: %v (method may be hanging — check handler)", method, err)
	}

	var resp Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal response for %s: %v\nraw: %s", method, err, string(respData))
	}

	return resp
}

// TestAllMethodsWired 测试所有方法是否正确注册并可调用。
//
// 每个方法使用独立 WebSocket 连接, 避免单个方法超时导致级联失败。
// thread/start 会尝试 Launch 真实 codex, 在此跳过。
func TestAllMethodsWired(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	type testCase struct {
		method             string
		params             any
		allowInternalError bool
		skip               bool // 需要真实 agent binary
	}

	cases := []testCase{
		// § 1. 初始化
		{method: "initialize", params: map[string]any{"protocolVersion": "2.0"}},

		// § 2. 线程
		{method: "thread/start", params: map[string]any{"cwd": "/tmp"}, skip: true}, // 需要 codex binary
		{method: "thread/resume", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},
		{method: "thread/fork", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},
		{method: "thread/archive", params: map[string]any{"threadId": "test"}},
		{method: "thread/unarchive", params: map[string]any{"threadId": "test"}},
		{method: "thread/name/set", params: map[string]any{"threadId": "nonexist", "name": "foo"}, allowInternalError: true},
		{method: "thread/compact/start", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},
		{method: "thread/rollback", params: map[string]any{"threadId": "nonexist", "turnIndex": 0}, allowInternalError: true},
		{method: "thread/list", params: nil},
		{method: "thread/loaded/list", params: nil},
		{method: "thread/read", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},
		{method: "thread/backgroundTerminals/clean", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},

		// § 3. 对话 (需要真实 agent, 允许 internal error)
		{method: "turn/start", params: map[string]any{"threadId": "nonexist", "input": []map[string]any{{"type": "text", "text": "hello"}}}, allowInternalError: true},
		{method: "turn/steer", params: map[string]any{"threadId": "nonexist", "input": []map[string]any{{"type": "text", "text": "steer"}}}, allowInternalError: true},
		{method: "turn/interrupt", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},
		{method: "review/start", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},

		// § 4. 文件搜索
		{method: "fuzzyFileSearch", params: map[string]any{"query": "main", "roots": []string{"/tmp"}}},
		{method: "fuzzyFileSearch/sessionStart", params: map[string]any{"sessionId": "s1", "roots": []string{"/tmp"}}},
		{method: "fuzzyFileSearch/sessionUpdate", params: map[string]any{"sessionId": "s1", "query": "test"}},
		{method: "fuzzyFileSearch/sessionStop", params: map[string]any{"sessionId": "s1"}},

		// § 5. Skills / Apps
		{method: "skills/list", params: nil},
		{method: "skills/remote/read", params: map[string]any{"url": "https://example.com/skill"}, allowInternalError: true},
		{method: "skills/remote/write", params: map[string]any{"name": "test-e2e-skill", "content": "# Test"}},
		{method: "skills/config/write", params: map[string]any{"agent_id": "test-agent", "skills": []string{"test-skill"}}},
		{method: "app/list", params: nil},

		// § 6. 模型 / 配置
		{method: "model/list", params: nil},
		{method: "collaborationMode/list", params: nil},
		{method: "experimentalFeature/list", params: nil},
		{method: "config/read", params: nil},
		{method: "config/value/write", params: map[string]any{"key": "TEST_E2E_KEY", "value": "v1"}},
		{method: "config/batchWrite", params: map[string]any{"entries": []map[string]any{{"key": "K1", "value": "V1"}}}},
		{method: "configRequirements/read", params: nil},

		// § 7. 账号
		{method: "account/login/start", params: map[string]any{"authMode": "apiKey"}},
		{method: "account/login/cancel", params: nil},
		{method: "account/logout", params: nil},
		{method: "account/read", params: nil},
		{method: "account/rateLimits/read", params: nil},

		// § 8. MCP
		{method: "mcpServer/oauth/login", params: map[string]any{"serverId": "test"}},
		{method: "config/mcpServer/reload", params: nil},
		{method: "mcpServerStatus/list", params: nil},

		// § 9. 命令执行 / 其他
		{method: "command/exec", params: map[string]any{"argv": []string{"echo", "hello"}}},
		{method: "feedback/upload", params: map[string]any{"threadId": "test", "rating": 5, "comment": "great"}},

		// § 10. 斜杠命令 (SOCKS 独有)
		{method: "thread/undo", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},
		{method: "thread/model/set", params: map[string]any{"threadId": "nonexist", "model": "gpt-4"}, allowInternalError: true},
		{method: "thread/personality/set", params: map[string]any{"threadId": "nonexist", "personality": "friendly"}, allowInternalError: true},
		{method: "thread/approvals/set", params: map[string]any{"threadId": "nonexist", "policy": "never"}, allowInternalError: true},
		{method: "thread/mcp/list", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},
		{method: "thread/skills/list", params: map[string]any{"threadId": "nonexist"}, allowInternalError: true},
		{method: "thread/debugMemory", params: map[string]any{"threadId": "nonexist", "action": "drop"}, allowInternalError: true},
	}

	passed, failed, skipped := 0, 0, 0

	for i, tc := range cases {
		id := i + 1
		t.Run(tc.method, func(t *testing.T) {
			if tc.skip {
				t.Skipf("[%s] skipped (requires real codex binary)", tc.method)
				skipped++
				return
			}

			// 每个方法独立连接
			ws := dial(t, env.addr)
			defer ws.Close()

			resp := rpcCall(t, ws, id, tc.method, tc.params)

			// 1. ID 必须匹配
			respID, _ := json.Marshal(resp.ID)
			expectedID, _ := json.Marshal(float64(id))
			if string(respID) != string(expectedID) {
				t.Errorf("[%s] id mismatch: got %s, want %s", tc.method, string(respID), string(expectedID))
				failed++
				return
			}

			// 2. 不允许 parse error / method_not_found / invalid_request
			if resp.Error != nil {
				switch resp.Error.Code {
				case CodeParseError:
					t.Fatalf("[%s] PARSE ERROR: %s", tc.method, resp.Error.Message)
				case CodeMethodNotFound:
					t.Fatalf("[%s] METHOD NOT FOUND — wiring broken!", tc.method)
				case CodeInvalidRequest:
					t.Fatalf("[%s] INVALID REQUEST: %s", tc.method, resp.Error.Message)
				default:
					if !tc.allowInternalError {
						t.Errorf("[%s] unexpected error (code=%d): %s", tc.method, resp.Error.Code, resp.Error.Message)
						failed++
						return
					}
					t.Logf("[%s] expected internal error: %s", tc.method, resp.Error.Message)
				}
				passed++
				return
			}

			// 3. 成功
			if resp.Result == nil {
				t.Errorf("[%s] result is nil", tc.method)
				failed++
				return
			}
			passed++
		})
	}

	t.Logf("=== SUMMARY: %d passed, %d failed, %d skipped (of %d total) ===", passed, failed, skipped, len(cases))
}

// TestNotificationNoResponse 测试 client notification (无 id) 不应收到响应。
func TestNotificationNoResponse(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	ws := dial(t, env.addr)
	defer ws.Close()

	// 发送 "initialized" 通知 (无 id)
	notif := `{"jsonrpc":"2.0","method":"initialized"}`
	if err := ws.WriteMessage(websocket.TextMessage, []byte(notif)); err != nil {
		t.Fatal(err)
	}

	// 紧接着发一个有 id 的请求, 应该直接收到这个请求的响应 (说明 notification 没有产生响应)
	req := `{"jsonrpc":"2.0","id":999,"method":"model/list","params":{}}`
	if err := ws.WriteMessage(websocket.TextMessage, []byte(req)); err != nil {
		t.Fatal(err)
	}

	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}

	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	// 收到的第一条响应应该是 id=999 (model/list), 而不是 notification 的响应
	respID, _ := json.Marshal(resp.ID)
	if string(respID) != "999" {
		t.Errorf("expected response id=999, got %s (notification leaked a response)", string(respID))
	}
}

// TestNotificationBroadcast 测试服务端 Notify 是否广播到所有连接。
func TestNotificationBroadcast(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	// 连接两个客户端
	ws1 := dial(t, env.addr)
	defer ws1.Close()
	ws2 := dial(t, env.addr)
	defer ws2.Close()

	time.Sleep(50 * time.Millisecond) // 等连接注册

	// 服务端推送通知
	env.srv.Notify("turn/started", map[string]any{
		"threadId": "test-thread",
		"turnId":   "test-turn",
	})

	// 两个客户端都应收到
	var wg sync.WaitGroup
	var received [2]bool

	for i, ws := range []*websocket.Conn{ws1, ws2} {
		wg.Add(1)
		go func(idx int, conn *websocket.Conn) {
			defer wg.Done()
			conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var notif Notification
			if err := json.Unmarshal(data, &notif); err != nil {
				return
			}
			if notif.Method == "turn/started" {
				received[idx] = true
			}
		}(i, ws)
	}

	wg.Wait()

	for i, ok := range received {
		if !ok {
			t.Errorf("client %d did not receive notification", i+1)
		}
	}
}

// TestMethodNotFound 测试未知方法返回 -32601。
func TestMethodNotFound(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	ws := dial(t, env.addr)
	defer ws.Close()

	resp := rpcCall(t, ws, 1, "nonexistent/method", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected code %d, got %d", CodeMethodNotFound, resp.Error.Code)
	}
}

// TestParseError 测试无效 JSON 返回 -32700。
func TestParseError(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	ws := dial(t, env.addr)
	defer ws.Close()

	// 发送无效 JSON
	if err := ws.WriteMessage(websocket.TextMessage, []byte("{invalid json")); err != nil {
		t.Fatal(err)
	}

	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}

	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Error == nil || resp.Error.Code != CodeParseError {
		t.Errorf("expected parse error, got: %+v", resp)
	}
}

// TestCommandExecE2E 测试 command/exec 真实执行。
func TestCommandExecE2E(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	ws := dial(t, env.addr)
	defer ws.Close()

	resp := rpcCall(t, ws, 1, "command/exec", map[string]any{
		"argv": []string{"echo", "hello-e2e"},
	})

	if resp.Error != nil {
		t.Fatalf("command/exec error: %s", resp.Error.Message)
	}

	// 解析 result
	resultBytes, _ := json.Marshal(resp.Result)
	var result map[string]any
	json.Unmarshal(resultBytes, &result)

	stdout, _ := result["stdout"].(string)
	if !strings.Contains(stdout, "hello-e2e") {
		t.Errorf("expected stdout to contain 'hello-e2e', got: %q", stdout)
	}

	exitCode, _ := result["exitCode"].(float64)
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %.0f", exitCode)
	}
}

// TestMethodCount 确认注册的方法数量 = 协议规定的数量。
func TestMethodCount(t *testing.T) {
	mgr := runner.NewAgentManager()
	lspMgr := lsp.NewManager(nil)
	cfg := &config.Config{}

	srv := New(Deps{Manager: mgr, LSP: lspMgr, Config: cfg})

	// 协议规定: 44 v2 methods + 7 slash commands = 51
	const expectedMin = 51
	actual := len(srv.methods)
	if actual < expectedMin {
		t.Errorf("method count %d < expected minimum %d", actual, expectedMin)
	}
	t.Logf("registered methods: %d", actual)

	// 打印所有方法名 (便于调试)
	methods := make([]string, 0, actual)
	for m := range srv.methods {
		methods = append(methods, m)
	}
	t.Logf("methods: %v", methods)
}

// TestFuzzyFileSearch 测试文件搜索功能。
func TestFuzzyFileSearch(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	ws := dial(t, env.addr)
	defer ws.Close()

	resp := rpcCall(t, ws, 1, "fuzzyFileSearch", map[string]any{
		"query": "server",
		"roots": []string{"/tmp"},
	})

	if resp.Error != nil {
		t.Fatalf("fuzzyFileSearch error: %s", resp.Error.Message)
	}

	// 验证 result 结构正确
	resultBytes, _ := json.Marshal(resp.Result)
	var result map[string]any
	json.Unmarshal(resultBytes, &result)

	files, ok := result["files"]
	if !ok {
		t.Fatal("missing 'files' key in result")
	}

	// files 应该是数组 (可能为空, 但不应为 nil)
	filesArr, ok := files.([]any)
	if !ok {
		t.Fatalf("files should be array, got %T", files)
	}

	t.Logf("fuzzyFileSearch found %d files matching 'server' in /tmp", len(filesArr))
}
