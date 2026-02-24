package tooladapter

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/tools"
)

// RuntimeToolHandler is the unified dynamic tool runtime handler signature.
type RuntimeToolHandler = func(ctx tools.ToolCallContext, args json.RawMessage) string

// RuntimeRegistry registers dynamic tool handlers for runtime dispatch.
type RuntimeRegistry interface {
	RegisterRuntimeTool(name string, handler RuntimeToolHandler)
}

// RuntimeLookup resolves runtime handlers for dispatch.
type RuntimeLookup interface {
	LookupRuntimeTool(name string) (RuntimeToolHandler, bool)
}

// SetRuntimeTool writes a runtime handler into map storage.
func SetRuntimeTool(dst map[string]RuntimeToolHandler, name string, handler RuntimeToolHandler) {
	if dst == nil || strings.TrimSpace(name) == "" || handler == nil {
		return
	}
	dst[strings.TrimSpace(name)] = handler
}

// GetRuntimeTool reads a runtime handler from map storage.
func GetRuntimeTool(src map[string]RuntimeToolHandler, name string) (RuntimeToolHandler, bool) {
	if src == nil {
		return nil, false
	}
	handler, ok := src[strings.TrimSpace(name)]
	return handler, ok
}

// ToolCallCounter increments and tracks tool-call invocation count.
type ToolCallCounter interface {
	IncrementToolCall(name string) int64
}

// CodeRunTracker tracks in-flight code_run/code_run_test execution cancellation.
type CodeRunTracker interface {
	RegisterCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string
	UnregisterCodeRunCancel(agentID, runKey string)
}

// Providers wires all dependencies required by register/dispatch.
type Providers struct {
	LSP           tools.LSPProvider
	CodeRun       tools.CodeRunProvider
	Approvals     tools.ApprovalProvider
	Resource      tools.ResourceProvider
	Orchestration tools.OrchestrationProvider
	AgentRuntime  tools.AgentRuntimeProvider
	Schema        tools.SchemaProvider
	Lookup        RuntimeLookup
	Counter       ToolCallCounter
	CodeRunTracker
}

type schemaProviderFunc func() []agentcore.DynamicTool

func (f schemaProviderFunc) AllSchemas() []agentcore.DynamicTool { return f() }

type extendedLSPDynamicToolProvider struct {
	name     string
	register func(tools.LSPProvider)
	build    func() []agentcore.DynamicTool
}

var (
	extendedLSPDynamicToolProvidersMu sync.RWMutex
	extendedLSPDynamicToolProviders   []extendedLSPDynamicToolProvider
)

// RegisterExtendedLSPDynamicToolProvider registers an additional extended LSP tool provider.
func RegisterExtendedLSPDynamicToolProvider(
	name string,
	register func(tools.LSPProvider),
	build func() []agentcore.DynamicTool,
) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || build == nil {
		return
	}

	extendedLSPDynamicToolProvidersMu.Lock()
	defer extendedLSPDynamicToolProvidersMu.Unlock()
	extendedLSPDynamicToolProviders = append(extendedLSPDynamicToolProviders, extendedLSPDynamicToolProvider{
		name:     trimmed,
		register: register,
		build:    build,
	})
}

// Register wires all dynamic tools into the runtime registry.
func Register(registry RuntimeRegistry, deps Providers) {
	if registry == nil {
		return
	}
	for _, tool := range runtimeTools(deps) {
		name := strings.TrimSpace(tool.Schema.Name)
		if name == "" || tool.Handler == nil {
			continue
		}
		registry.RegisterRuntimeTool(name, tool.Handler)
	}
}

// AllSchemas returns all dynamic tool schemas exposed to agents.
func AllSchemas(deps Providers) []agentcore.DynamicTool {
	allTools := schemaTools(deps)
	if len(allTools) == 0 {
		return nil
	}
	schemas := tools.Schemas(allTools)
	sort.SliceStable(schemas, func(i, j int) bool {
		return schemas[i].Name < schemas[j].Name
	})
	return schemas
}

func runtimeTools(deps Providers) []tools.Tool {
	out := make([]tools.Tool, 0, 64)
	if deps.LSP != nil {
		out = append(out, buildLSPTools(deps.LSP)...)
	}

	schemaProvider := deps.Schema
	if schemaProvider == nil {
		fallbackDeps := deps
		fallbackDeps.Schema = nil
		schemaProvider = schemaProviderFunc(func() []agentcore.DynamicTool {
			return AllSchemas(fallbackDeps)
		})
	}

	out = append(out, tools.OrchestrationTools(deps.Orchestration, deps.AgentRuntime, schemaProvider)...)
	out = append(out, tools.ResourceTools(deps.Resource)...)
	out = append(out, tools.CodeRunTools(deps.CodeRun, deps.AgentRuntime, deps.Approvals)...)
	return dedupeToolsByName(out)
}

func schemaTools(deps Providers) []tools.Tool {
	out := make([]tools.Tool, 0, 64)
	if deps.LSP != nil && hasAvailableLSPServer(deps.LSP) {
		out = append(out, buildLSPTools(deps.LSP)...)
	}

	schemaProvider := deps.Schema
	if schemaProvider == nil {
		fallbackDeps := deps
		fallbackDeps.Schema = nil
		schemaProvider = schemaProviderFunc(func() []agentcore.DynamicTool {
			return AllSchemas(fallbackDeps)
		})
	}

	out = append(out, tools.OrchestrationTools(deps.Orchestration, deps.AgentRuntime, schemaProvider)...)
	out = append(out, tools.ResourceTools(deps.Resource)...)
	out = append(out, tools.CodeRunTools(deps.CodeRun, deps.AgentRuntime, deps.Approvals)...)
	return dedupeToolsByName(out)
}

