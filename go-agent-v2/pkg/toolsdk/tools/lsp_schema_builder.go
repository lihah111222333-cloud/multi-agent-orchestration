package tools

import (
	"maps"

	agentcore "github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

const (
	defaultFilePathDescription = "Absolute or relative path to the file"
	defaultLineDescription     = "0-indexed line number"
	defaultColumnDescription   = "0-indexed column number"
)

func lspBaseSpec(name string, description string, schema map[string]any, handler func(LSPHandlerProvider) LSPDynamicToolHandler) lspBaseToolSpec {
	return lspBaseToolSpec{
		schema:  agentcore.DynamicTool{Name: name, Description: description, InputSchema: schema},
		handler: handler,
	}
}

func lspRequired(fields ...string) []string {
	out := fields[:0]
	for _, field := range fields {
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func lspSchema(properties map[string]any, required []string, extras map[string]any) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	maps.Copy(schema, extras)
	return schema
}

func lspFileLineColumnSchema(filePathDescription string, lineDescription string, columnDescription string, extraProperties map[string]any, required []string, schemaExtras map[string]any) map[string]any {
	if filePathDescription == "" {
		filePathDescription = defaultFilePathDescription
	}
	if lineDescription == "" {
		lineDescription = defaultLineDescription
	}
	if columnDescription == "" {
		columnDescription = defaultColumnDescription
	}
	properties := map[string]any{
		"file_path": lspStringProperty(filePathDescription),
		"line":      lspNumberProperty(lineDescription),
		"column":    lspNumberProperty(columnDescription),
	}
	maps.Copy(properties, extraProperties)
	return lspSchema(properties, required, schemaExtras)
}

func lspStringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func lspEnumStringProperty(description string, enumValues []string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": enumValues}
}

func lspNumberProperty(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func lspBooleanProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func lspStringArrayProperty(description string) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "string"}}
}
