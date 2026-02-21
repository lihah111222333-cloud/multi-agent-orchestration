package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

type env struct {
	ID     any             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type threadStartResp struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

func send(c *websocket.Conn, id int, method string, params map[string]any) error {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	log.Printf("--> %s id=%d", method, id)
	return c.WriteMessage(websocket.TextMessage, b)
}

func waitID(c *websocket.Conn, want int, timeout time.Duration) (json.RawMessage, error) {
	deadline := time.Now().Add(timeout)
	for {
		if err := c.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		_, msg, err := c.ReadMessage()
		if err != nil {
			return nil, err
		}
		var e env
		if err := json.Unmarshal(msg, &e); err != nil {
			continue
		}
		if e.Method != "" {
			log.Printf("<-- notify %s", e.Method)
			continue
		}
		id := 0
		switch v := e.ID.(type) {
		case float64:
			id = int(v)
		}
		if id != want {
			continue
		}
		if e.Error != nil {
			return nil, fmt.Errorf("rpc error id=%d code=%d msg=%s", want, e.Error.Code, e.Error.Message)
		}
		log.Printf("<-- response id=%d ok", want)
		return e.Result, nil
	}
}

func waitCompacted(c *websocket.Conn, timeout time.Duration) (bool, error) {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := c.ReadMessage()
		if err != nil {
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				continue
			}
			return false, err
		}
		var e env
		if err := json.Unmarshal(msg, &e); err != nil {
			continue
		}
		if e.Method != "" {
			log.Printf("<-- notify %s", e.Method)
			if e.Method == "thread/compacted" {
				return true, nil
			}
		}
	}
	return false, nil
}

func main() {
	addr := "ws://127.0.0.1:4510/"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	u, _ := url.Parse(addr)
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	log.Printf("connected %s", u.String())

	if err := send(c, 1, "thread/start", map[string]any{"cwd": "."}); err != nil {
		log.Fatal(err)
	}
	res, err := waitID(c, 1, 90*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	var ts threadStartResp
	_ = json.Unmarshal(res, &ts)
	if ts.Thread.ID == "" {
		log.Fatal("empty thread id")
	}
	threadID := ts.Thread.ID
	log.Printf("thread_id=%s", threadID)

	if err := send(c, 2, "turn/start", map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{"type": "text", "text": "quick prompt before compact"}},
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitID(c, 2, 45*time.Second); err != nil {
		log.Printf("turn/start id2 err: %v", err)
	}

	if err := send(c, 3, "turn/interrupt", map[string]any{"threadId": threadID}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitID(c, 3, 45*time.Second); err != nil {
		log.Printf("turn/interrupt err: %v", err)
	}

	if err := send(c, 4, "turn/start", map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{"type": "text", "text": "/compact"}},
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitID(c, 4, 45*time.Second); err != nil {
		log.Fatal(err)
	}

	ok, err := waitCompacted(c, 90*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		log.Fatal("no thread/compacted")
	}
	log.Printf("E2E PASS: thread/compacted observed")
}
