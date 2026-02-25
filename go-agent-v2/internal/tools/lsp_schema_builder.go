package tools

import (
	"maps"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
)

const (
	defaultFilePathDescription = "Absolute or relative path to the file"
	defaultLineDescription     = "0-indexed line number"
	defaultColumnDescription   = "0-indexed column number"
)

type lspToolBinding struct {
	schema  agentcore.DynamicTool
	handler func(LSPProvider) LSPDynamicToolHandler
}

func buildLSPExtRegistryProvider(name string, bindings []lspToolBinding) LSPExtRegistryProvider {
	return LSPExtRegistryProvider{
		Name: name,
		Register: func(provider LSPProvider) {
			if provider == nil {
				return
			}
			for _, binding := range bindings {
				if binding.handler == nil {
					continue
				}
				handler := binding.handler(provider)
				if handler == nil {
					continue
				}
				provider.BindDynamicTool(binding.schema.Name, handler)
			}
		},
		Build: func() []agentcore.DynamicTool {
			out := make([]agentcore.DynamicTool, 0, len(bindings))
			for _, binding := range bindings {
				out = append(out, binding.schema)
			}
			return out
		},
	}
}

func lspDynamicTool(name, description string, inputSchema map[string]any) agentcore.DynamicTool {
	return agentcore.DynamicTool{Name: name, Description: description, InputSchema: inputSchema}
}

func lspBaseSpec(
	name, description string,
	schema map[string]any,
	handler func(LSPHandlerProvider) LSPDynamicToolHandler,
) lspBaseToolSpec {
	return lspBaseToolSpec{schema: lspDynamicTool(name, description, schema), handler: handler}
}

func lspBinding(
	name, description string,
	schema map[string]any,
	handler func(LSPProvider) LSPDynamicToolHandler,
) lspToolBinding {
	return lspToolBinding{schema: lspDynamicTool(name, description, schema), handler: handler}
}

func lspRequired(fields ...string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, len(fields))
	copy(out, fields)
	return out
}

func lspSchema(properties map[string]any, required []string, extras map[string]any) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	maps.Copy(schema, extras)
	return schema
}

func lspFilePathSchema(
	filePathDescription string,
	required bool,
	extraProperties map[string]any,
	schemaExtras map[string]any,
) map[string]any {
	properties := map[string]any{"file_path": lspStringProperty(defaultIfEmpty(filePathDescription, defaultFilePathDescription))}
	maps.Copy(properties, extraProperties)
	requiredFields := []string(nil)
	if required {
		requiredFields = lspRequired("file_path")
	}
	return lspSchema(properties, requiredFields, schemaExtras)
}

func lspFileLineColumnSchema(
	filePathDescription string,
	lineDescription string,
	columnDescription string,
	extraProperties map[string]any,
	required []string,
	schemaExtras map[string]any,
) map[string]any {
	properties := map[string]any{
		"file_path": lspStringProperty(defaultIfEmpty(filePathDescription, defaultFilePathDescription)),
		"line":      lspNumberProperty(defaultIfEmpty(lineDescription, defaultLineDescription)),
		"column":    lspNumberProperty(defaultIfEmpty(columnDescription, defaultColumnDescription)),
	}
	maps.Copy(properties, extraProperties)
	return lspSchema(properties, required, schemaExtras)
}

func defaultIfEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func lspStringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func lspEnumStringProperty(description string, enumValues []string) map[string]any {
	prop := lspStringProperty(description)
	if len(enumValues) > 0 {
		prop["enum"] = enumValues
	}
	return prop
}

func lspNumberProperty(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func lspBooleanProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func lspStringArrayProperty(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}
