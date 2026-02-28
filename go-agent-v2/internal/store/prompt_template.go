package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type PromptTemplateStore struct{ BaseStore }

func NewPromptTemplateStore(pool *pgxpool.Pool) *PromptTemplateStore {
	return &PromptTemplateStore{NewBaseStore(pool)}
}

const ptCols = `id, prompt_key, title, agent_key, tool_name, prompt_text,
	variables, tags, description, enabled, created_by, updated_by, created_at, updated_at`

func (s *PromptTemplateStore) Save(ctx context.Context, t *PromptTemplate) (*PromptTemplate, error) {
	if existing, _ := s.Get(ctx, t.PromptKey); existing != nil {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO prompt_versions (prompt_key, title, agent_key, tool_name, prompt_text,
			   variables, tags, enabled, created_by, updated_by, source_updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11)`,
			existing.PromptKey, existing.Title, existing.AgentKey, existing.ToolName,
			existing.PromptText, string(mustMarshalJSON(existing.Variables)), string(mustMarshalJSON(existing.Tags)),
			existing.Enabled,
			existing.CreatedBy, existing.UpdatedBy, existing.UpdatedAt); err != nil {
			logger.Warn("store: save prompt version failed", "prompt_key", existing.PromptKey, logger.FieldError, err)
		}
	}

	rows, err := s.pool.Query(ctx,
		`INSERT INTO prompt_templates (prompt_key, title, agent_key, tool_name, prompt_text,
		   variables, tags, description, enabled, created_by, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, NOW())
		 ON CONFLICT (prompt_key) DO UPDATE SET
		   title=EXCLUDED.title, agent_key=EXCLUDED.agent_key, tool_name=EXCLUDED.tool_name,
		   prompt_text=EXCLUDED.prompt_text, variables=EXCLUDED.variables, tags=EXCLUDED.tags,
		   description=EXCLUDED.description, enabled=EXCLUDED.enabled,
		   updated_by=EXCLUDED.updated_by, updated_at=NOW()
		 RETURNING `+ptCols,
		t.PromptKey, t.Title, t.AgentKey, t.ToolName, t.PromptText,
		string(mustMarshalJSON(t.Variables)), string(mustMarshalJSON(t.Tags)), t.Description, t.Enabled,
		defaultStr(t.UpdatedBy, ""), defaultStr(t.UpdatedBy, ""))
	if err != nil {
		return nil, err
	}
	return collectOne[PromptTemplate](rows)
}

func (s *PromptTemplateStore) Get(ctx context.Context, promptKey string) (*PromptTemplate, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+ptCols+" FROM prompt_templates WHERE prompt_key = $1", promptKey)
	if err != nil {
		return nil, err
	}
	return collectOne[PromptTemplate](rows)
}

func (s *PromptTemplateStore) List(ctx context.Context, agentKey, keyword string, limit int) ([]PromptTemplate, error) {
	q := NewQueryBuilder().Eq("agent_key", agentKey).KeywordLike(keyword, "prompt_key", "title", "prompt_text")
	sql, params := q.Build("SELECT "+ptCols+" FROM prompt_templates", "updated_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[PromptTemplate](rows)
}

func (s *PromptTemplateStore) SetEnabled(ctx context.Context, promptKey string, enabled bool, updatedBy string) error {
	return SetEnabledByKey(ctx, s.pool, "prompt_templates", "prompt_key", promptKey, updatedBy, enabled)
}

func (s *PromptTemplateStore) Delete(ctx context.Context, promptKey string) error {
	return DeleteByKey(ctx, s.pool, "prompt_templates", "prompt_key", promptKey)
}
