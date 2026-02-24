package apiserver

import (
	"sort"
	"strings"
	"sync"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/tools"
)

type extendedLSPDynamicToolProvider struct {
	name     string
	register func(tools.LSPProvider)
	build    func() []agentcore.DynamicTool
}

var (
	extendedLSPDynamicToolProvidersMu sync.RWMutex
	extendedLSPDynamicToolProviders   []extendedLSPDynamicToolProvider
)

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

func registerExtendedLSPDynamicToolProvider(
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

func registerExtendedLSPDynamicTools(lspTools tools.LSPHandlerProvider, dynTools map[string]tools.LSPDynamicToolHandler) {
	providers := snapshotExtendedLSPDynamicToolProviders()
	contextProvider := lspExtProviderContext{
		LSPHandlerProvider: lspTools,
		dynTools:           dynTools,
	}
	for _, provider := range providers {
		if provider.register != nil {
			provider.register(contextProvider)
		}
	}
}

func extendedLSPDynamicToolSchemas() []agentcore.DynamicTool {
	providers := snapshotExtendedLSPDynamicToolProviders()
	if len(providers) == 0 {
		return nil
	}

	toolsOut := make([]agentcore.DynamicTool, 0, len(providers))
	for _, provider := range providers {
		toolsOut = append(toolsOut, provider.build()...)
	}

	sort.SliceStable(toolsOut, func(i, j int) bool {
		return toolsOut[i].Name < toolsOut[j].Name
	})
	return dedupeDynamicToolsByName(toolsOut)
}

func dedupeDynamicToolsByName(tools []agentcore.DynamicTool) []agentcore.DynamicTool {
	if len(tools) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]agentcore.DynamicTool, 0, len(tools))
	for _, tool := range tools {
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
