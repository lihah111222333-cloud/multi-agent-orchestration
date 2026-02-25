package codexadapter

import (
	"context"
	"strings"
	"time"
)

const prefThreadArchivesChat = "threadArchives.chat"

func (a *Adapter) loadThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	if a == nil || a.ctx == nil || a.ctx.Store == nil {
		return map[string]int64{}, nil
	}
	value, err := a.ctx.Store.Get(ctx, prefThreadArchivesChat)
	if err != nil {
		return nil, err
	}
	return NormalizeThreadArchiveMap(value), nil
}

// PersistThreadArchivedState writes thread archive marker to preference storage.
func (a *Adapter) PersistThreadArchivedState(
	ctx context.Context,
	threadID string,
	archivedAt int64,
) error {
	if a == nil || a.ctx == nil || a.ctx.Store == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	if archivedAt <= 0 {
		archivedAt = time.Now().UnixMilli()
	}
	return a.updateThreadArchiveMap(ctx, func(archivedMap map[string]int64) {
		archivedMap[id] = archivedAt
	})
}

// RemoveThreadArchivedState clears thread archive marker from preference storage.
func (a *Adapter) RemoveThreadArchivedState(
	ctx context.Context,
	threadID string,
) error {
	if a == nil || a.ctx == nil || a.ctx.Store == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	return a.updateThreadArchiveMap(ctx, func(archivedMap map[string]int64) {
		delete(archivedMap, id)
	})
}

func (a *Adapter) updateThreadArchiveMap(ctx context.Context, update func(map[string]int64)) error {
	if a == nil || a.ctx == nil || a.ctx.Store == nil {
		return nil
	}
	archivedMap, err := a.loadThreadArchiveMap(ctx)
	if err != nil {
		return err
	}
	if archivedMap == nil {
		archivedMap = map[string]int64{}
	}
	if update != nil {
		update(archivedMap)
	}
	return a.ctx.Store.Set(ctx, prefThreadArchivesChat, archivedMap)
}
