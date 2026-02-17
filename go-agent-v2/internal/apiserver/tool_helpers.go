// tool_helpers.go — 动态工具 JSON 输出辅助函数。
//
// 替代 fmt.Sprintf 构造 JSON 字符串, 防止注入 (引号/换行泄漏)。
package apiserver

import "encoding/json"

// toolJSON 将任意值序列化为 JSON 字符串 (供 Dynamic Tool 使用)。
//
// 用 json.Marshal 代替 fmt.Sprintf 构造, 保证输出合法 JSON。
func toolJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{"error":"internal: json marshal failed"}`
	}
	return string(data)
}

// toolError 将 error 序列化为 {"error":"..."} 格式 JSON 字符串。
func toolError(err error) string {
	return toolJSON(map[string]string{"error": err.Error()})
}
