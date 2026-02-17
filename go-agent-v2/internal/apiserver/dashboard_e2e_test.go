// dashboard_e2e_test.go — Dashboard JSON-RPC 方法 E2E 测试 (真实数据库)。
//
// 测试策略:
//   - 连接真实 PostgreSQL (从环境变量/fallback .env 读取)
//   - 通过 InvokeMethod 调用全部 12 个 dashboard/* 方法
//   - 验证返回结构正确、无 panic、错误优雅处理
//
// 运行:
//
//	POSTGRES_CONNECTION_STRING=postgresql://... go test -run TestDashboard -v ./internal/apiserver/
package apiserver

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
)

// dashTestEnv Dashboard E2E 测试环境 (带 DB)。
type dashTestEnv struct {
	srv  *Server
	pool *pgxpool.Pool
}

// setupDashTestServer 启动带 DB 的测试服务器。
// 如果没有 POSTGRES_CONNECTION_STRING 则跳过测试。
func setupDashTestServer(t *testing.T) *dashTestEnv {
	t.Helper()

	connStr := os.Getenv("POSTGRES_CONNECTION_STRING")
	if connStr == "" {
		// 尝试从 .env 文件读取 (本地开发)
		connStr = readEnvFile(".env", "POSTGRES_CONNECTION_STRING")
	}
	if connStr == "" {
		// 尝试项目根目录
		connStr = readEnvFile("../../.env", "POSTGRES_CONNECTION_STRING")
	}
	if connStr == "" {
		t.Skip("POSTGRES_CONNECTION_STRING not set, skipping DB E2E tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("cannot connect to DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("DB ping failed: %v", err)
	}

	mgr := runner.NewAgentManager()
	lspMgr := lsp.NewManager(nil)
	cfg := &config.Config{}

	srv := New(Deps{
		Manager:   mgr,
		LSP:       lspMgr,
		Config:    cfg,
		DB:        pool,
		SkillsDir: "../../.agent/skills",
	})

	return &dashTestEnv{srv: srv, pool: pool}
}

// readEnvFile 从 .env 文件读取指定变量 (简单解析, 非生产用)。
func readEnvFile(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if strings.TrimSpace(parts[0]) == key && len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// ============================================================
// § 1. 全部 Dashboard 方法可调用 (无 panic)
// ============================================================

// TestDashboard_AllMethodsCallable 测试所有 dashboard/* 方法可以成功调用,
// 返回非 nil 结果, 且不会 panic。
func TestDashboard_AllMethodsCallable(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	tests := []struct {
		method string
		params string
	}{
		{"dashboard/agentStatus", `{}`},
		{"dashboard/agentStatus", `{"status":"running"}`},
		{"dashboard/dags", `{}`},
		{"dashboard/dags", `{"keyword":"test","status":"running","limit":10}`},
		{"dashboard/taskAcks", `{}`},
		{"dashboard/taskAcks", `{"keyword":"test","status":"pending","limit":10}`},
		{"dashboard/taskTraces", `{}`},
		{"dashboard/taskTraces", `{"agentId":"test-agent","limit":10}`},
		{"dashboard/commandCards", `{}`},
		{"dashboard/commandCards", `{"keyword":"test","limit":10}`},
		{"dashboard/prompts", `{}`},
		{"dashboard/prompts", `{"agentKey":"default","keyword":"test","limit":10}`},
		{"dashboard/sharedFiles", `{}`},
		{"dashboard/sharedFiles", `{"prefix":"/","limit":10}`},
		{"dashboard/skills", `{}`},
		{"dashboard/auditLogs", `{}`},
		{"dashboard/auditLogs", `{"eventType":"agent","action":"start","limit":10}`},
		{"dashboard/aiLogs", `{}`},
		{"dashboard/aiLogs", `{"category":"chat","keyword":"test","limit":10}`},
		{"dashboard/busLogs", `{}`},
		{"dashboard/busLogs", `{"category":"bus","severity":"error","limit":10}`},
	}

	for _, tt := range tests {
		t.Run(tt.method+"_"+tt.params, func(t *testing.T) {
			ctx := context.Background()
			result, err := env.srv.InvokeMethod(ctx, tt.method, json.RawMessage(tt.params))
			if err != nil {
				t.Fatalf("InvokeMethod(%s) error: %v", tt.method, err)
			}
			if result == nil {
				t.Fatalf("InvokeMethod(%s) returned nil", tt.method)
			}

			// 验证结果可以序列化为 JSON
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}
			if len(data) < 2 { // 至少 "{}"
				t.Fatalf("result too small: %s", string(data))
			}
			t.Logf("✓ %s → %d bytes", tt.method, len(data))
		})
	}
}

// ============================================================
// § 2. Dashboard 返回结构验证
// ============================================================

// TestDashboard_ResponseStructure 验证每个方法返回的 JSON 结构包含正确的顶层键。
func TestDashboard_ResponseStructure(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	tests := []struct {
		method    string
		params    string
		expectKey string // 期望的顶层键
	}{
		{"dashboard/agentStatus", `{}`, "agents"},
		{"dashboard/dags", `{}`, "dags"},
		{"dashboard/taskAcks", `{}`, "acks"},
		{"dashboard/taskTraces", `{}`, "traces"},
		{"dashboard/commandCards", `{}`, "cards"},
		{"dashboard/prompts", `{}`, "prompts"},
		{"dashboard/sharedFiles", `{}`, "files"},
		{"dashboard/skills", `{}`, "skills"},
		{"dashboard/auditLogs", `{}`, "logs"},
		{"dashboard/aiLogs", `{}`, "logs"},
		{"dashboard/busLogs", `{}`, "logs"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			ctx := context.Background()
			result, err := env.srv.InvokeMethod(ctx, tt.method, json.RawMessage(tt.params))
			if err != nil {
				t.Fatalf("error: %v", err)
			}

			// 序列化再反序列化检查结构
			data, _ := json.Marshal(result)
			var m map[string]json.RawMessage
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("not a JSON object: %s", string(data))
			}
			if _, ok := m[tt.expectKey]; !ok {
				t.Fatalf("missing key %q in response: %s", tt.expectKey, string(data))
			}

			// 验证值是数组
			var arr []json.RawMessage
			if err := json.Unmarshal(m[tt.expectKey], &arr); err != nil {
				t.Fatalf("key %q is not an array: %s", tt.expectKey, string(m[tt.expectKey]))
			}
			t.Logf("✓ %s.%s = %d items", tt.method, tt.expectKey, len(arr))
		})
	}
}

