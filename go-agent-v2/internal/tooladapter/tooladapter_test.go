package tooladapter

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/tools"
)

type fakeRuntimeRegistry struct {
	handlers map[string]RuntimeToolHandler
}

func newFakeRuntimeRegistry() *fakeRuntimeRegistry {
	return &fakeRuntimeRegistry{handlers: make(map[string]RuntimeToolHandler)}
}

func (r *fakeRuntimeRegistry) RegisterRuntimeTool(name string, handler RuntimeToolHandler) {
	if r.handlers == nil {
		r.handlers = make(map[string]RuntimeToolHandler)
	}
	r.handlers[name] = handler
}

func (r *fakeRuntimeRegistry) LookupRuntimeTool(name string) (RuntimeToolHandler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

type fakeCounter struct {
	counts map[string]int64
}

func newFakeCounter() *fakeCounter {
	return &fakeCounter{counts: make(map[string]int64)}
}

func (c *fakeCounter) IncrementToolCall(name string) int64 {
	c.counts[name]++
	return c.counts[name]
}

type fakeSharedProviders struct{}

func (fakeSharedProviders) CodeRunner() *executor.CodeRunner { return &executor.CodeRunner{} }
func (fakeSharedProviders) AuditLogStore() *store.AuditLogStore {
	return nil
}
func (fakeSharedProviders) AwaitApproval(string, string, string, string, bool) bool { return true }
func (fakeSharedProviders) DAGStore() *store.TaskDAGStore                           { return &store.TaskDAGStore{} }
func (fakeSharedProviders) CommandCardStore() *store.CommandCardStore {
	return &store.CommandCardStore{}
}
func (fakeSharedProviders) PromptTemplateStore() *store.PromptTemplateStore {
	return &store.PromptTemplateStore{}
}
func (fakeSharedProviders) SharedFileStore() *store.SharedFileStore { return &store.SharedFileStore{} }
func (fakeSharedProviders) WorkspaceManager() *service.WorkspaceManager {
	return &service.WorkspaceManager{}
}
func (fakeSharedProviders) NotifyEvent(string, any)                               {}
func (fakeSharedProviders) Manager() *runner.AgentManager                         { return nil }
func (fakeSharedProviders) SubmitPrompt(string, string, []string, []string) error { return nil }
func (fakeSharedProviders) RememberReportRequest(string, string)                  {}
func (fakeSharedProviders) NextThreadSeq() int64                                  { return 1 }
func (fakeSharedProviders) CancelCodeRuns(string) int                             { return 0 }
func (fakeSharedProviders) SetAgentWorkDir(string, string)                        {}
func (fakeSharedProviders) ClearAgentWorkDir(string)                              {}
func (fakeSharedProviders) GetAgentWorkDir(string) string                         { return "" }

type fakeLSPProvider struct {
	hasAvailableServer bool
	bound              map[string]tools.LSPDynamicToolHandler
}

func newFakeLSPProvider(hasAvailableServer bool) *fakeLSPProvider {
	return &fakeLSPProvider{hasAvailableServer: hasAvailableServer, bound: make(map[string]tools.LSPDynamicToolHandler)}
}

func (p *fakeLSPProvider) BindDynamicTool(name string, handler tools.LSPDynamicToolHandler) {
	if p.bound == nil {
		p.bound = make(map[string]tools.LSPDynamicToolHandler)
	}
	p.bound[name] = handler
}

func (p *fakeLSPProvider) AvailabilitySummary() map[string]any {
	return map[string]any{"hasAvailableServer": p.hasAvailableServer}
}

func (p *fakeLSPProvider) DiagnosticsQuery(string) map[string]any { return map[string]any{} }
func (p *fakeLSPProvider) Hover(json.RawMessage) string           { return "hover" }
func (p *fakeLSPProvider) OpenFile(json.RawMessage) string        { return "open_file" }
func (p *fakeLSPProvider) Diagnostics(json.RawMessage) string     { return "diagnostics" }
func (p *fakeLSPProvider) Definition(json.RawMessage) string      { return "definition" }
func (p *fakeLSPProvider) References(json.RawMessage) string      { return "references" }
func (p *fakeLSPProvider) DocumentSymbol(json.RawMessage) string  { return "document_symbol" }
func (p *fakeLSPProvider) Rename(json.RawMessage) string          { return "rename" }
func (p *fakeLSPProvider) Completion(json.RawMessage) string      { return "completion" }
func (p *fakeLSPProvider) DidChange(json.RawMessage) string       { return "did_change" }
func (p *fakeLSPProvider) CodeAction(json.RawMessage) string      { return "code_action" }
func (p *fakeLSPProvider) SignatureHelp(json.RawMessage) string   { return "signature_help" }
func (p *fakeLSPProvider) Format(json.RawMessage) string          { return "format" }
func (p *fakeLSPProvider) CallHierarchy(json.RawMessage) string   { return "call_hierarchy" }
func (p *fakeLSPProvider) TypeHierarchy(json.RawMessage) string   { return "type_hierarchy" }
func (p *fakeLSPProvider) SemanticTokens(json.RawMessage) string  { return "semantic_tokens" }
func (p *fakeLSPProvider) FoldingRange(json.RawMessage) string    { return "folding_range" }
func (p *fakeLSPProvider) WorkspaceSymbol(json.RawMessage) string { return "workspace_symbol" }
func (p *fakeLSPProvider) Implementation(json.RawMessage) string  { return "implementation" }
func (p *fakeLSPProvider) TypeDefinition(json.RawMessage) string  { return "type_definition" }

func testProviders(hasAvailableServer bool) Providers {
	deps := fakeSharedProviders{}
	return Providers{
		LSP:           newFakeLSPProvider(hasAvailableServer),
		CodeRun:       deps,
		Approvals:     deps,
		Resource:      deps,
		Orchestration: deps,
		AgentRuntime:  deps,
	}
}

func TestRegisterAndAllSchemasAlign(t *testing.T) {
	resetExtendedLSPDynamicToolProvidersForTest()
	t.Cleanup(resetExtendedLSPDynamicToolProvidersForTest)

	registry := newFakeRuntimeRegistry()
	providers := testProviders(true)

	Register(registry, providers)
	schemas := AllSchemas(providers)

	if len(registry.handlers) == 0 {
		t.Fatalf("expected registered handlers")
	}
	if len(schemas) == 0 {
		t.Fatalf("expected schemas")
	}

	handlerNames := make([]string, 0, len(registry.handlers))
	for name := range registry.handlers {
		handlerNames = append(handlerNames, name)
	}
	sort.Strings(handlerNames)

	schemaSet := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		schemaSet[schema.Name] = struct{}{}
	}

	for _, name := range handlerNames {
		if _, ok := schemaSet[name]; !ok {
			t.Fatalf("registered handler %q missing in schemas", name)
		}
	}

	for _, required := range []string{
		"code_run",
		"code_run_test",
		"task_create_dag",
		"orchestration_launch_agent",
		"lsp_hover",
		"lsp_code_action",
	} {
		if _, ok := registry.handlers[required]; !ok {
			t.Fatalf("expected handler %q to be registered", required)
		}
	}
}

