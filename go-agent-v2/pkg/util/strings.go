package util

import "strings"

// FirstNonEmpty 返回第一个非空 (trim 后) 的字符串。
//
// 用于统一多处重复的 firstNonEmpty / firstTrackedTurnNonEmpty 模式。
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
