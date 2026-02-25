package codexadapter

import "testing"

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
		{name: "exceeds_max", initialCount: 100, total: 99999, want: threadMessageHydrationMaxRecords},
		{name: "negative_initial", initialCount: -5, total: 10, want: 10},
		{name: "negative_initial_exceeds", initialCount: -5, total: 99999, want: threadMessageHydrationMaxRecords},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateHydrationLoadLimit(tt.initialCount, tt.total)
			if got != tt.want {
				t.Errorf("calculateHydrationLoadLimit(%d, %d) = %d, want %d",
					tt.initialCount, tt.total, got, tt.want)
			}
		})
	}
}