func TestDispatchBuildsToolCallContextAndCounts(t *testing.T) {
	registry := newFakeRuntimeRegistry()
	counter := newFakeCounter()
	requestID := int64(42)
	baseCtx := context.WithValue(context.Background(), "key", "value")

	var gotCtx tools.ToolCallContext
	registry.RegisterRuntimeTool("probe", func(ctx tools.ToolCallContext, args json.RawMessage) string {
		gotCtx = ctx
		if string(args) != `{"ok":true}` {
			t.Fatalf("unexpected args: %s", string(args))
		}
		return "done"
	})

	result, err := Dispatch(DynamicToolCall{
		AgentID:   "agent-1",
		Tool:      "probe",
		CallID:    "call-1",
		RequestID: &requestID,
		Arguments: json.RawMessage(`{"ok":true}`),
		Ctx:       baseCtx,
	}, Providers{Lookup: registry, Counter: counter})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if result != "done" {
		t.Fatalf("unexpected result: %q", result)
	}
	if gotCtx.AgentID != "agent-1" || gotCtx.CallID != "call-1" || gotCtx.RequestID != &requestID {
		t.Fatalf("unexpected context: %+v", gotCtx)
	}
	if gotCtx.Ctx != baseCtx {
		t.Fatalf("expected dispatch to pass through context")
	}
	if counter.counts["probe"] != 1 {
		t.Fatalf("expected probe count = 1, got %d", counter.counts["probe"])
	}
}

