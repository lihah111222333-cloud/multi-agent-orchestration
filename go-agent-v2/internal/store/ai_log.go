// ai_log.go — AI 日志查询 (对应 Python ai_log.py, 12 字段, 3 regex)。
// 基于 system_logs 派生，无独立表。
package store

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AILogStore AI 日志存储。
type AILogStore struct{ BaseStore }

// NewAILogStore 创建 AI 日志存储。
func NewAILogStore(pool *pgxpool.Pool) *AILogStore { return &AILogStore{NewBaseStore(pool)} }

// Python ai_log.py 中 3 个核心 regex:
var (
	// POST https://api.openai.com/v1/chat/completions → method=POST, url=…
	reHTTP = regexp.MustCompile(`(?i)(GET|POST|PUT|DELETE|PATCH|HEAD)\s+(https?://\S+)`)

	// HTTP/1.1 200 OK → status_code=200, status_text=OK
	reStatus = regexp.MustCompile(`(?i)HTTP/\d\.\d\s+(\d{3})\s*(\S*)`)

	// model=gpt-4o / model: gpt-4o → model=…
	reModel = regexp.MustCompile(`(?i)model[=:]\s*([^\s,;"'\]]+)`)
)

// classifyAILog 精细分类 (对应 Python _classify_row, 6 类别)。
func classifyAILog(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "api request") || strings.Contains(lower, "request to") ||
		strings.Contains(lower, "http request"):
		return "api_request"
	case strings.Contains(lower, "api error") || strings.Contains(lower, "api_error"):
		return "api_error"
	case strings.Contains(lower, "compat") || strings.Contains(lower, "fallback") ||
		strings.Contains(msg, "兼容"):
		return "compat_fallback"
	case strings.Contains(lower, "runtime") && strings.Contains(lower, "config"):
		return "runtime_config"
	case strings.Contains(lower, "error") || strings.Contains(lower, "exception"):
		return "error"
	default:
		return "ai_event"
	}
}

// extractHTTP 从消息提取 HTTP method + url + endpoint (对应 Python _extract_endpoint)。
func extractHTTP(msg string) (method, url, endpoint string) {
	if m := reHTTP.FindStringSubmatch(msg); len(m) == 3 {
		method = strings.ToUpper(m[1])
		url = m[2]
		// 提取路径部分作为 endpoint
		if idx := strings.Index(url, "//"); idx >= 0 {
			rest := url[idx+2:]
			if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
				endpoint = rest[slashIdx:]
			}
		}
	}
	return
}

// extractStatus 提取 HTTP 状态码 (对应 Python _HTTPX_STATUS_RE)。
func extractStatus(msg string) (code, text string) {
	if m := reStatus.FindStringSubmatch(msg); len(m) >= 2 {
		code = m[1]
		if len(m) >= 3 {
			text = m[2]
		}
	}
	return
}

// extractModel 提取模型名 (对应 Python _MODEL_RE)。
func extractModel(msg string) string {
	if m := reModel.FindStringSubmatch(msg); len(m) == 2 {
		return m[1]
	}
	return ""
}

// Query 查询 AI 日志 (从 system_logs 读取、分类、提取 12 字段)。
func (s *AILogStore) Query(ctx context.Context, category, keyword string, limit int) ([]AILogRow, error) {
	q := NewQueryBuilder().
		KeywordLike(keyword, "message")
	sql, params := q.Build(
		"SELECT "+sysLogCols+" FROM system_logs",
		"ts DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	sysLogs, err := collectRows[SystemLog](rows)
	if err != nil {
		return nil, err
	}

	var result []AILogRow
	for _, log := range sysLogs {
		cat := classifyAILog(log.Message)
		if category != "" && cat != category {
			continue
		}
		method, url, endpoint := extractHTTP(log.Message)
		statusCode, statusText := extractStatus(log.Message)
		model := extractModel(log.Message)

		result = append(result, AILogRow{
			Ts:         log.Ts,
			Level:      log.Level,
			Logger:     log.Logger,
			Message:    log.Message,
			Raw:        log.Raw,
			Category:   cat,
			Method:     method,
			URL:        url,
			Endpoint:   endpoint,
			StatusCode: statusCode,
			StatusText: statusText,
			Model:      model,
		})
	}
	return result, nil
}
