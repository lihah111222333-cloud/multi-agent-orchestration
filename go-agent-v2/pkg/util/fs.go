package util

import (
	"os"
	"strings"
)

// FileExists 检查路径是否为已存在的常规文件（非目录）。
// 空路径或 whitespace-only 路径直接返回 false。
func FileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// IsRemoteImageURL 检查字符串是否为远程图片 URL 或 data:image URI。
func IsRemoteImageURL(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "data:image/")
}
