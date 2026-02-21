package store

import "testing"

// ========================================
// Bug 2 (TDD): classifyAILog 分类正确性
// ========================================

func TestClassifyAILog_AllCategories(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"api_request POST", "API Request POST https://api.openai.com/v1/chat/completions", "api_request"},
		{"api_request request_to", "HTTP request to api", "api_request"},
		{"api_error", "api error: rate limit exceeded", "api_error"},
		{"api_error underscore", "api_error: timeout", "api_error"},
		{"compat_fallback", "compat mode fallback", "compat_fallback"},
		{"compat_chinese", "模型兼容层切换", "compat_fallback"},
		{"runtime_config", "runtime config updated", "runtime_config"},
		{"error generic", "unhandled error occurred", "error"},
		{"error exception", "fatal exception in processing", "error"},
		{"ai_event default", "model loaded successfully", "ai_event"},
		{"ai_event empty", "", "ai_event"},
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

// ========================================
// extractHTTP 正则提取
// ========================================

func TestExtractHTTP(t *testing.T) {
	tests := []struct {
		name         string
		msg          string
		wantMethod   string
		wantURL      string
		wantEndpoint string
	}{
		{
			"full url",
			"POST https://api.openai.com/v1/chat/completions",
			"POST", "https://api.openai.com/v1/chat/completions", "/v1/chat/completions",
		},
		{
			"get request",
			"GET http://localhost:8080/health",
			"GET", "http://localhost:8080/health", "/health",
		},
		{"no match", "no http here", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, url, endpoint := extractHTTP(tt.msg)
			if method != tt.wantMethod || url != tt.wantURL || endpoint != tt.wantEndpoint {
				t.Errorf("extractHTTP(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.msg, method, url, endpoint, tt.wantMethod, tt.wantURL, tt.wantEndpoint)
			}
		})
	}
}

// ========================================
// extractStatus 状态码提取
// ========================================

func TestExtractStatus(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		wantCode string
		wantText string
	}{
		{"200 OK", "HTTP/1.1 200 OK", "200", "OK"},
		{"404 NotFound", "HTTP/2.0 404 NotFound", "404", "NotFound"},
		{"no match", "status unknown", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, text := extractStatus(tt.msg)
			if code != tt.wantCode || text != tt.wantText {
				t.Errorf("extractStatus(%q) = (%q, %q), want (%q, %q)",
					tt.msg, code, text, tt.wantCode, tt.wantText)
			}
		})
	}
}

// ========================================
// extractModel 模型名提取
// ========================================

func TestExtractModel(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"model=gpt-4o", "using model=gpt-4o for inference", "gpt-4o"},
		{"model: claude-3", "model: claude-3-sonnet", "claude-3-sonnet"},
		{"no match", "no model mentioned", ""},
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