// ============================================================
// § 3. Dashboard 无 DB 时优雅降级
// ============================================================

// TestDashboard_NoDB_GracefulDegradation 验证无 DB 连接时 dashboard 方法返回空数组。
func TestDashboard_NoDB_GracefulDegradation(t *testing.T) {
	// 不传 DB → stores = nil
	srv := New(Deps{
		Manager: runner.NewAgentManager(),
		LSP:     lsp.NewManager(nil),
		Config:  &config.Config{},
	})

	methods := []string{
		"dashboard/agentStatus",
		"dashboard/dags",
		"dashboard/taskAcks",
		"dashboard/taskTraces",
		"dashboard/commandCards",
		"dashboard/prompts",
		"dashboard/sharedFiles",
		// dashboard/skills 依赖文件系统, 非 DB, 不在此测试
		"dashboard/auditLogs",
		"dashboard/aiLogs",
		"dashboard/busLogs",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			result, err := srv.InvokeMethod(context.Background(), method, json.RawMessage(`{}`))
			if err != nil {
				t.Fatalf("should not error without DB: %v", err)
			}
			data, _ := json.Marshal(result)
			if !strings.Contains(string(data), "[]") {
				t.Fatalf("expected empty array, got: %s", string(data))
			}
			t.Logf("✓ %s → graceful empty", method)
		})
	}
}

// ============================================================
// § 4. Dashboard 带筛选参数
// ============================================================

// TestDashboard_FilterParams 验证带过滤参数调用不会出错。
func TestDashboard_FilterParams(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	tests := []struct {
		method string
		params string
	}{
		{"dashboard/agentStatus", `{"status":"idle"}`},
		{"dashboard/dags", `{"keyword":"nonexistent","status":"completed","limit":5}`},
		{"dashboard/taskAcks", `{"keyword":"x","status":"done","priority":"high","assignedTo":"nobody","limit":1}`},
		{"dashboard/taskTraces", `{"agentId":"nonexistent","keyword":"x","limit":1}`},
		{"dashboard/commandCards", `{"keyword":"nonexistent","limit":1}`},
		{"dashboard/prompts", `{"agentKey":"nonexistent","keyword":"x","limit":1}`},
		{"dashboard/sharedFiles", `{"prefix":"nonexistent/","limit":1}`},
		{"dashboard/auditLogs", `{"eventType":"none","action":"none","actor":"x","keyword":"x","limit":1}`},
		{"dashboard/aiLogs", `{"category":"none","keyword":"x","limit":1}`},
		{"dashboard/busLogs", `{"category":"none","severity":"none","keyword":"x","limit":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result, err := env.srv.InvokeMethod(context.Background(), tt.method, json.RawMessage(tt.params))
			if err != nil {
				t.Fatalf("filter call error: %v", err)
			}
			data, _ := json.Marshal(result)
			t.Logf("✓ %s (filtered) → %d bytes", tt.method, len(data))
		})
	}
}

// ============================================================
// § 5. DAG 详情查询
// ============================================================

// TestDashboard_DAGDetail_MissingKey 验证 dagDetail 缺少 dagKey 返回错误。
func TestDashboard_DAGDetail_MissingKey(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	_, err := env.srv.InvokeMethod(context.Background(), "dashboard/dagDetail", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing dagKey")
	}
	// 可能是 "dagKey" 或 "dag_key" 相关错误
	errStr := err.Error()
	if !strings.Contains(errStr, "dagKey") && !strings.Contains(errStr, "dag_key") && !strings.Contains(errStr, "required") {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("✓ dagDetail without key → %v", err)
}

// ============================================================
// § 6. Limit 边界测试
// ============================================================

// TestDashboard_LimitBoundary 验证 limit 参数边界值处理。
func TestDashboard_LimitBoundary(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	tests := []struct {
		name   string
		params string
	}{
		{"zero_limit", `{"limit":0}`},
		{"negative_limit", `{"limit":-1}`},
		{"huge_limit", `{"limit":99999}`},
		{"normal_limit", `{"limit":50}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := env.srv.InvokeMethod(context.Background(), "dashboard/dags", json.RawMessage(tt.params))
			if err != nil {
				t.Fatalf("limit %s error: %v", tt.name, err)
			}
			data, _ := json.Marshal(result)
			t.Logf("✓ dags %s → %d bytes", tt.name, len(data))
		})
	}
}

