package codexadapter

import (
	"testing"

	messagessvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/messages"
)

func TestCalculateHydrationLoadLimit(t *testing.T) {
	tests := []struct {
		name         string
		initialCount int
		total        int64
		want         int
	}{
		{name: "zero_both", initialCount: 0, total: 0, want: 0},
		{name: "initial_bigger", initialCount: 100, total: 50, want: 100},
		{name: "total_bigger", initialCount: 50, total: 200, want: 200},
		{name: "exceeds_max", initialCount: 100, total: 99999, want: messagessvc.ThreadMessageHydrationMaxRecords},
		{name: "negative_initial", initialCount: -5, total: 10, want: 10},
		{name: "negative_initial_exceeds", initialCount: -5, total: 99999, want: messagessvc.ThreadMessageHydrationMaxRecords},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := messagessvc.CalculateHydrationLoadLimit(tt.initialCount, tt.total)
			if got != tt.want {
				t.Errorf("calculateHydrationLoadLimit(%d, %d) = %d, want %d",
					tt.initialCount, tt.total, got, tt.want)
			}
		})
	}
}
