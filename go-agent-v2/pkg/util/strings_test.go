package util

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{"all empty", []string{"", "  ", "\t"}, ""},
		{"first non-empty", []string{"hello", "world"}, "hello"},
		{"skip blanks", []string{"", "  ", "found"}, "found"},
		{"single value", []string{"only"}, "only"},
		{"no args", nil, ""},
		{"trims whitespace", []string{"  trimmed  "}, "trimmed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FirstNonEmpty(tt.input...)
			if got != tt.want {
				t.Errorf("FirstNonEmpty(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
