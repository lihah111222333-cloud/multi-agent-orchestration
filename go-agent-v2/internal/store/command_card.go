package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type CommandCardStore struct{ BaseStore }

func NewCommandCardStore(pool *pgxpool.Pool) *CommandCardStore {
	return &CommandCardStore{NewBaseStore(pool)}
}

const ccCols = `id, card_key, title, description, command_template,
	args_schema, risk_level, enabled, created_by, updated_by, created_at, updated_at`

func (s *CommandCardStore) Save(ctx context.Context, c *CommandCard) (*CommandCard, error) {
	existing, _ := s.Get(ctx, c.CardKey)
	if existing != nil {
		schemaJSON := mustMarshalJSON(existing.ArgsSchema)
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO command_card_versions (card_key, title, description, command_template,
			   args_schema, risk_level, enabled, created_by, updated_by, source_updated_at)
			 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10)`,
			existing.CardKey, existing.Title, existing.Description, existing.CommandTemplate,
			string(schemaJSON), existing.RiskLevel, existing.Enabled,
			existing.CreatedBy, existing.UpdatedBy, existing.UpdatedAt); err != nil {
			logger.Warn("store: save command card version failed", "card_key", existing.CardKey, logger.FieldError, err)
		}
	}

	schemaJSON := mustMarshalJSON(c.ArgsSchema)
	updatedBy := defaultStr(c.UpdatedBy, "")
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
		defaultStr(c.RiskLevel, "normal"), c.Enabled, updatedBy, updatedBy)
	if err != nil {
		return nil, err
	}
	return collectOne[CommandCard](rows)
}

func (s *CommandCardStore) Get(ctx context.Context, cardKey string) (*CommandCard, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+ccCols+" FROM command_cards WHERE card_key = $1", cardKey)
	if err != nil {
		return nil, err
	}
	return collectOne[CommandCard](rows)
}

func (s *CommandCardStore) List(ctx context.Context, keyword string, limit int) ([]CommandCard, error) {
	sql, params := NewQueryBuilder().
		KeywordLike(keyword, "c.card_key", "c.title", "c.description", "c.command_template").
		Build(
			`SELECT c.id, c.card_key, c.title, c.description, c.command_template,
			c.args_schema, c.risk_level, c.enabled, c.created_by, c.updated_by,
			c.created_at, c.updated_at,
			stats.last_run_at, COALESCE(stats.run_count, 0) AS run_count
		 FROM command_cards AS c
		 LEFT JOIN (
			SELECT card_key,
				   MAX(created_at) AS last_run_at,
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

func (s *CommandCardStore) SetEnabled(ctx context.Context, cardKey string, enabled bool, updatedBy string) error {
	return SetEnabledByKey(ctx, s.pool, "command_cards", "card_key", cardKey, updatedBy, enabled)
}

func (s *CommandCardStore) Delete(ctx context.Context, cardKey string) error {
	return DeleteByKey(ctx, s.pool, "command_cards", "card_key", cardKey)
}