func TestDispatchUnknownToolStillCountsAndReturnsError(t *testing.T) {
	registry := newFakeRuntimeRegistry()
	counter := newFakeCounter()

	result, err := Dispatch(DynamicToolCall{Tool: "missing"}, Providers{Lookup: registry, Counter: counter})
	if err == nil {
		t.Fatalf("expected dispatch error")
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("unexpected error: %v", err)
	}
	if counter.counts["missing"] != 1 {
		t.Fatalf("expected missing count = 1, got %d", counter.counts["missing"])
	}
}

func TestBuildToolCallContextDefaultsContext(t *testing.T) {
	callCtx := BuildToolCallContext(DynamicToolCall{AgentID: "agent-x", CallID: "call-x"})
	if callCtx.AgentID != "agent-x" || callCtx.CallID != "call-x" {
		t.Fatalf("unexpected context identity: %+v", callCtx)
	}
	if callCtx.Ctx == nil {
		t.Fatalf("expected non-nil context")
	}
}

func TestRegisterIncludesCustomExtendedLSPProviders(t *testing.T) {
	resetExtendedLSPDynamicToolProvidersForTest()
	t.Cleanup(resetExtendedLSPDynamicToolProvidersForTest)

	RegisterExtendedLSPDynamicToolProvider(
		"custom_probe",
		func(provider tools.LSPProvider) {
			provider.BindDynamicTool("lsp_custom_probe", func(_ json.RawMessage) string { return "custom" })
		},
		func() []agentcore.DynamicTool {
			return []agentcore.DynamicTool{{
				Name:        "lsp_custom_probe",
				Description: "custom",
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			}}
		},
	)

	providers := testProviders(true)
	registry := newFakeRuntimeRegistry()
	Register(registry, providers)

	if _, ok := registry.LookupRuntimeTool("lsp_custom_probe"); !ok {
		t.Fatalf("custom lsp handler not registered")
	}

	schemas := AllSchemas(providers)
	found := false
	for _, schema := range schemas {
		if schema.Name == "lsp_custom_probe" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("custom lsp schema missing")
	}
}

func TestLSPAvailabilityTransitionKeepsRuntimeHandlers(t *testing.T) {
	resetExtendedLSPDynamicToolProvidersForTest()
	t.Cleanup(resetExtendedLSPDynamicToolProvidersForTest)

	deps := fakeSharedProviders{}
	lspProvider := newFakeLSPProvider(false)
	providers := Providers{
		LSP:           lspProvider,
		CodeRun:       deps,
		Approvals:     deps,
		Resource:      deps,
		Orchestration: deps,
		AgentRuntime:  deps,
	}

	registry := newFakeRuntimeRegistry()
	Register(registry, providers)

	if _, ok := registry.LookupRuntimeTool("lsp_hover"); !ok {
		t.Fatalf("expected lsp_hover runtime handler to be registered even when unavailable")
	}

	initialSchemas := AllSchemas(providers)
	for _, schema := range initialSchemas {
		if schema.Name == "lsp_hover" {
			t.Fatalf("lsp_hover should not be exposed in schemas when unavailable")
		}
	}

	lspProvider.hasAvailableServer = true

	updatedSchemas := AllSchemas(providers)
	found := false
	for _, schema := range updatedSchemas {
		if schema.Name == "lsp_hover" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected lsp_hover schema once availability becomes true")
	}
	if _, ok := registry.LookupRuntimeTool("lsp_hover"); !ok {
		t.Fatalf("lsp_hover runtime handler should remain registered after availability transition")
	}
}
