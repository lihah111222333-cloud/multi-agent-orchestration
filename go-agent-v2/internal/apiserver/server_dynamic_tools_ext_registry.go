package apiserver

import (
	"sort"
	"strings"
	"sync"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

type extendedLSPDynamicToolProvider struct {
	name     string
	register func(*Server)
	build    func(*Server) []codex.DynamicTool
}

var (
	extendedLSPDynamicToolProvidersMu sync.RWMutex
	extendedLSPDynamicToolProviders   []extendedLSPDynamicToolProvider
)

func registerExtendedLSPDynamicToolProvider(
	name string,
	register func(*Server),
	build func(*Server) []codex.DynamicTool,
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
	extendedLSPDynamicToolProvidersMu.RLock()
	providers := append([]extendedLSPDynamicToolProvider(nil), extendedLSPDynamicToolProviders...)
	extendedLSPDynamicToolProvidersMu.RUnlock()

	sort.SliceStable(providers, func(i, j int) bool {
		return providers[i].name < providers[j].name
	})
	return providers
}

func (s *Server) registerExtendedLSPDynamicTools() {
	providers := snapshotExtendedLSPDynamicToolProviders()
	for _, provider := range providers {
		if provider.register != nil {
			provider.register(s)
		}
	}
}

func (s *Server) buildExtendedLSPDynamicTools() []codex.DynamicTool {
	providers := snapshotExtendedLSPDynamicToolProviders()
	if len(providers) == 0 {
		return nil
	}

	tools := make([]codex.DynamicTool, 0, len(providers))
	for _, provider := range providers {
		tools = append(tools, provider.build(s)...)
	}

	sort.SliceStable(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return dedupeDynamicToolsByName(tools)
}

func dedupeDynamicToolsByName(tools []codex.DynamicTool) []codex.DynamicTool {
	if len(tools) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]codex.DynamicTool, 0, len(tools))
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
