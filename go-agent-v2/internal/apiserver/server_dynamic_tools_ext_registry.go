package apiserver

import (
	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
	"github.com/multi-agent/go-agent-v2/internal/tools"
)

func registerExtendedLSPDynamicToolProvider(
	name string,
	register func(tools.LSPProvider),
	build func() []agentcore.DynamicTool,
) {
	tooladapter.RegisterExtendedLSPDynamicToolProvider(name, register, build)
}
