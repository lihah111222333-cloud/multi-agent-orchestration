// convert.go — 统一 any→string / any→[]string 转换工具。
//
// 消除 4 处重复实现:
//   - apiserver.asString
//   - codex.trimmedStringValue
//   - uistate.extractStringList
//   - orchestrator.extractStringSlice
package util

import (
	"fmt"
	"strings"
)

// AsString 将 any 安全转为 trimmed string。
//
// 支持 string / fmt.Stringer，其他类型返回 ""。
func AsString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

// AsStringSlice 将 any 安全转为 []string（去空、trim）。
//
// 支持 []string / []any，其他类型返回 nil。
func AsStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		items := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(item)
			if text != "" {
				items = append(items, text)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				trimmed := strings.TrimSpace(text)
				if trimmed != "" {
					items = append(items, trimmed)
				}
			}
		}
		return items
	default:
		return nil
	}
}
