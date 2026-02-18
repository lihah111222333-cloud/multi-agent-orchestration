package codex

import (
	"encoding/json"
	"testing"
)

func TestParseThreadResumeResult(t *testing.T) {
	tests := []struct {
		name       string
		raw        json.RawMessage
		fallbackID string
		want       string
		wantErr    bool
	}{
		{
			name:       "uses thread id from response",
			raw:        json.RawMessage(`{"thread":{"id":"thread-resumed"}}`),
			fallbackID: "thread-old",
			want:       "thread-resumed",
		},
		{
			name:       "falls back to request id when response empty",
			raw:        json.RawMessage(`{}`),
			fallbackID: "thread-old",
			want:       "thread-old",
		},
		{
			name:    "errors when both response and fallback empty",
			raw:     json.RawMessage(`{}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseThreadResumeResult(tt.raw, tt.fallbackID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseThreadResumeResult() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Fatalf("parseThreadResumeResult() = %q, want %q", got, tt.want)
			}
		})
	}
}
