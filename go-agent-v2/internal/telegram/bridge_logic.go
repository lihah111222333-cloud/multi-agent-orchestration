// bridge_logic.go — Telegram 桥接纯逻辑函数 (对应 Python tg_bridge.py)。
//
// 无外部依赖，均为可独立测试的纯函数。
package telegram

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// ========================================
// 常量
// ========================================

const (
	defaultMaxTruncate = 4000
	maxHistoryLen      = 200
)

// workerAgentIDRe 匹配 agent_01..agent_99 (大小写不敏感, 与 Python 等价)。
var workerAgentIDRe = regexp.MustCompile(`(?i)^agent_\d{2}$`)

// ========================================
// truncateText (对应 Python _truncate)
// ========================================

// truncateText 中间截断: 保留头尾各 half，中间替换为 "... (已截断) ..."。
func truncateText(text string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = defaultMaxTruncate
	}
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	half := maxLen/2 - 20
	if half < 0 {
		half = 0
	}
	return string(runes[:half]) + "\n\n... (已截断) ...\n\n" + string(runes[len(runes)-half:])
}

// ========================================
// isAuthorized (对应 Python _is_authorized)
// ========================================

// isAuthorized 检查 chatID 是否在白名单。空白名单 = 允许所有。
func isAuthorized(chatID string, allowedChatID string) bool {
	if allowedChatID == "" {
		return true
	}
	return chatID == allowedChatID
}

// ========================================
// normalizeMasterName (对应 Python _normalize_master_name)
// ========================================

// normalizeMasterName 标准化主 Agent 名称 (修正常见拼写错误)。
func normalizeMasterName(value string) string {
	text := strings.TrimSpace(strings.ToLower(value))
	text = strings.ReplaceAll(text, "agenr", "agent")
	text = strings.ReplaceAll(text, "agnet", "agent")
	return text
}

// ========================================
// isWorkerAgentID (对应 Python _is_worker_agent_id)
// ========================================

// isWorkerAgentID 检测是否为有效工人 Agent ID (agent_01 ~ agent_99)。
func isWorkerAgentID(value string) bool {
	return workerAgentIDRe.MatchString(strings.TrimSpace(value))
}

// ========================================
// History 消息历史 (对应 Python _history deque)
// ========================================

// HistoryEntry 历史记录条目。
type HistoryEntry struct {
	Ts     string `json:"ts"`
	Role   string `json:"role"`
	Text   string `json:"text"`
	ChatID string `json:"chat_id"`
	User   string `json:"user"`
	Status string `json:"status"`
}

// History 线程安全的环形消息历史。
type History struct {
	mu      sync.Mutex // 保护 entries slice
	entries []HistoryEntry
	maxLen  int
}

// NewHistory 创建消息历史 (默认 200 条)。
func NewHistory() *History {
	return &History{maxLen: maxHistoryLen}
}

// Add 添加记录 (对应 Python _add_history)。
func (h *History) Add(role, text, chatID, user, status string) HistoryEntry {
	// 截断 text 到 4000 字符
	runes := []rune(text)
	if len(runes) > 4000 {
		runes = runes[:4000]
	}

	entry := HistoryEntry{
		Ts:     time.Now().UTC().Format(time.RFC3339),
		Role:   role,
		Text:   string(runes),
		ChatID: chatID,
		User:   user,
		Status: status,
	}

	h.mu.Lock()
	h.entries = append(h.entries, entry)
	if len(h.entries) > h.maxLen {
		h.entries = h.entries[len(h.entries)-h.maxLen:]
	}
	h.mu.Unlock()

	return entry
}

// Get 获取最近 limit 条记录 (对应 Python get_tg_history)。
func (h *History) Get(limit int) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	if limit <= 0 || limit > len(h.entries) {
		limit = len(h.entries)
	}
	start := len(h.entries) - limit
	result := make([]HistoryEntry, limit)
	copy(result, h.entries[start:])
	return result
}

// Clear 清空历史 (对应 Python clear_tg_history)。
func (h *History) Clear() {
	h.mu.Lock()
	h.entries = nil
	h.mu.Unlock()
}

// Len 返回当前记录数。
func (h *History) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}
