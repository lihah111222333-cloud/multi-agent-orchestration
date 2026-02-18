// server_pure_functions_test.go — 重构前行为保持测试 (characterization tests)。
//
// 覆盖 server.go 中所有待重构的纯函数。
// TDD: 先 GREEN → 重构 → 重跑 GREEN = 行为一致。
package apiserver

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

// ========================================
// mergePayloadFromMap
// ========================================

func TestMergePayloadFromMap_ExtractsKnownKeys(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		wantKey string
		wantVal any
	}{
		{"delta", map[string]any{"delta": "hello"}, "delta", "hello"},
		{"content", map[string]any{"content": "world"}, "content", "world"},
		{"exit_code", map[string]any{"exit_code": 1}, "exit_code", 1},
		{"tool_name", map[string]any{"tool_name": "lsp_hover"}, "tool_name", "lsp_hover"},
		{"file_path_direct", map[string]any{"file_path": "/a/b.go"}, "file_path", "/a/b.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{}
			mergePayloadFromMap(payload, tt.data)
			if payload[tt.wantKey] != tt.wantVal {
				t.Errorf("payload[%q] = %v, want %v", tt.wantKey, payload[tt.wantKey], tt.wantVal)
			}
		})
	}
}

func TestMergePayloadFromMap_NilData(t *testing.T) {
	payload := map[string]any{"existing": true}
	mergePayloadFromMap(payload, nil)
	if len(payload) != 1 {
		t.Errorf("payload modified on nil data: %v", payload)
	}
}

func TestMergePayloadFromMap_CallIDAlias(t *testing.T) {
	payload := map[string]any{}
	mergePayloadFromMap(payload, map[string]any{"call_id": "abc123"})
	if payload["id"] != "abc123" {
		t.Errorf("call_id → id alias: got %v", payload["id"])
	}
}

func TestMergePayloadFromMap_ItemIDAlias(t *testing.T) {
	payload := map[string]any{}
	mergePayloadFromMap(payload, map[string]any{"item_id": "item-1"})
	if payload["id"] != "item-1" {
		t.Errorf("item_id → id alias: got %v", payload["id"])
	}
}

func TestMergePayloadFromMap_FilePathAlias(t *testing.T) {
	payload := map[string]any{}
	mergePayloadFromMap(payload, map[string]any{"file_path": "/foo/bar.go"})
	if payload["file"] != "/foo/bar.go" {
		t.Errorf("file_path → file alias: got %v", payload["file"])
	}
}

func TestMergePayloadFromMap_PathAlias(t *testing.T) {
	payload := map[string]any{}
	mergePayloadFromMap(payload, map[string]any{"path": "/baz.go"})
	if payload["file"] != "/baz.go" {
		t.Errorf("path → file alias: got %v", payload["file"])
	}
}

func TestMergePayloadFromMap_ExistingIDNotOverwritten(t *testing.T) {
	payload := map[string]any{"id": "existing"}
	mergePayloadFromMap(payload, map[string]any{"call_id": "new-id"})
	if payload["id"] != "existing" {
		t.Errorf("existing id was overwritten: got %v", payload["id"])
	}
}

func TestMergePayloadFromMap_ExistingFileNotOverwritten(t *testing.T) {
	payload := map[string]any{"file": "/existing.go"}
	mergePayloadFromMap(payload, map[string]any{"file_path": "/new.go"})
	if payload["file"] != "/existing.go" {
		t.Errorf("existing file was overwritten: got %v", payload["file"])
	}
}

// ========================================
// mergePayloadFields
// ========================================

func TestMergePayloadFields_TopLevel(t *testing.T) {
	payload := map[string]any{}
	raw := json.RawMessage(`{"delta":"hello","content":"world"}`)
	mergePayloadFields(payload, raw)

	if payload["delta"] != "hello" {
		t.Errorf("delta = %v, want %q", payload["delta"], "hello")
	}
	if payload["content"] != "world" {
		t.Errorf("content = %v, want %q", payload["content"], "world")
	}
}

func TestMergePayloadFields_NestedMap(t *testing.T) {
	payload := map[string]any{}
	raw := json.RawMessage(`{"msg":{"delta":"nested-delta"}}`)
	mergePayloadFields(payload, raw)

	if payload["delta"] != "nested-delta" {
		t.Errorf("nested delta = %v, want %q", payload["delta"], "nested-delta")
	}
}

