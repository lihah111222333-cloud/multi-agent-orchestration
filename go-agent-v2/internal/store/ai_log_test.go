// ai_log_test.go — AI 日志分类 + regex 提取的纯逻辑测试。
// Python 对应: test_ai_log.py → test_query_ai_logs_and_filters (分类+endpoint+status_code)。
package store

import "testing"

func TestClassifyAILog(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"api_request_post", `HTTP Request: POST https://api.openai.com/v1/responses "HTTP/1.1 200 OK"`, "api_request"},
		{"api_request_to", "request to https://api.com", "api_request"},
		{"api_error", "api error 500: internal", "api_error"},
		{"api_error_underscore", "api_error: rate limit exceeded", "api_error"},
		{"compat_fallback", "[主控 Gateway] 已临时关闭 use_previous_response_id 以兼容当前网关", "compat_fallback"},
		{"runtime_config", "runtime config changed for agent_01", "runtime_config"},
		{"error_generic", "exception occurred in worker", "error"},
		{"ai_event_default", "unrelated worker message", "ai_event"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAILog(tt.msg)
			if got != tt.want {
				t.Errorf("classifyAILog(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestExtractHTTP(t *testing.T) {
	tests := []struct {
		name                        string
		msg                         string
		wantMethod, wantURL, wantEP string
	}{
		{
			"post_openai",
			`HTTP Request: POST https://api.openai.com/v1/chat/completions "HTTP/1.1 200 OK"`,
			"POST", "https://api.openai.com/v1/chat/completions", "/v1/chat/completions",
		},
		{
			"get_url",
			"GET https://example.com/api/v2/status",
			"GET", "https://example.com/api/v2/status", "/api/v2/status",
		},
		{"no_match", "hello world", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, url, ep := extractHTTP(tt.msg)
			if method != tt.wantMethod {
				t.Errorf("method = %q, want %q", method, tt.wantMethod)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if ep != tt.wantEP {
				t.Errorf("endpoint = %q, want %q", ep, tt.wantEP)
			}
		})
	}
}

func TestExtractStatus(t *testing.T) {
	tests := []struct {
		name               string
		msg                string
		wantCode, wantText string
	}{
		{"200_OK", `HTTP/1.1 200 OK`, "200", "OK"},
		{"404_alone", `HTTP/1.1 404`, "404", ""},
		{"no_match", "hello world", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, text := extractStatus(tt.msg)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if text != tt.wantText {
				t.Errorf("text = %q, want %q", text, tt.wantText)
			}
		})
	}
}

func TestExtractModel(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"equals_sign", "model=gpt-4o", "gpt-4o"},
		{"colon", "model: gpt-4o-mini", "gpt-4o-mini"},
		{"no_match", "hello world", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractModel(tt.msg)
			if got != tt.want {
				t.Errorf("extractModel(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}
