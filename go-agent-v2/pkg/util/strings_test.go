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

func TestIsSystemNoiseText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "agents", text: "# AGENTS.md\ncontent", want: true},
		{name: "environment", text: "<environment_context>ctx</environment_context>", want: true},
		{name: "instructions", text: "<INSTRUCTIONS>rule</INSTRUCTIONS>", want: true},
		{name: "permissions", text: "<permissions instructions>x</permissions instructions>", want: true},
		{name: "normal", text: "请帮我修复 bug", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSystemNoiseText(tt.text); got != tt.want {
				t.Fatalf("IsSystemNoiseText(%q)=%v, want=%v", tt.text, got, tt.want)
			}
		})
	}
}

func TestStripLeadingSystemNoise(t *testing.T) {
	input := `# AGENTS.md instructions for /repo
<INSTRUCTIONS>
rule
</INSTRUCTIONS>
<environment_context>
ctx
</environment_context>
真实问题`
	got := StripLeadingSystemNoise(input)
	if got != "真实问题" {
		t.Fatalf("StripLeadingSystemNoise=%q, want=%q", got, "真实问题")
	}
}

func TestStripLeadingSystemNoise_NoiseOnly(t *testing.T) {
	input := `<permissions instructions>
only-noise
</permissions instructions>`
	got := StripLeadingSystemNoise(input)
	if got != "" {
		t.Fatalf("StripLeadingSystemNoise=%q, want empty", got)
	}
}

func TestStripLeadingSystemNoise_ManualTextKeepsOriginal(t *testing.T) {
	input := "请解释 # AGENTS.md 是什么"
	got := StripLeadingSystemNoise(input)
	if got != input {
		t.Fatalf("StripLeadingSystemNoise=%q, want original", got)
	}
}