func buildLSPTools(provider tools.LSPProvider) []tools.Tool {
	if provider == nil {
		return nil
	}

	var out []tools.Tool

	baseHandlers := make(map[string]tools.LSPDynamicToolHandler)
	tools.RegisterLSPHandlers(baseHandlers, provider)
	for _, schema := range tools.LSPTools() {
		handler, ok := baseHandlers[strings.TrimSpace(schema.Name)]
		if !ok || handler == nil {
			continue
		}
		out = append(out, tools.Tool{
			Schema: schema,
			Handler: func(h tools.LSPDynamicToolHandler) RuntimeToolHandler {
				return func(_ tools.ToolCallContext, args json.RawMessage) string {
					return h(args)
				}
			}(handler),
		})
	}

	extProviders := snapshotExtendedLSPDynamicToolProviders()
	extHandlers := make(map[string]tools.LSPDynamicToolHandler)
	extContext := lspExtProviderContext{
		LSPHandlerProvider: provider,
		dynTools:           extHandlers,
	}
	for _, ext := range extProviders {
		if ext.register != nil {
			ext.register(extContext)
		}
	}

	for _, schema := range buildExtendedLSPDynamicToolSchemas(extProviders) {
		handler, ok := extHandlers[strings.TrimSpace(schema.Name)]
		if !ok || handler == nil {
			continue
		}
		out = append(out, tools.Tool{
			Schema: schema,
			Handler: func(h tools.LSPDynamicToolHandler) RuntimeToolHandler {
				return func(_ tools.ToolCallContext, args json.RawMessage) string {
					return h(args)
				}
			}(handler),
		})
	}
	return dedupeToolsByName(out)
}

func hasAvailableLSPServer(provider tools.LSPProvider) bool {
	if provider == nil {
		return false
	}
	summary := provider.AvailabilitySummary()
	if len(summary) == 0 {
		return true
	}
	v, ok := summary["hasAvailableServer"]
	if !ok {
		return true
	}
	available, ok := v.(bool)
	if !ok {
		return true
	}
	return available
}

func snapshotExtendedLSPDynamicToolProviders() []extendedLSPDynamicToolProvider {
	defaultProviders := tools.LSPExtTools()

	extendedLSPDynamicToolProvidersMu.RLock()
	customProviders := append([]extendedLSPDynamicToolProvider(nil), extendedLSPDynamicToolProviders...)
	extendedLSPDynamicToolProvidersMu.RUnlock()

	providers := make([]extendedLSPDynamicToolProvider, 0, len(defaultProviders)+len(customProviders))
	seen := make(map[string]struct{}, len(defaultProviders)+len(customProviders))
	appendProvider := func(provider extendedLSPDynamicToolProvider) {
		name := strings.TrimSpace(provider.name)
		if name == "" || provider.build == nil {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		provider.name = name
		providers = append(providers, provider)
	}

	for _, provider := range defaultProviders {
		appendProvider(extendedLSPDynamicToolProvider{
			name:     provider.Name,
			register: provider.Register,
			build:    provider.Build,
		})
	}
	for _, provider := range customProviders {
		appendProvider(provider)
	}

	sort.SliceStable(providers, func(i, j int) bool {
		return providers[i].name < providers[j].name
	})
	return providers
}

func buildExtendedLSPDynamicToolSchemas(providers []extendedLSPDynamicToolProvider) []agentcore.DynamicTool {
	if len(providers) == 0 {
		return nil
	}

	toolsOut := make([]agentcore.DynamicTool, 0, len(providers))
	for _, provider := range providers {
		if provider.build == nil {
			continue
		}
		toolsOut = append(toolsOut, provider.build()...)
	}

	sort.SliceStable(toolsOut, func(i, j int) bool {
		return toolsOut[i].Name < toolsOut[j].Name
	})
	return dedupeSchemasByName(toolsOut)
}

type lspExtProviderContext struct {
	tools.LSPHandlerProvider
	dynTools map[string]tools.LSPDynamicToolHandler
}

func (c lspExtProviderContext) BindDynamicTool(name string, handler tools.LSPDynamicToolHandler) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || handler == nil || c.dynTools == nil {
		return
	}
	c.dynTools[trimmed] = handler
}

func dedupeToolsByName(list []tools.Tool) []tools.Tool {
	if len(list) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(list))
	out := make([]tools.Tool, 0, len(list))
	for _, tool := range list {
		name := strings.TrimSpace(tool.Schema.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, tool)
	}
	return out
}

func dedupeSchemasByName(list []agentcore.DynamicTool) []agentcore.DynamicTool {
	if len(list) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(list))
	out := make([]agentcore.DynamicTool, 0, len(list))
	for _, tool := range list {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, tool)
	}
	return out
}

func resetExtendedLSPDynamicToolProvidersForTest() {
	extendedLSPDynamicToolProvidersMu.Lock()
	extendedLSPDynamicToolProviders = nil
	extendedLSPDynamicToolProvidersMu.Unlock()
}
