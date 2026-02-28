package tools

import "encoding/json"

func ToolJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{"error":"internal: json marshal failed"}`
	}
	return string(data)
}

func ToolError(err error) string {
	if err == nil {
		return ToolJSON(map[string]string{"error": "unknown error"})
	}
	return ToolJSON(map[string]string{"error": err.Error()})
}