func TestMergePayloadFields_NestedString(t *testing.T) {
	payload := map[string]any{}
	raw := json.RawMessage(`{"data":"{\"content\":\"from-string\"}"}`)
	mergePayloadFields(payload, raw)

	if payload["content"] != "from-string" {
		t.Errorf("nested string content = %v, want %q", payload["content"], "from-string")
	}
}

func TestMergePayloadFields_EmptyRaw(t *testing.T) {
	payload := map[string]any{"keep": true}
	mergePayloadFields(payload, nil)
	if len(payload) != 1 || payload["keep"] != true {
		t.Errorf("payload modified on nil raw: %v", payload)
	}
}

func TestMergePayloadFields_InvalidJSON(t *testing.T) {
	payload := map[string]any{}
	mergePayloadFields(payload, json.RawMessage(`{invalid`))
	if len(payload) != 0 {
		t.Errorf("payload modified on invalid JSON: %v", payload)
	}
}

// ========================================
// extractEventContent
// ========================================

func TestExtractEventContent(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"delta", `{"delta":"hello world"}`, "hello world"},
		{"content", `{"content":"abc"}`, "abc"},
		{"message", `{"message":"msg text"}`, "msg text"},
		{"command", `{"command":"ls -la"}`, "ls -la"},
		{"text", `{"text":"some text"}`, "some text"},
		{"output", `{"output":"out data"}`, "out data"},
		{"diff", `{"diff":"--- a/f\n+++ b/f"}`, "--- a/f\n+++ b/f"},
		{"empty_obj", `{}`, ""},
		{"empty_data", ``, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := codex.Event{Data: json.RawMessage(tt.data)}
			got := extractEventContent(event)
			if got != tt.want {
				t.Errorf("extractEventContent(%q) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
}

func TestExtractEventContent_NestedMsg(t *testing.T) {
	event := codex.Event{Data: json.RawMessage(`{"msg":{"delta":"nested"}}`)}
	got := extractEventContent(event)
	if got != "nested" {
		t.Errorf("nested msg.delta = %q, want %q", got, "nested")
	}
}

func TestExtractEventContent_NestedStringData(t *testing.T) {
	event := codex.Event{Data: json.RawMessage(`{"data":"{\"content\":\"from-str\"}"}`)}
	got := extractEventContent(event)
	if got != "from-str" {
		t.Errorf("nested string data.content = %q, want %q", got, "from-str")
	}
}

// ========================================
// classifyEventRole
// ========================================

func TestClassifyEventRole(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
	}{
		// assistant
		{"agent_message_delta", "assistant"},
		{"agent_message", "assistant"},
		{"agentMessage", "assistant"},
		{"agent_reasoning_delta", "assistant"},
		{"reasoning_delta", "assistant"},
		// tool
		{"exec_command_begin", "tool"},
		{"exec_command_end", "tool"},
		{"patch_apply", "tool"},
		{"mcp_server_update", "tool"},
		{"dynamic_tool_call", "tool"},
		{"commandExecution/output", "tool"},
		{"fileChange/started", "tool"},
		{"dynamicTool/call", "tool"},
		{"tool/call/result", "tool"},
		// user
		{"turn_started", "user"},
		{"turn/started", "user"},
		{"user_message", "user"},
		{"item/usermessage", "user"},
		// system (default)
		{"session_configured", "system"},
		{"idle", "system"},
		{"unknown_event", "system"},
		{"token_count", "system"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := classifyEventRole(tt.eventType)
			if got != tt.want {
				t.Errorf("classifyEventRole(%q) = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}

// ========================================
// normalizeFiles
// ========================================

func TestNormalizeFiles(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want []string
	}{
		{"nil", nil, nil},
		{"string", "file.go", []string{"file.go"}},
		{"empty_string", "  ", nil},
		{"string_slice", []string{"a.go", "b.go", "a.go"}, []string{"a.go", "b.go"}},
		{"any_slice", []any{"x.go", "y.go"}, []string{"x.go", "y.go"}},
		{"any_slice_with_empty", []any{"x.go", "", " ", "y.go"}, []string{"x.go", "y.go"}},
		{"int_returns_nil", 42, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeFiles(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("normalizeFiles(%v) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// ========================================
// uniqueStrings
// ========================================

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		want  []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, nil},
		{"no_dups", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"with_dups", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{"whitespace", []string{"a", " ", "", "b"}, []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uniqueStrings(tt.items)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("uniqueStrings(%v) = %v, want %v", tt.items, got, tt.want)
			}
		})
	}
}

// ========================================
// parseFilesFromPatchDelta
// ========================================

func TestParseFilesFromPatchDelta(t *testing.T) {
	tests := []struct {
		name  string
		delta string
		want  []string
	}{
		{"empty", "", nil},
		{
			"diff_format",
			"diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n",
			[]string{"foo.go"},
		},
		{
			"status_format",
			"M main.go\nA new.go\nD old.go\n",
			[]string{"main.go", "new.go", "old.go"},
		},
		{
			"dedup",
			"diff --git a/x.go b/x.go\nM x.go\n",
			[]string{"x.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFilesFromPatchDelta(tt.delta)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseFilesFromPatchDelta() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ========================================
// firstNonEmpty
// ========================================

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  string
	}{
		{"normal", []string{"", "  ", "file.go", "other.go"}, "file.go"},
		{"all_empty", []string{"", " ", "  "}, ""},
		{"empty_slice", []string{}, ""},
		{"nil", nil, ""},
		{"first_nonempty", []string{"a.go"}, "a.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstNonEmpty(tt.files)
			if got != tt.want {
				t.Errorf("firstNonEmpty(%v) = %q, want %q", tt.files, got, tt.want)
			}
		})
	}
}

// ========================================
// toolResultSuccess
// ========================================

func TestToolResultSuccess(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"success", "some output", true},
		{"error_prefix", "error: something failed", false},
		{"Error_mixed_case", "Error: Something", false},
		{"failed_prefix", "failed to connect", false},
		{"unknown_tool", "unknown tool: xyz", false},
		{"json_error", `{"error":"bad request"}`, false},
		{"json_error_field", `{"result":"fail","error":"oops"}`, false},
		{"normal_output", "file created successfully", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolResultSuccess(tt.result)
			if got != tt.want {
				t.Errorf("toolResultSuccess(%q) = %v, want %v", tt.result, got, tt.want)
			}
		})
	}
}

// ========================================
// parseIntID
// ========================================

func TestParseIntID(t *testing.T) {
	tests := []struct {
		name   string
		raw    json.RawMessage
		want   int64
		wantOK bool
	}{
		{"positive", json.RawMessage(`123`), 123, true},
		{"zero", json.RawMessage(`0`), 0, true},
		{"negative", json.RawMessage(`-42`), -42, true},
		{"large", json.RawMessage(`999999`), 999999, true},
		{"null", json.RawMessage(`null`), 0, false},
		{"empty", json.RawMessage(``), 0, false},
		{"float", json.RawMessage(`1.5`), 0, false},
		{"string", json.RawMessage(`"abc"`), 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseIntID(tt.raw)
			if ok != tt.wantOK {
				t.Errorf("parseIntID(%s) ok = %v, want %v", string(tt.raw), ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("parseIntID(%s) = %d, want %d", string(tt.raw), got, tt.want)
			}
		})
	}
}

// ========================================
// rawIDtoAny
// ========================================

func TestRawIDtoAny(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want any
	}{
		{"integer", json.RawMessage(`42`), int64(42)},
		{"null", json.RawMessage(`null`), nil},
		{"empty", json.RawMessage(``), nil},
		{"string", json.RawMessage(`"abc"`), "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rawIDtoAny(tt.raw)
			if got != tt.want {
				t.Errorf("rawIDtoAny(%s) = %v (%T), want %v (%T)", string(tt.raw), got, got, tt.want, tt.want)
			}
		})
	}
}

// ========================================
// checkLocalOrigin
// ========================================

func TestCheckLocalOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"no_origin", "", true},
		{"http_localhost", "http://localhost:3000", true},
		{"https_localhost", "https://localhost", true},
		{"http_127", "http://127.0.0.1:4500", true},
		{"https_127", "https://127.0.0.1", true},
		{"http_ipv6", "http://[::1]:8080", true},
		{"wails", "wails://wails", true},
		{"remote", "http://evil.com", false},
		{"https_remote", "https://attacker.io", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			got := checkLocalOrigin(req)
			if got != tt.want {
				t.Errorf("checkLocalOrigin(origin=%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}
