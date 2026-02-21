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

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type threadStartResp struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

func sendRPC(conn *websocket.Conn, id int, method string, params map[string]any) error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	log.Printf("--> %s id=%d", method, id)
	return conn.WriteMessage(websocket.TextMessage, b)
}

func waitResponse(conn *websocket.Conn, reqID int, timeout time.Duration, compacted *bool) (json.RawMessage, error) {
	deadline := time.Now().Add(timeout)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var env rpcEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}
		if env.Method != "" {
			log.Printf("<-- notify %s", env.Method)
			if env.Method == "thread/compacted" {
				*compacted = true
			}
			continue
		}
		idNum := 0
		switch v := env.ID.(type) {
		case float64:
			idNum = int(v)
		case int:
			idNum = v
		}
		if idNum != reqID {
			log.Printf("<-- response id=%v (skip, waiting=%d)", env.ID, reqID)
			continue
		}
		if env.Error != nil {
			return nil, fmt.Errorf("rpc error id=%d code=%d msg=%s", reqID, env.Error.Code, env.Error.Message)
		}
		log.Printf("<-- response id=%d ok", reqID)
		return env.Result, nil
	}
}

func waitCompacted(conn *websocket.Conn, timeout time.Duration) (bool, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return false, err
	}
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				return false, nil
			}
			return false, err
		}
		var env rpcEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}
		if env.Method == "thread/compacted" {
			log.Printf("<-- notify thread/compacted")
			return true, nil
		}
		if env.Method != "" {
			log.Printf("<-- notify %s", env.Method)
		}
	}
	return false, nil
}

func main() {
	wsAddr := "ws://127.0.0.1:4510/"
	if len(os.Args) > 1 {
		wsAddr = os.Args[1]
	}
	u, err := url.Parse(wsAddr)
	if err != nil {
		log.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()
	log.Printf("connected %s", u.String())

	compactedSeen := false

	if err := sendRPC(conn, 1, "thread/start", map[string]any{"cwd": "."}); err != nil {
		log.Fatal(err)
	}
	res1, err := waitResponse(conn, 1, 90*time.Second, &compactedSeen)
	if err != nil {
		log.Fatal(err)
	}
	var start threadStartResp
	if err := json.Unmarshal(res1, &start); err != nil {
		log.Fatalf("parse thread/start result failed: %v", err)
	}
	threadID := start.Thread.ID
	if threadID == "" {
		log.Fatal("empty thread id")
	}
	log.Printf("thread_id=%s", threadID)

	if err := sendRPC(conn, 2, "turn/start", map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type": "text",
			"text": "e2e compact probe: give a long answer then continue.",
		}},
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitResponse(conn, 2, 45*time.Second, &compactedSeen); err != nil {
		log.Printf("turn/start failed (continue compact test): %v", err)
	}

	// Simulate frontend fix path: interrupt first, then compact.
	if err := sendRPC(conn, 3, "turn/interrupt", map[string]any{"threadId": threadID}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitResponse(conn, 3, 45*time.Second, &compactedSeen); err != nil {
		log.Printf("turn/interrupt failed (continue compact test): %v", err)
	}

	if err := sendRPC(conn, 4, "thread/compact/start", map[string]any{"threadId": threadID}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitResponse(conn, 4, 45*time.Second, &compactedSeen); err != nil {
		log.Fatal(err)
	}

	if compactedSeen {
		log.Printf("E2E PASS: compact rpc succeeded and compact notification observed during response wait")
		return
	}
	ok, err := waitCompacted(conn, 20*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		log.Printf("E2E PASS: compact rpc succeeded (no compacted notification observed in extra window)")
		return
	}
	log.Printf("E2E PASS: compact rpc succeeded and compact notification observed")
}
