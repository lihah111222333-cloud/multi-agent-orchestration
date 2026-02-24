package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

type schemaStubProvider struct{}

func (schemaStubProvider) CodeRunner() *executor.CodeRunner { return &executor.CodeRunner{} }
func (schemaStubProvider) AuditLogStore() *store.AuditLogStore {
	return nil
}
func (schemaStubProvider) AwaitApproval(string, string, string, string, bool) bool { return true }
func (schemaStubProvider) DAGStore() *store.TaskDAGStore                           { return &store.TaskDAGStore{} }
func (schemaStubProvider) CommandCardStore() *store.CommandCardStore {
	return &store.CommandCardStore{}
}
func (schemaStubProvider) PromptTemplateStore() *store.PromptTemplateStore {
	return &store.PromptTemplateStore{}
}
func (schemaStubProvider) SharedFileStore() *store.SharedFileStore { return &store.SharedFileStore{} }
func (schemaStubProvider) WorkspaceManager() *service.WorkspaceManager {
	return &service.WorkspaceManager{}
}
func (schemaStubProvider) NotifyEvent(string, any)                               {}
func (schemaStubProvider) Manager() *runner.AgentManager                         { return nil }
func (schemaStubProvider) SubmitPrompt(string, string, []string, []string) error { return nil }
func (schemaStubProvider) RememberReportRequest(string, string)                  {}
func (schemaStubProvider) NextThreadSeq() int64                                  { return 1 }
func (schemaStubProvider) CancelCodeRuns(string) int                             { return 0 }
func (schemaStubProvider) SetAgentWorkDir(string, string)                        {}
func (schemaStubProvider) ClearAgentWorkDir(string)                              {}
func (schemaStubProvider) GetAgentWorkDir(string) string                         { return "" }
func (schemaStubProvider) AllSchemas() []agentcore.DynamicTool                   { return nil }

func TestDynamicToolSchemasStable(t *testing.T) {
	provider := schemaStubProvider{}

	var schemas []agentcore.DynamicTool
	schemas = append(schemas, LSPTools()...)
	for _, ext := range LSPExtTools() {
		schemas = append(schemas, ext.Build()...)
	}
	schemas = append(schemas, Schemas(OrchestrationTools(provider, provider, provider))...)
	schemas = append(schemas, Schemas(ResourceTools(provider))...)
	schemas = append(schemas, Schemas(CodeRunTools(provider, provider, provider))...)

	sort.SliceStable(schemas, func(i, j int) bool {
		return schemas[i].Name < schemas[j].Name
	})

	goldenPath := filepath.Join("testdata", "tool_schemas.golden.json")
	got, err := json.MarshalIndent(schemas, "", "  ")
	if err != nil {
		t.Fatalf("marshal schemas: %v", err)
	}
	got = append(got, '\n')

	if os.Getenv("UPDATE_TOOL_SCHEMAS_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("tool schema golden mismatch\nwant:\n%s\n\ngot:\n%s", string(want), string(got))
	}
}
