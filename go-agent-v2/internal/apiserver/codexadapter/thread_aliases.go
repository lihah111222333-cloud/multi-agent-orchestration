package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const prefThreadAliases = "threads.aliases"

func (a *Adapter) loadThreadAliases(ctx context.Context) map[string]string {
	if a == nil || a.ctx == nil || a.ctx.Store() == nil {
		return map[string]string{}
	}
	value, err := a.ctx.Store().Get(ctx, prefThreadAliases)
	if err != nil {
		logger.Warn("thread aliases: load preference failed", logger.FieldError, err)
		return map[string]string{}
	}
	return NormalizeThreadAliases(value)
}

func (a *Adapter) persistThreadAlias(ctx context.Context, threadID, alias string) error {
	if a == nil || a.ctx == nil || a.ctx.Store() == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	value, err := a.ctx.Store().Get(ctx, prefThreadAliases)
	if err != nil {
		return err
	}
	aliases := NormalizeThreadAliases(value)
	nextAlias := strings.TrimSpace(alias)
	if nextAlias == "" || nextAlias == id {
		delete(aliases, id)
	} else {
		aliases[id] = nextAlias
	}
	return a.ctx.Store().Set(ctx, prefThreadAliases, aliases)
}

// NormalizeThreadAliases parses various formats (map, JSON string, json.RawMessage)
// into a normalized map[string]string of thread aliases.
//
// This is the single canonical implementation — apiserver root delegates here.
func NormalizeThreadAliases(value any) map[string]string {
	aliases := map[string]string{}
	addAlias := func(threadID string, alias any) {
		id := strings.TrimSpace(threadID)
		if id == "" {
			return
		}
		name := strings.TrimSpace(StringValue(alias))
		if name == "" || name == id {
			return
		}
		aliases[id] = name
	}

	switch typed := value.(type) {
	case map[string]string:
		for threadID, alias := range typed {
			addAlias(threadID, alias)
		}
	case map[string]any:
		for threadID, alias := range typed {
			addAlias(threadID, alias)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for threadID, alias := range decoded {
				addAlias(threadID, alias)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for threadID, alias := range decoded {
				addAlias(threadID, alias)
			}
		}
	}
	return aliases
}

func applyThreadAliases(threads []ThreadListItem, aliases map[string]string) {
	if len(threads) == 0 || len(aliases) == 0 {
		return
	}
	for i := range threads {
		id := strings.TrimSpace(threads[i].ID)
		if id == "" {
			continue
		}
		alias := strings.TrimSpace(aliases[id])
		if alias == "" {
			continue
		}
		threads[i].Name = alias
	}
}

// StringValue extracts a string from any value (string or fmt.Stringer).
//
// Equivalent to apiserver.asString — canonical location for codexadapter usage.
func StringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}
