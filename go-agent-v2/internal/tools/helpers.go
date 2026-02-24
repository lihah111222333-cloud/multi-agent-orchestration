package tools

import "encoding/json"

// ToolJSON marshals a value into a JSON string for dynamic tool responses.
func ToolJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{"error":"internal: json marshal failed"}`
	}
	return string(data)
}

// ToolError encodes an error as a dynamic-tool JSON payload.
func ToolError(err error) string {
	if err == nil {
		return ToolJSON(map[string]string{"error": "unknown error"})
	}
	return ToolJSON(map[string]string{"error": err.Error()})
}
