package tooladapter

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	agentcore "github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/tools"
)

type RuntimeToolHandler = func(ctx tools.ToolCallContext, args json.RawMessage) string

type RuntimeRegistry interface {
	RegisterRuntimeTool(name string, handler RuntimeToolHandler)
}

type RuntimeLookup interface {
	LookupRuntimeTool(name string) (RuntimeToolHandler, bool)
}

func SetRuntimeTool(dst map[string]RuntimeToolHandler, name string, handler RuntimeToolHandler) {
	if dst == nil || strings.TrimSpace(name) == "" || handler == nil {
		return
	}
	dst[name] = handler
}

func GetRuntimeTool(src map[string]RuntimeToolHandler, name string) (RuntimeToolHandler, bool) {
	if src == nil || strings.TrimSpace(name) == "" {
		return nil, false
	}
	handler, ok := src[name]
	return handler, ok
}

type ToolCallCounter interface {
	IncrementToolCall(name string) int64
}

type CodeRunTracker interface {
	RegisterCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string
	UnregisterCodeRunCancel(agentID, runKey string)
}

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

func (f schemaProviderFunc) AllSchemas() []agentcore.DynamicTool {
	if f == nil {
		return nil
	}
	return f()
}

type lspAddonDynamicToolProvider struct {
	name     string
	register func(tools.LSPProvider)
	build    func() []agentcore.DynamicTool
}

var (
	lspAddonDynamicToolProvidersMu sync.RWMutex
	lspAddonDynamicToolProviders   []lspAddonDynamicToolProvider
)

func RegisterExtendedLSPDynamicToolProvider(name string, register func(tools.LSPProvider), build func() []agentcore.DynamicTool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || (register == nil && build == nil) {
		return
	}

	provider := lspAddonDynamicToolProvider{
		name:     trimmed,
		register: register,
		build:    build,
	}

	lspAddonDynamicToolProvidersMu.Lock()
	defer lspAddonDynamicToolProvidersMu.Unlock()

	for i := range lspAddonDynamicToolProviders {
		if lspAddonDynamicToolProviders[i].name == trimmed {
			lspAddonDynamicToolProviders[i] = provider
			return
		}
	}

	lspAddonDynamicToolProviders = append(lspAddonDynamicToolProviders, provider)
	sort.SliceStable(lspAddonDynamicToolProviders, func(i, j int) bool {
		return lspAddonDynamicToolProviders[i].name < lspAddonDynamicToolProviders[j].name
	})
}

func Register(registry RuntimeRegistry, deps Providers) {
	if registry == nil {
		return
	}
	for _, tool := range runtimeTools(deps) {
		if strings.TrimSpace(tool.Schema.Name) == "" || tool.Handler == nil {
			continue
		}
		registry.RegisterRuntimeTool(tool.Schema.Name, tool.Handler)
	}
}

func AllSchemas(deps Providers) []agentcore.DynamicTool {
	allTools := schemaTools(deps)
	schemas := make([]agentcore.DynamicTool, 0, len(allTools))
	for _, tool := range allTools {
		if strings.TrimSpace(tool.Schema.Name) == "" {
			continue
		}
		schemas = append(schemas, tool.Schema)
	}
	return dedupeSchemasByName(schemas)
}

func runtimeTools(deps Providers) []tools.Tool {
	resolvedSchema := deps.Schema
	if resolvedSchema == nil {
		resolvedSchema = schemaProviderFunc(func() []agentcore.DynamicTool { return nil })
	}

	all := make([]tools.Tool, 0, 32)
	all = append(all, buildLSPTools(deps.LSP)...)
	all = append(all, tools.CodeRunTools(deps.CodeRun, deps.AgentRuntime, deps.Approvals)...)
	all = append(all, tools.ResourceTools(deps.Resource)...)
	all = append(all, tools.OrchestrationTools(deps.Orchestration, deps.AgentRuntime, resolvedSchema)...)
	return dedupeToolsByName(all)
}

func schemaTools(deps Providers) []tools.Tool {
	resolvedSchema := deps.Schema
	if resolvedSchema == nil {
		resolvedSchema = schemaProviderFunc(func() []agentcore.DynamicTool { return nil })
	}

	lspTools := buildLSPTools(deps.LSP)
	if !hasAvailableLSPServer(deps.LSP) {
		filtered := make([]tools.Tool, 0, 1)
		for _, tool := range lspTools {
			if tool.Schema.Name == "lsp_grep" {
				filtered = append(filtered, tool)
				break
			}
		}
		lspTools = filtered
	}

	all := make([]tools.Tool, 0, 32)
	all = append(all, lspTools...)
	all = append(all, tools.CodeRunTools(deps.CodeRun, deps.AgentRuntime, deps.Approvals)...)
	all = append(all, tools.ResourceTools(deps.Resource)...)
	all = append(all, tools.OrchestrationTools(deps.Orchestration, deps.AgentRuntime, resolvedSchema)...)
	return dedupeToolsByName(all)
}

