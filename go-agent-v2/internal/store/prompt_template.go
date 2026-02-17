// prompt_template.go — 提示词模板 CRUD (表 prompt_templates + prompt_versions)。
// Python: agent_ops_store.py save_prompt_template / list_prompt_template_versions / rollback
package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PromptTemplateStore 提示词模板存储。
type PromptTemplateStore struct{ BaseStore }

// NewPromptTemplateStore 创建。
func NewPromptTemplateStore(pool *pgxpool.Pool) *PromptTemplateStore {
	return &PromptTemplateStore{NewBaseStore(pool)}
}

const ptCols = `id, prompt_key, title, agent_key, tool_name, prompt_text,
	variables, tags, description, enabled, created_by, updated_by, created_at, updated_at`

// Save 创建或更新 (UPSERT)。先保存旧版本快照。
func (s *PromptTemplateStore) Save(ctx context.Context, t *PromptTemplate) (*PromptTemplate, error) {
	// 版本快照: 若已存在，先写入 prompt_versions
	existing, _ := s.Get(ctx, t.PromptKey)
	if existing != nil {
		varsJSON, _ := json.Marshal(existing.Variables)
		tagsJSON, _ := json.Marshal(existing.Tags)
		_, _ = s.pool.Exec(ctx,
			`INSERT INTO prompt_versions (prompt_key, title, agent_key, tool_name, prompt_text,
			   variables, tags, enabled, created_by, updated_by, source_updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11)`,
			existing.PromptKey, existing.Title, existing.AgentKey, existing.ToolName,
			existing.PromptText, string(varsJSON), string(tagsJSON), existing.Enabled,
			existing.CreatedBy, existing.UpdatedBy, existing.UpdatedAt)
	}

	varsJSON, _ := json.Marshal(t.Variables)
	tagsJSON, _ := json.Marshal(t.Tags)
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
		string(varsJSON), string(tagsJSON), t.Description, t.Enabled,
		defaultStr(t.UpdatedBy, ""), defaultStr(t.UpdatedBy, ""))
	if err != nil {
		return nil, err
	}
	return collectOne[PromptTemplate](rows)
}

// Get 按 prompt_key 查询。
func (s *PromptTemplateStore) Get(ctx context.Context, promptKey string) (*PromptTemplate, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+ptCols+" FROM prompt_templates WHERE prompt_key = $1", promptKey)
	if err != nil {
		return nil, err
	}
	return collectOne[PromptTemplate](rows)
}

// List 列表查询。
func (s *PromptTemplateStore) List(ctx context.Context, agentKey, keyword string, limit int) ([]PromptTemplate, error) {
	q := NewQueryBuilder().
		Eq("agent_key", agentKey).
		KeywordLike(keyword, "prompt_key", "title", "prompt_text")
	sql, params := q.Build("SELECT "+ptCols+" FROM prompt_templates", "updated_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[PromptTemplate](rows)
}

// SetEnabled 启用/禁用。
func (s *PromptTemplateStore) SetEnabled(ctx context.Context, promptKey string, enabled bool, updatedBy string) error {
	return SetEnabledByKey(ctx, s.pool, "prompt_templates", "prompt_key", promptKey, updatedBy, enabled)
}

// Delete 删除模板。
func (s *PromptTemplateStore) Delete(ctx context.Context, promptKey string) error {
	return DeleteByKey(ctx, s.pool, "prompt_templates", "prompt_key", promptKey)
}

// DeleteBatch 批量删除 (对应 Python delete_prompt_templates)。
func (s *PromptTemplateStore) DeleteBatch(ctx context.Context, promptKeys []string) (int64, error) {
	return DeleteBatchByKeys(ctx, s.pool, "prompt_templates", "prompt_key", promptKeys)
}

// ListVersions 查询历史版本 (对应 Python list_prompt_template_versions)。
func (s *PromptTemplateStore) ListVersions(ctx context.Context, promptKey string, limit int) ([]PromptVersion, error) {
	q := NewQueryBuilder().Eq("prompt_key", promptKey)
	sql, params := q.Build(
		`SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
			variables, tags, enabled, created_by, updated_by, source_updated_at, created_at
		 FROM prompt_versions`,
		"created_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[PromptVersion](rows)
}

// Rollback 回滚到指定版本 (对应 Python rollback_prompt_template)。
func (s *PromptTemplateStore) Rollback(ctx context.Context, promptKey string, versionID int, updatedBy string) (*PromptTemplate, error) {
	// 读取目标版本
	vRows, err := s.pool.Query(ctx,
		`SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
			variables, tags, enabled, created_by, updated_by, source_updated_at, created_at
		 FROM prompt_versions WHERE prompt_key = $1 AND id = $2`,
		promptKey, versionID)
	if err != nil {
		return nil, err
	}
	ver, err := collectOne[PromptVersion](vRows)
	if err != nil || ver == nil {
		return nil, err
	}
	// 用版本内容覆盖当前
	return s.Save(ctx, &PromptTemplate{
		PromptKey:  ver.PromptKey,
		Title:      ver.Title,
		AgentKey:   ver.AgentKey,
		ToolName:   ver.ToolName,
		PromptText: ver.PromptText,
		Variables:  ver.Variables,
		Tags:       ver.Tags,
		Enabled:    ver.Enabled,
		UpdatedBy:  updatedBy,
	})
}
