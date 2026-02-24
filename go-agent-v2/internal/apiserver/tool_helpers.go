package apiserver

import "github.com/multi-agent/go-agent-v2/internal/tools"

func toolJSON(v any) string {
	return tools.ToolJSON(v)
}

func toolError(err error) string {
	return tools.ToolError(err)
}
