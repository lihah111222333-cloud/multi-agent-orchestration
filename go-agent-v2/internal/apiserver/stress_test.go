// stress_test.go — 并发压力测试。
//
// 测试 WebSocket 服务器在高并发场景下的稳定性:
//   - 多连接并发
//   - 高频请求
//   - 通知广播压测
//   - 大消息处理
package apiserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestStressConcurrentConnections 50 个并发连接同时发请求。
func TestStressConcurrentConnections(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	const numConns = 50
	var wg sync.WaitGroup
	var success, fail atomic.Int64

	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(connIdx int) {
			defer wg.Done()

			ws := dial(t, env.addr)
			defer ws.Close()

			resp := rpcCall(t, ws, connIdx+1, "initialize", map[string]any{
				"protocolVersion": "2.0",
				"clientName":      fmt.Sprintf("stress-client-%d", connIdx),
			})

			if resp.Error != nil {
				fail.Add(1)
				t.Logf("[conn-%d] error: %s", connIdx, resp.Error.Message)
			} else {
				success.Add(1)
			}
		}(i)
	}

	wg.Wait()
	t.Logf("=== CONCURRENT CONNECTIONS: %d success, %d fail (of %d) ===", success.Load(), fail.Load(), numConns)

	if fail.Load() > 0 {
		t.Errorf("some connections failed: %d/%d", fail.Load(), numConns)
	}
}

// TestStressHighFrequencyRequests 单连接高频请求 (1000 次)。
func TestStressHighFrequencyRequests(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	ws := dial(t, env.addr)
	defer ws.Close()

	const numRequests = 1000
	start := time.Now()
	var fail int

	for i := 1; i <= numRequests; i++ {
		resp := rpcCall(t, ws, i, "thread/list", nil)
		if resp.Error != nil {
			fail++
		}
	}

	elapsed := time.Since(start)
	rps := float64(numRequests) / elapsed.Seconds()
	t.Logf("=== HIGH FREQUENCY: %d requests in %v (%.0f req/s), %d failures ===", numRequests, elapsed, rps, fail)

	if fail > 0 {
		t.Errorf("some requests failed: %d/%d", fail, numRequests)
	}
	if rps < 100 {
		t.Errorf("throughput too low: %.0f req/s (expect >= 100)", rps)
	}
}

// TestStressNotificationBroadcast 多连接通知广播压测。
func TestStressNotificationBroadcast(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	const numConns = 10
	const numNotifs = 100

	// 建立多个读取连接
	conns := make([]*websocket.Conn, numConns)
	for i := 0; i < numConns; i++ {
		conns[i] = dial(t, env.addr)
		defer conns[i].Close()
	}

	// 收集通知
	var received atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(ws *websocket.Conn) {
			defer wg.Done()
			ws.SetReadDeadline(time.Now().Add(3 * time.Second))
			for {
				_, _, err := ws.ReadMessage()
				if err != nil {
					return
				}
				received.Add(1)
			}
		}(conns[i])
	}

	// 发送通知
	for i := 0; i < numNotifs; i++ {
		env.srv.Notify("test/stress", map[string]any{"seq": i})
	}

	// 等待接收
	time.Sleep(2 * time.Second)

	// 关闭连接以终止读取
	for _, ws := range conns {
		ws.Close()
	}
	wg.Wait()

	expected := numConns * numNotifs
	actual := int(received.Load())
	t.Logf("=== NOTIFICATION BROADCAST: %d/%d received ===", actual, expected)

	if actual < expected*90/100 { // 允许 10% 丢失
		t.Errorf("too many missed notifications: %d/%d (< 90%%)", actual, expected)
	}
}

// TestStressLargeMessage 大消息处理 (1MB JSON)。
func TestStressLargeMessage(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	ws := dial(t, env.addr)
	defer ws.Close()

	// 构造 1MB payload
	largeValue := strings.Repeat("x", 1<<20) // 1MB
	resp := rpcCall(t, ws, 1, "config/value/write", map[string]any{
		"key":   "STRESS_TEST_LARGE",
		"value": largeValue,
	})

	if resp.Error != nil {
		t.Logf("large message error (expected for some configs): %s", resp.Error.Message)
	} else {
		t.Logf("=== LARGE MESSAGE: 1MB payload accepted ===")
	}

	// 必须不 panic 或挂起 — 能到这里就说明服务没崩
}

// TestStressRapidConnectDisconnect 快速连接断开压测。
func TestStressRapidConnectDisconnect(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	const iterations = 100
	var wg sync.WaitGroup
	var fail atomic.Int64

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws := dial(t, env.addr)
			_ = ws.Close()
		}()
	}

	wg.Wait()
	t.Logf("=== RAPID CONNECT/DISCONNECT: %d iterations, %d failures ===", iterations, fail.Load())
}

// TestStressParallelMethodCalls 多方法并发调用 (连接复用)。
//
// 20 个连接分摊 500 个请求 — 测试真正的方法并发, 而非连接数上限。
func TestStressParallelMethodCalls(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	methods := []struct {
		name   string
		params any
	}{
		{"initialize", map[string]any{"protocolVersion": "2.0"}},
		{"thread/list", nil},
		{"thread/loaded/list", nil},
		{"model/list", nil},
		{"config/read", nil},
		{"account/read", nil},
		{"skills/list", nil},
		{"app/list", nil},
		{"experimentalFeature/list", nil},
		{"mcpServerStatus/list", nil},
	}

	const poolSize = 20
	const perMethod = 50
	total := len(methods) * perMethod

	// 连接池: 每个连接有自己的写锁
	type pooledConn struct {
		ws *websocket.Conn
		mu sync.Mutex
	}
	pool := make([]*pooledConn, poolSize)
	for i := range pool {
		ws := dial(t, env.addr)
		pool[i] = &pooledConn{ws: ws}
		defer ws.Close()
	}

	var wg sync.WaitGroup
	var success, fail atomic.Int64
	var reqID atomic.Int64

	for _, m := range methods {
		for j := 0; j < perMethod; j++ {
			wg.Add(1)
			go func(name string, params any) {
				defer wg.Done()

				id := int(reqID.Add(1))
				pc := pool[id%poolSize]

				req := map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"method":  name,
				}
				if params != nil {
					req["params"] = params
				}

				data, _ := json.Marshal(req)

				// 序列化同一连接的读写 (gorilla/websocket 不支持并发)
				pc.mu.Lock()
				err := pc.ws.WriteMessage(websocket.TextMessage, data)
				if err != nil {
					pc.mu.Unlock()
					fail.Add(1)
					return
				}
				pc.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
				_, _, err = pc.ws.ReadMessage()
				pc.mu.Unlock()

				if err != nil {
					fail.Add(1)
					return
				}
				success.Add(1)
			}(m.name, m.params)
		}
	}

	wg.Wait()
	t.Logf("=== PARALLEL METHODS: %d/%d success ===", success.Load(), total)

	if fail.Load() > 0 {
		t.Errorf("parallel method failures: %d/%d", fail.Load(), total)
	}
}
