package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type env struct {
	ID     any             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcErr         `json:"error,omitempty"`
}

type tokenUsage struct {
	UsedTokens          float64
	ContextWindowTokens float64
	UsedPercent         float64
	LeftPercent         float64
}

func send(c *websocket.Conn, id int, method string, params map[string]any) error {
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	b, _ := json.Marshal(payload)
	log.Printf("--> %s id=%d", method, id)
	return c.WriteMessage(websocket.TextMessage, b)
}

func waitResp(c *websocket.Conn, id int, timeout time.Duration) (json.RawMessage, error) {
	if err := c.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return nil, err
		}
		var e env
		if err := json.Unmarshal(msg, &e); err != nil {
			continue
		}
		if e.Method != "" {
			if strings.HasPrefix(e.Method, "turn/") || e.Method == "thread/tokenUsage/updated" || e.Method == "thread/compacted" {
				log.Printf("<-- notify %s", e.Method)
			}
			continue
		}
		idNum := 0
		switch v := e.ID.(type) {
		case float64:
			idNum = int(v)
		case int:
			idNum = v
		}
		if idNum != id {
			continue
		}
		if e.Error != nil {
			return nil, fmt.Errorf("rpc error id=%d code=%d msg=%s", id, e.Error.Code, e.Error.Message)
		}
		log.Printf("<-- response id=%d ok", id)
		return e.Result, nil
	}
}

func getFloat(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

func readTokenUsage(c *websocket.Conn, id int, threadID string) (tokenUsage, string, error) {
	if err := send(c, id, "ui/state/get", map[string]any{"threadId": threadID}); err != nil {
		return tokenUsage{}, "", err
	}
	res, err := waitResp(c, id, 15*time.Second)
	if err != nil {
		return tokenUsage{}, "", err
	}
	var root map[string]any
	if err := json.Unmarshal(res, &root); err != nil {
		return tokenUsage{}, "", err
	}
	statuses, _ := root["statuses"].(map[string]any)
	status, _ := statuses[threadID].(string)
	usageMapRoot, _ := root["tokenUsageByThread"].(map[string]any)
	usageRaw, _ := usageMapRoot[threadID].(map[string]any)
	u := tokenUsage{}
	if usageRaw != nil {
		u.UsedTokens = getFloat(usageRaw, "usedTokens")
		u.ContextWindowTokens = getFloat(usageRaw, "contextWindowTokens")
		u.UsedPercent = getFloat(usageRaw, "usedPercent")
		u.LeftPercent = getFloat(usageRaw, "leftPercent")
		if u.LeftPercent <= 0 && u.ContextWindowTokens > 0 {
			u.LeftPercent = 100 - ((u.UsedTokens / u.ContextWindowTokens) * 100)
		}
	}
	return u, status, nil
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

	// 1) Start thread.
	if err := send(c, 1, "thread/start", map[string]any{"cwd": "."}); err != nil {
		log.Fatal(err)
	}
	res1, err := waitResp(c, 1, 90*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	var started map[string]any
	_ = json.Unmarshal(res1, &started)
	threadObj, _ := started["thread"].(map[string]any)
	threadID, _ := threadObj["id"].(string)
	if threadID == "" {
		log.Fatal("thread id empty")
	}
	log.Printf("thread_id=%s", threadID)

	// 2) Load skills + large text to consume tokens.
	largeText := strings.Repeat("请基于技能进行分析并输出详细步骤。", 1400)
	skills := []string{"Docker容器化部署", "Swagger文档规范", "GORM数据库操作"}
	if err := send(c, 2, "turn/start", map[string]any{
		"threadId":             threadID,
		"manualSkillSelection": true,
		"selectedSkills":       skills,
		"input": []map[string]any{{
			"type": "text",
			"text": largeText,
		}},
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitResp(c, 2, 30*time.Second); err != nil {
		log.Printf("turn/start warn: %v", err)
	}

	// Poll usage before compact.
	var before tokenUsage
	for i := 0; i < 20; i++ {
		u, status, err := readTokenUsage(c, 100+i, threadID)
		if err != nil {
			log.Printf("ui/state/get before warn: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		before = u
		log.Printf("before poll #%d status=%s used=%.0f limit=%.0f used%%=%.2f left%%=%.2f", i+1, status, u.UsedTokens, u.ContextWindowTokens, u.UsedPercent, u.LeftPercent)
		if u.UsedTokens > 0 && u.UsedPercent > 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// 3) Compact.
	if err := send(c, 250, "turn/interrupt", map[string]any{"threadId": threadID}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitResp(c, 250, 45*time.Second); err != nil {
		log.Printf("turn/interrupt warn: %v", err)
	}
	for i := 0; i < 10; i++ {
		u, status, err := readTokenUsage(c, 260+i, threadID)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		log.Printf("post-interrupt poll #%d status=%s used%%=%.2f left%%=%.2f", i+1, status, u.UsedPercent, u.LeftPercent)
		if strings.EqualFold(status, "idle") {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err := send(c, 300, "thread/compact/start", map[string]any{"threadId": threadID}); err != nil {
		log.Fatal(err)
	}
	if _, err := waitResp(c, 300, 45*time.Second); err != nil {
		log.Fatal(err)
	}

	// Poll usage after compact.
	var after tokenUsage
	for i := 0; i < 20; i++ {
		u, status, err := readTokenUsage(c, 400+i, threadID)
		if err != nil {
			log.Printf("ui/state/get after warn: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		after = u
		log.Printf("after poll #%d status=%s used=%.0f limit=%.0f used%%=%.2f left%%=%.2f", i+1, status, u.UsedTokens, u.ContextWindowTokens, u.UsedPercent, u.LeftPercent)
		if u.LeftPercent >= 99 {
			break
		}
		time.Sleep(2 * time.Second)
	}

	ok := after.LeftPercent >= 99
	fmt.Printf("\nRESULT before_used%%=%.2f before_left%%=%.2f after_used%%=%.2f after_left%%=%.2f pass=%v\n", before.UsedPercent, before.LeftPercent, after.UsedPercent, after.LeftPercent, ok)
	if !ok {
		os.Exit(2)
	}
}
