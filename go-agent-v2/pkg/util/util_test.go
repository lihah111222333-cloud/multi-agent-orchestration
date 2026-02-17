// util_test.go — EscapeLike / ClampInt 表驱动测试。
// Python 对应: tests/test_utils.py (escape_like + normalize_limit)。
package util

import "testing"

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"percent", "100%", `100\%`},
		{"underscore", "a_b", `a\_b`},
		{"backslash", `a\b`, `a\\b`},
		{"combined", `%_\`, `\%\_\\`},
		{"no_special", "hello", "hello"},
		{"empty", "", ""},
		{"multiple_percent", "%%", `\%\%`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeLike(tt.in)
			if got != tt.want {
				t.Errorf("EscapeLike(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestClampInt(t *testing.T) {
	tests := []struct {
		name      string
		v, lo, hi int
		want      int
	}{
		{"below_min", -1, 0, 10, 0},
		{"above_max", 20, 0, 10, 10},
		{"in_range", 5, 0, 10, 5},
		{"at_min", 0, 0, 10, 0},
		{"at_max", 10, 0, 10, 10},
		{"negative_range", -5, -10, -1, -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampInt(tt.v, tt.lo, tt.hi)
			if got != tt.want {
				t.Errorf("ClampInt(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
			}
		})
	}
}
