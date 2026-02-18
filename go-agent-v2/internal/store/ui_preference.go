package store

import (
	"context"
	"encoding/json"

	stderrors "errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type UIPreferenceStore struct {
	pool *pgxpool.Pool
}

func NewUIPreferenceStore(pool *pgxpool.Pool) *UIPreferenceStore {
	return &UIPreferenceStore{pool: pool}
}

// Get retrieves a preference by key. Returns nil if not found.
func (s *UIPreferenceStore) Get(ctx context.Context, key string) (any, error) {
	var val json.RawMessage
	err := s.pool.QueryRow(ctx, "SELECT value FROM ui_preferences WHERE key = $1", key).Scan(&val)
	if err != nil {
		if stderrors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, apperrors.Wrap(err, "UIPreferenceStore.Get", "query preference")
	}

	var result any
	if err := json.Unmarshal(val, &result); err != nil {
		return nil, apperrors.Wrap(err, "UIPreferenceStore.Get", "unmarshal preference")
	}
	return result, nil
}

// Set saves a preference. Value is marshaled to JSON.
func (s *UIPreferenceStore) Set(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return apperrors.Wrap(err, "UIPreferenceStore.Set", "marshal preference")
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO ui_preferences (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = NOW()
	`, key, data)

	if err != nil {
		return apperrors.Wrap(err, "UIPreferenceStore.Set", "upsert preference")
	}
	return nil
}

// GetAll retrieves all preferences as a map.
func (s *UIPreferenceStore) GetAll(ctx context.Context) (map[string]any, error) {
	rows, err := s.pool.Query(ctx, "SELECT key, value FROM ui_preferences")
	if err != nil {
		return nil, apperrors.Wrap(err, "UIPreferenceStore.GetAll", "query preferences")
	}
	defer rows.Close()

	result := make(map[string]any)
	for rows.Next() {
		var key string
		var raw json.RawMessage
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, apperrors.Wrap(err, "UIPreferenceStore.GetAll", "scan preference")
		}

		var val any
		if err := json.Unmarshal(raw, &val); err != nil {
			// Skip malformed entries to prevent partial failures
			continue
		}
		result[key] = val
	}

	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap(err, "UIPreferenceStore.GetAll", "iterate preferences")
	}

	return result, nil
}