// ============================================================
// § 7. 并发调用安全性
// ============================================================

// TestDashboard_ConcurrentCalls 验证并发调用 dashboard 方法无 race。
func TestDashboard_ConcurrentCalls(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	methods := []string{
		"dashboard/agentStatus",
		"dashboard/dags",
		"dashboard/taskAcks",
		"dashboard/taskTraces",
		"dashboard/commandCards",
		"dashboard/prompts",
		"dashboard/sharedFiles",
		"dashboard/auditLogs",
		"dashboard/aiLogs",
		"dashboard/busLogs",
	}

	done := make(chan error, len(methods)*3)

	for i := 0; i < 3; i++ {
		for _, method := range methods {
			go func(m string) {
				_, err := env.srv.InvokeMethod(context.Background(), m, json.RawMessage(`{}`))
				done <- err
			}(method)
		}
	}

	for i := 0; i < len(methods)*3; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent call error: %v", err)
		}
	}
	t.Logf("✓ %d concurrent calls completed", len(methods)*3)
}

// ============================================================
// § 8. 日志字段完整性检查
// ============================================================

// TestDashboard_LogFieldCompleteness 验证日志返回的 JSON 字段齐全。
func TestDashboard_LogFieldCompleteness(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	// 检查一组 JSON 数组中的第一条记录是否包含全部预期字段。
	checkFields := func(t *testing.T, method string, expectKeys []string) {
		t.Helper()
		result, err := env.srv.InvokeMethod(context.Background(), method, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("InvokeMethod(%s) error: %v", method, err)
		}

		data, _ := json.Marshal(result)
		var top map[string]json.RawMessage
		if err := json.Unmarshal(data, &top); err != nil {
			t.Fatalf("not a JSON object: %s", string(data))
		}

		// 找到数组 key (logs, traces, etc.)
		var arrRaw json.RawMessage
		for _, v := range top {
			var arr []json.RawMessage
			if json.Unmarshal(v, &arr) == nil && len(arr) > 0 {
				arrRaw = v
				break
			}
		}
		if arrRaw == nil {
			t.Skipf("%s: no data in DB, skipping field check", method)
			return
		}

		var items []map[string]any
		if err := json.Unmarshal(arrRaw, &items); err != nil {
			t.Fatalf("cannot parse array: %v", err)
		}
		if len(items) == 0 {
			t.Skipf("%s: empty results", method)
			return
		}

		first := items[0]
		var missing []string
		for _, key := range expectKeys {
			if _, ok := first[key]; !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s: missing fields in first item: %v\n  available: %v", method, missing, mapKeys(first))
		} else {
			t.Logf("✓ %s: all %d expected fields present", method, len(expectKeys))
		}
	}

	t.Run("auditLogs_fields", func(t *testing.T) {
		checkFields(t, "dashboard/auditLogs", []string{
			"ts", "event_type", "action", "result", "actor", "target", "detail", "level", "extra",
		})
	})

	t.Run("aiLogs_fields", func(t *testing.T) {
		checkFields(t, "dashboard/aiLogs", []string{
			"ts", "level", "logger", "message", "raw",
			"category", "method", "url", "endpoint", "status_code", "status_text", "model",
		})
	})

	t.Run("busLogs_fields", func(t *testing.T) {
		checkFields(t, "dashboard/busLogs", []string{
			"ts", "category", "severity", "source", "tool_name", "message", "traceback", "extra",
		})
	})

	t.Run("taskTraces_fields", func(t *testing.T) {
		checkFields(t, "dashboard/taskTraces", []string{
			"id", "trace_id", "span_id", "parent_span_id", "span_name", "component",
			"status", "input_payload", "output_payload", "error_text", "metadata",
			"started_at", "duration_ms",
		})
	})
}

// mapKeys 返回 map 的所有 key (辅助调试)。
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
