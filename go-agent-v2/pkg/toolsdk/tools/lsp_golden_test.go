package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

func TestLSPSchemasMatchGoldenSnapshot(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "tool_schemas.golden.json"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	var golden []agentcore.DynamicTool
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("unmarshal golden file: %v", err)
	}

	goldenByName := make(map[string]agentcore.DynamicTool)
	for _, tool := range golden {
		if strings.HasPrefix(tool.Name, "lsp_") {
			goldenByName[tool.Name] = tool
		}
	}

	expected := LSPTools()
	if len(expected) == 0 {
		t.Fatalf("expected LSP schemas from code")
	}

	expectedByName := make(map[string]agentcore.DynamicTool, len(expected))
	for _, tool := range expected {
		expectedByName[tool.Name] = tool
	}

	for name, tool := range expectedByName {
		goldenTool, ok := goldenByName[name]
		if !ok {
			t.Fatalf("golden snapshot missing lsp schema: %s", name)
		}
		expectedJSON, err := json.Marshal(tool)
		if err != nil {
			t.Fatalf("marshal expected schema %s: %v", name, err)
		}
		goldenJSON, err := json.Marshal(goldenTool)
		if err != nil {
			t.Fatalf("marshal golden schema %s: %v", name, err)
		}
		if string(expectedJSON) != string(goldenJSON) {
			t.Fatalf("golden schema drift for %s\nexpected: %s\ngolden:   %s", name, string(expectedJSON), string(goldenJSON))
		}
	}

	for name := range goldenByName {
		if _, ok := expectedByName[name]; !ok {
			t.Fatalf("golden snapshot contains unexpected lsp schema: %s", name)
		}
	}
}
