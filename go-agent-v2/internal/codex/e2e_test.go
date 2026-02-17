// e2e_test.go — Codex 集成 E2E 测试。
//
// NOTE: WebSocket/SSE 测试已移除 (socket 方法已注释掉)。
// 保留 HTTP REST 测试 + 新增 AppServerClient 占位测试。
//
// 使用方式:
//  1. HTTP REST测试: codex http-api --port 4000 && CODEX_PORT=4000 go test -v -run TestE2E -timeout 120s ./internal/codex/
//  2. App-Server 测试: 需要 codex app-server 可用
package codex

import (
	"os"
	"strconv"
	"testing"
)

// getCodexPort 从环境变量获取 codex 端口。
func getCodexPort(t *testing.T) int {
	t.Helper()
	portStr := os.Getenv("CODEX_PORT")
	if portStr == "" {
		t.Skip("CODEX_PORT not set, skipping E2E test. Start codex first: codex http-api --port 4000")
		return 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("invalid CODEX_PORT: %q", portStr)
	}
	return port
}

// TestE2EHealth 测试: 健康检查。
func TestE2EHealth(t *testing.T) {
	port := getCodexPort(t)
	client := NewClient(port)

	if err := client.Health(); err != nil {
		t.Fatalf("Health: %v", err)
	}
	t.Logf("✅ Health check passed on port %d", port)
}

// TestE2ECreateAndDeleteThread 测试: 创建 + 删除线程。
func TestE2ECreateAndDeleteThread(t *testing.T) {
	port := getCodexPort(t)
	client := NewClient(port)

	// 创建
	resp, err := client.CreateThread(CreateThreadRequest{
		Prompt: "You are a test assistant. Reply briefly.",
		Cwd:    "/tmp",
	})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if resp.ThreadID == "" {
		t.Fatal("ThreadID is empty")
	}
	t.Logf("✅ Thread created: %s", resp.ThreadID)

	// 列表
	threads, err := client.ListThreads()
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	found := false
	for _, th := range threads {
		if th.ThreadID == resp.ThreadID {
			found = true
		}
	}
	if !found {
		t.Error("Created thread not found in list")
	}
	t.Logf("✅ ListThreads: %d threads", len(threads))

	// 删除
	if err := client.DeleteThread(resp.ThreadID); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	t.Log("✅ Thread deleted")
}

// TestE2ESubmitHTTP 测试: 纯 HTTP POST Submit。
func TestE2ESubmitHTTP(t *testing.T) {
	port := getCodexPort(t)
	client := NewClient(port)

	// 创建线程
	resp, err := client.CreateThread(CreateThreadRequest{
		Prompt: "You are a test assistant. Always reply with exactly: OK",
		Cwd:    "/tmp",
	})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	t.Logf("Thread: %s", resp.ThreadID)
	defer func() { _ = client.DeleteThread(resp.ThreadID) }()

	// Submit via HTTP POST
	if err := client.Submit("Say hello", nil, nil, nil); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	t.Log("✅ HTTP Submit succeeded")
}

// NOTE: WebSocket E2E 测试 (TestE2EWebSocketLifecycle, TestE2ESlashCommand,
// TestE2ETransportMode) 已移除。
// 实时通信测试应使用 AppServerClient + JSON-RPC。
