package tools

import agentcore "github.com/multi-agent/go-agent-v2/internal/agentcore"

const (
	defaultFilePathDescription = "Absolute or relative path to the file"
	defaultLineDescription     = "0-indexed line number"
	defaultColumnDescription   = "0-indexed column number"
)

type lspToolBinding struct {
	schema  agentcore.DynamicTool
	handler func(LSPProvider) LSPDynamicToolHandler
}

func lspDynamicTool(name string, description string, inputSchema map[string]any) agentcore.DynamicTool {
	return agentcore.DynamicTool{Name: name, Description: description, InputSchema: inputSchema}
}

func lspBaseSpec(name string, description string, schema map[string]any, handler func(LSPHandlerProvider) LSPDynamicToolHandler) lspBaseToolSpec {
	return lspBaseToolSpec{schema: lspDynamicTool(name, description, schema), handler: handler}
}

func lspRequired(fields ...string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "" {
			out = append(out, field)
		}
	}
	if len(out) == 0 {
		return nil
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
	for key, value := range extras {
		schema[key] = value
	}
	return schema
}

func lspFilePathSchema(filePathDescription string, required bool, extraProperties map[string]any, schemaExtras map[string]any) map[string]any {
	properties := map[string]any{
		"file_path": lspStringProperty(defaultIfEmpty(filePathDescription, defaultFilePathDescription)),
	}
	for key, value := range extraProperties {
		properties[key] = value
	}
	requiredFields := []string{}
	if required {
		requiredFields = append(requiredFields, "file_path")
	}
	return lspSchema(properties, requiredFields, schemaExtras)
}

func lspFileLineColumnSchema(filePathDescription string, lineDescription string, columnDescription string, extraProperties map[string]any, required []string, schemaExtras map[string]any) map[string]any {
	properties := map[string]any{
		"file_path": lspStringProperty(defaultIfEmpty(filePathDescription, defaultFilePathDescription)),
		"line":      lspNumberProperty(defaultIfEmpty(lineDescription, defaultLineDescription)),
		"column":    lspNumberProperty(defaultIfEmpty(columnDescription, defaultColumnDescription)),
	}
	for key, value := range extraProperties {
		properties[key] = value
	}
	return lspSchema(properties, required, schemaExtras)
}

func defaultIfEmpty(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
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
