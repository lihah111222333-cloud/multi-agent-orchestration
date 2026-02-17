// rpc-test — 直接测试 app-server JSON-RPC 方法。
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	addr := "ws://127.0.0.1:4500"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	log.Printf("connecting to %s ...", addr)
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	log.Println("connected!")

	// 后台读取所有消息 (包括通知)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("read error: %v", err)
				return
			}
			// 美化 JSON
			var pretty json.RawMessage
			if json.Unmarshal(msg, &pretty) == nil {
				out, _ := json.MarshalIndent(pretty, "", "  ")
				fmt.Printf("\n<<< RECV:\n%s\n", out)
			} else {
				fmt.Printf("\n<<< RECV: %s\n", msg)
			}
		}
	}()

	// 1. 发送 thread/start
	req1 := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "thread/start",
		"params":  map[string]any{"cwd": "."},
	}
	data1, _ := json.Marshal(req1)
	log.Printf(">>> SEND: %s", data1)
	if err := conn.WriteMessage(websocket.TextMessage, data1); err != nil {
		log.Fatalf("write thread/start: %v", err)
	}

	// 等待 thread/start 响应 + 可能的通知
	log.Println("waiting 20s for thread/start response (codex spawn + health check)...")
	time.Sleep(20 * time.Second)

	// 2. 发送 turn/start (用 thread-* 的 ID)
	// 先发一个 thread/list 看看有什么
	req2 := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "thread/list",
		"params":  map[string]any{},
	}
	data2, _ := json.Marshal(req2)
	log.Printf(">>> SEND: %s", data2)
	conn.WriteMessage(websocket.TextMessage, data2)
	time.Sleep(2 * time.Second)

	// 等用户 Ctrl+C
	log.Println("listening for notifications... Ctrl+C to exit")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	log.Println("bye")
}
