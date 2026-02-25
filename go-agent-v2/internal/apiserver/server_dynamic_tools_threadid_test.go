package apiserver

import "testing"

func TestResolveDynamicToolThreadIDs(t *testing.T) {
	tests := []struct {
		name         string
		agentID      string
		rawThreadID  string
		wantThreadID string
		wantCodexID  string
	}{
		{
			name:         "prefer agent thread ID when both present",
			agentID:      "thread-123",
			rawThreadID:  "019c96bd-1450-7510-800f-6270ab10f06c",
			wantThreadID: "thread-123",
			wantCodexID:  "019c96bd-1450-7510-800f-6270ab10f06c",
		},
		{
			name:         "agent only",
			agentID:      "thread-123",
			rawThreadID:  "",
			wantThreadID: "thread-123",
			wantCodexID:  "",
		},
		{
			name:         "fallback to raw thread ID when agent is empty",
			agentID:      "",
			rawThreadID:  "019c96bd-1450-7510-800f-6270ab10f06c",
			wantThreadID: "019c96bd-1450-7510-800f-6270ab10f06c",
			wantCodexID:  "",
		},
		{
			name:         "drop duplicate codex thread ID",
			agentID:      "thread-123",
			rawThreadID:  "thread-123",
			wantThreadID: "thread-123",
			wantCodexID:  "",
		},
		{
			name:         "trim whitespace",
			agentID:      " thread-123 ",
			rawThreadID:  " 019c96bd-1450-7510-800f-6270ab10f06c ",
			wantThreadID: "thread-123",
			wantCodexID:  "019c96bd-1450-7510-800f-6270ab10f06c",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			gotThreadID, gotCodexID := resolveDynamicToolThreadIDs(tt.agentID, tt.rawThreadID)
			if gotThreadID != tt.wantThreadID {
				t.Fatalf("threadID = %q, want %q", gotThreadID, tt.wantThreadID)
			}
			if gotCodexID != tt.wantCodexID {
				t.Fatalf("codexThreadID = %q, want %q", gotCodexID, tt.wantCodexID)
			}
		})
	}
}
