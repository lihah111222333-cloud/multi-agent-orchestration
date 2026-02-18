// rpc-test — 直接测试 app-server JSON-RPC 方法。
package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func main() {
	logger.Init("development")

	addr := "ws://127.0.0.1:4500"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	logger.Info("connecting", logger.FieldAddr, addr)
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		logger.Fatal("dial failed", logger.Any(logger.FieldError, err))
	}
	defer conn.Close()
	logger.Info("connected")

	// 后台读取所有消息 (包括通知)
	util.SafeGo(func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				logger.Error("read error", logger.Any(logger.FieldError, err))
				return
			}
			// 美化 JSON
			var pretty json.RawMessage
			if json.Unmarshal(msg, &pretty) == nil {
				out, _ := json.MarshalIndent(pretty, "", "  ")
				logger.Info("recv", "data", string(out))
			} else {
				logger.Info("recv", "data", string(msg))
			}
		}
	})

	// 1. 发送 thread/start
	req1 := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "thread/start",
		"params":  map[string]any{"cwd": "."},
	}
	data1, err := json.Marshal(req1)
	if err != nil {
		logger.Fatal("marshal thread/start", logger.Any(logger.FieldError, err))
	}
	logger.Info(">>> SEND", "data", string(data1))
	if err := conn.WriteMessage(websocket.TextMessage, data1); err != nil {
		logger.Fatal("write thread/start failed", logger.Any(logger.FieldError, err))
	}

	// 等待 thread/start 响应 + 可能的通知
	logger.Info("waiting 20s for thread/start response (codex spawn + health check)...")
	time.Sleep(20 * time.Second)

	// 2. 发送 turn/start (用 thread-* 的 ID)
	// 先发一个 thread/list 看看有什么
	req2 := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "thread/list",
		"params":  map[string]any{},
	}
	data2, err := json.Marshal(req2)
	if err != nil {
		logger.Fatal("marshal thread/list", logger.Any(logger.FieldError, err))
	}
	logger.Info(">>> SEND", "data", string(data2))
	if err := conn.WriteMessage(websocket.TextMessage, data2); err != nil {
		logger.Error("write thread/list failed", logger.Any(logger.FieldError, err))
	}
	time.Sleep(2 * time.Second)

	// 等用户 Ctrl+C
	logger.Info("listening for notifications... Ctrl+C to exit")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	logger.Info("bye")
}