func buildLSPTools(provider tools.LSPProvider) []tools.Tool {
	if provider == nil {
		return nil
	}

	dynTools := make(map[string]tools.LSPDynamicToolHandler)
	tools.RegisterLSPHandlers(dynTools, provider)

	addonProviders := snapshotLSPAddonDynamicToolProviders()
	addonCtx := lspAddonProviderContext{LSPHandlerProvider: provider, dynTools: dynTools}
	for _, ext := range addonProviders {
		if ext.register != nil {
			ext.register(addonCtx)
		}
	}

	schemas := append([]agentcore.DynamicTool{}, tools.LSPTools()...)
	schemas = append(schemas, buildLSPAddonDynamicToolSchemas(addonProviders)...)
	schemas = append(schemas, tools.LSPAddonTools()...)
	schemas = dedupeSchemasByName(schemas)

	result := make([]tools.Tool, 0, len(schemas))
	for _, schema := range schemas {
		handler, ok := dynTools[schema.Name]
		if !ok || handler == nil {
			continue
		}
		localHandler := handler
		result = append(result, tools.Tool{
			Schema: schema,
			Handler: func(_ tools.ToolCallContext, args json.RawMessage) string {
				return localHandler(args)
			},
		})
	}
	return dedupeToolsByName(result)
}

func hasAvailableLSPServer(provider tools.LSPProvider) bool {
	if provider == nil {
		return false
	}
	summary := provider.AvailabilitySummary()
	if len(summary) == 0 {
		return true
	}
	available, known := availabilityFromAny(summary)
	if !known {
		return true
	}
	return available
}

func availabilityFromAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		return availabilityFromStatus(v)
	case map[string]any:
		if raw, ok := v["available"]; ok {
			if available, known := availabilityFromAny(raw); known {
				return available, true
			}
		}
		if raw, ok := v["enabled"]; ok {
			if available, known := availabilityFromAny(raw); known {
				return available, true
			}
		}
		if raw, ok := v["status"]; ok {
			if available, known := availabilityFromAny(raw); known {
				return available, true
			}
		}
		known := false
		anyAvailable := false
		for _, raw := range v {
			available, ok := availabilityFromAny(raw)
			if !ok {
				continue
			}
			known = true
			if available {
				anyAvailable = true
			}
		}
		return anyAvailable, known
	case []any:
		known := false
		anyAvailable := false
		for _, raw := range v {
			available, ok := availabilityFromAny(raw)
			if !ok {
				continue
			}
			known = true
			if available {
				anyAvailable = true
			}
		}
		return anyAvailable, known
	default:
		return false, false
	}
}

func availabilityFromStatus(status string) (bool, bool) {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" {
		return false, false
	}
	switch s {
	case "available", "enabled", "ready", "ok", "healthy", "running":
		return true, true
	case "unavailable", "disabled", "offline", "down", "stopped", "error", "failed":
		return false, true
	default:
		return false, false
	}
}

func snapshotLSPAddonDynamicToolProviders() []lspAddonDynamicToolProvider {
	lspAddonDynamicToolProvidersMu.RLock()
	defer lspAddonDynamicToolProvidersMu.RUnlock()
	if len(lspAddonDynamicToolProviders) == 0 {
		return nil
	}
	out := make([]lspAddonDynamicToolProvider, 0, len(lspAddonDynamicToolProviders))
	for _, provider := range lspAddonDynamicToolProviders {
		out = append(out, provider)
	}
	return out
}

func buildLSPAddonDynamicToolSchemas(providers []lspAddonDynamicToolProvider) []agentcore.DynamicTool {
	if len(providers) == 0 {
		return nil
	}
	out := make([]agentcore.DynamicTool, 0, len(providers))
	for _, provider := range providers {
		if provider.build == nil {
			continue
		}
		out = append(out, provider.build()...)
	}
	return dedupeSchemasByName(out)
}

type lspAddonProviderContext struct {
	tools.LSPHandlerProvider
	dynTools map[string]tools.LSPDynamicToolHandler
}

func (c lspAddonProviderContext) BindDynamicTool(name string, handler tools.LSPDynamicToolHandler) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || handler == nil || c.dynTools == nil {
		return
	}
	c.dynTools[trimmed] = handler
}

func dedupeToolsByName(list []tools.Tool) []tools.Tool {
	if len(list) <= 1 {
		return list
	}
	out := make([]tools.Tool, 0, len(list))
	seen := make(map[string]struct{}, len(list))
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
	if len(list) <= 1 {
		return list
	}
	out := make([]agentcore.DynamicTool, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	for _, schema := range list {
		name := strings.TrimSpace(schema.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, schema)
	}
	return out
}

func resetExtendedLSPDynamicToolProvidersForTest() {
	lspAddonDynamicToolProvidersMu.Lock()
	defer lspAddonDynamicToolProvidersMu.Unlock()
	lspAddonDynamicToolProviders = nil
}
