// command_card.go — 命令卡 CRUD (表 command_cards + command_card_versions)。
// Python: agent_ops_store.py save_command_card / list_command_cards / set_command_card_enabled / delete
package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CommandCardStore 命令卡存储。
type CommandCardStore struct{ BaseStore }

// NewCommandCardStore 创建。
func NewCommandCardStore(pool *pgxpool.Pool) *CommandCardStore {
	return &CommandCardStore{NewBaseStore(pool)}
}

const ccCols = `id, card_key, title, description, command_template,
	args_schema, risk_level, enabled, created_by, updated_by, created_at, updated_at`

// Save 创建或更新 (UPSERT, 先版本快照)。
func (s *CommandCardStore) Save(ctx context.Context, c *CommandCard) (*CommandCard, error) {
	// 版本快照
	existing, _ := s.Get(ctx, c.CardKey)
	if existing != nil {
		schemaJSON, _ := json.Marshal(existing.ArgsSchema)
		_, _ = s.pool.Exec(ctx,
			`INSERT INTO command_card_versions (card_key, title, description, command_template,
			   args_schema, risk_level, enabled, created_by, updated_by, source_updated_at)
			 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10)`,
			existing.CardKey, existing.Title, existing.Description, existing.CommandTemplate,
			string(schemaJSON), existing.RiskLevel, existing.Enabled,
			existing.CreatedBy, existing.UpdatedBy, existing.UpdatedAt)
	}

	schemaJSON, _ := json.Marshal(c.ArgsSchema)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO command_cards (card_key, title, description, command_template, args_schema,
		   risk_level, enabled, created_by, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, NOW())
		 ON CONFLICT (card_key) DO UPDATE SET
		   title=EXCLUDED.title, description=EXCLUDED.description,
		   command_template=EXCLUDED.command_template, args_schema=EXCLUDED.args_schema,
		   risk_level=EXCLUDED.risk_level, enabled=EXCLUDED.enabled,
		   updated_by=EXCLUDED.updated_by, updated_at=NOW()
		 RETURNING `+ccCols,
		c.CardKey, c.Title, c.Description, c.CommandTemplate, string(schemaJSON),
		defaultStr(c.RiskLevel, "normal"), c.Enabled,
		defaultStr(c.UpdatedBy, ""), defaultStr(c.UpdatedBy, ""))
	if err != nil {
		return nil, err
	}
	return collectOne[CommandCard](rows)
}

// Get 按 card_key 查询。
func (s *CommandCardStore) Get(ctx context.Context, cardKey string) (*CommandCard, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+ccCols+" FROM command_cards WHERE card_key = $1", cardKey)
	if err != nil {
		return nil, err
	}
	return collectOne[CommandCard](rows)
}

// List 列表查询 (含 run 统计)。
func (s *CommandCardStore) List(ctx context.Context, keyword string, limit int) ([]CommandCard, error) {
	q := NewQueryBuilder().
		KeywordLike(keyword, "c.card_key", "c.title", "c.description", "c.command_template")
	sql, params := q.Build(
		`SELECT c.id, c.card_key, c.title, c.description, c.command_template,
			c.args_schema, c.risk_level, c.enabled, c.created_by, c.updated_by,
			c.created_at, c.updated_at,
			stats.last_run_at, COALESCE(stats.run_count, 0) AS run_count
		 FROM command_cards AS c
		 LEFT JOIN (
			SELECT card_key,
				   MAX(COALESCE(created_at)) AS last_run_at,
				   COUNT(*)::BIGINT AS run_count
			FROM command_card_runs GROUP BY card_key
		 ) AS stats ON stats.card_key = c.card_key`,
		"c.updated_at DESC, c.id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[CommandCard](rows)
}

// SetEnabled 启用/禁用 (对应 Python set_command_card_enabled)。
func (s *CommandCardStore) SetEnabled(ctx context.Context, cardKey string, enabled bool, updatedBy string) error {
	return SetEnabledByKey(ctx, s.pool, "command_cards", "card_key", cardKey, updatedBy, enabled)
}

// Delete 删除单个。
func (s *CommandCardStore) Delete(ctx context.Context, cardKey string) error {
	return DeleteByKey(ctx, s.pool, "command_cards", "card_key", cardKey)
}

// DeleteBatch 批量删除 (对应 Python delete_command_cards)。
func (s *CommandCardStore) DeleteBatch(ctx context.Context, cardKeys []string) (int64, error) {
	return DeleteBatchByKeys(ctx, s.pool, "command_cards", "card_key", cardKeys)
}

// ListVersions 查询历史版本。
func (s *CommandCardStore) ListVersions(ctx context.Context, cardKey string, limit int) ([]CommandCardVersion, error) {
	q := NewQueryBuilder().Eq("card_key", cardKey)
	sql, params := q.Build(
		`SELECT id, card_key, title, description, command_template, args_schema,
			risk_level, enabled, created_by, updated_by, source_updated_at, created_at
		 FROM command_card_versions`,
		"created_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[CommandCardVersion](rows)
}
