package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestEncodeFrame(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`)
	frame := EncodeFrame(body)

	want := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	if string(frame) != want {
		t.Errorf("EncodeFrame:\ngot  %q\nwant %q", string(frame), want)
	}
}

func TestReadFrame(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"result":null}`)
	frame := EncodeFrame(body)

	reader := bufio.NewReader(bytes.NewReader(frame))
	c := &Client{stdout: reader}

	got, err := c.readFrame()
	if err != nil {
		t.Fatalf("readFrame error: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("readFrame:\ngot  %q\nwant %q", string(got), string(body))
	}
}

func TestReadFrameMultiple(t *testing.T) {
	msg1 := []byte(`{"jsonrpc":"2.0","id":1}`)
	msg2 := []byte(`{"jsonrpc":"2.0","id":2}`)
	var buf bytes.Buffer
	buf.Write(EncodeFrame(msg1))
	buf.Write(EncodeFrame(msg2))

	reader := bufio.NewReader(&buf)
	c := &Client{stdout: reader}

	got1, err := c.readFrame()
	if err != nil {
		t.Fatalf("first readFrame: %v", err)
	}
	got2, err := c.readFrame()
	if err != nil {
		t.Fatalf("second readFrame: %v", err)
	}

	if !bytes.Equal(got1, msg1) {
		t.Errorf("frame1: got %q, want %q", got1, msg1)
	}
	if !bytes.Equal(got2, msg2) {
		t.Errorf("frame2: got %q, want %q", got2, msg2)
	}
}

func TestPathToURI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"file:///tmp/test.go", "file:///tmp/test.go"},
		{"/tmp/test.go", "file:///tmp/test.go"},
	}
	for _, tc := range tests {
		got := pathToURI(tc.input)
		if got != tc.want {
			t.Errorf("pathToURI(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDiagnosticSeverityString(t *testing.T) {
	tests := []struct {
		s    DiagnosticSeverity
		want string
	}{
		{SeverityError, "error"},
		{SeverityWarning, "warning"},
		{SeverityInformation, "info"},
		{SeverityHint, "hint"},
		{DiagnosticSeverity(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("DiagnosticSeverity(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestProtocolTypes(t *testing.T) {
	// 验证 InitializeParams 序列化正确
	params := InitializeParams{
		ProcessID: 1234,
		RootURI:   "file:///tmp/project",
		Capabilities: ClientCapabilities{
			TextDocument: &TextDocumentClientCapabilities{
				Hover: &HoverCapability{
					ContentFormat: []string{"markdown"},
				},
			},
		},
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal InitializeParams: %v", err)
	}

	var decoded InitializeParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal InitializeParams: %v", err)
	}
	if decoded.ProcessID != 1234 {
		t.Errorf("ProcessID = %d, want 1234", decoded.ProcessID)
	}
	if decoded.RootURI != "file:///tmp/project" {
		t.Errorf("RootURI = %q, want file:///tmp/project", decoded.RootURI)
	}
}

func TestDefaultServersConfig(t *testing.T) {
	if len(DefaultServers) != 3 {
		t.Fatalf("want 3 DefaultServers, got %d", len(DefaultServers))
	}

	langs := map[string]bool{}
	for _, s := range DefaultServers {
		langs[s.Language] = true
	}
	for _, want := range []string{"go", "rust", "typescript"} {
		if !langs[want] {
			t.Errorf("missing language %q in DefaultServers", want)
		}
	}
}
