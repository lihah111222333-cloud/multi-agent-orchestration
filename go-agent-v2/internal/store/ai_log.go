package store

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type AILogStore struct{ BaseStore }

func NewAILogStore(pool *pgxpool.Pool) *AILogStore { return &AILogStore{NewBaseStore(pool)} }

var (
	reHTTP   = regexp.MustCompile(`(?i)(GET|POST|PUT|DELETE|PATCH|HEAD)\s+(https?://\S+)`)
	reStatus = regexp.MustCompile(`(?i)HTTP/\d\.\d\s+(\d{3})\s*(\S*)`)
	reModel  = regexp.MustCompile(`(?i)model[=:]\s*([^\s,;"'\]]+)`)
)

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

func extractHTTP(msg string) (method, url, endpoint string) {
	if m := reHTTP.FindStringSubmatch(msg); len(m) == 3 {
		method = strings.ToUpper(m[1])
		url = m[2]
		if _, rest, ok := strings.Cut(url, "//"); ok {
			if slash := strings.IndexByte(rest, '/'); slash >= 0 {
				endpoint = rest[slash:]
			}
		}
	}
	return
}

func (s *AILogStore) Query(ctx context.Context, category, keyword string, limit int) ([]AILogRow, error) {
	limit = util.ClampInt(limit, 1, 2000)
	fetchLimit := limit
	if category != "" {
		fetchLimit = min(limit*5, 2000)
	}
	sql, params := NewQueryBuilder().KeywordLike(keyword, "message").Build("SELECT "+sysLogCols+" FROM system_logs", "ts DESC, id DESC", fetchLimit)
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
		statusCode, statusText := "", ""
		if m := reStatus.FindStringSubmatch(log.Message); len(m) >= 2 {
			statusCode = m[1]
			if len(m) == 3 {
				statusText = m[2]
			}
		}
		model := ""
		if m := reModel.FindStringSubmatch(log.Message); len(m) == 2 {
			model = m[1]
		}

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
		if len(result) == limit {
			break
		}
	}
	return result, nil
}
