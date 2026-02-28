package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkspaceRunStore struct{ BaseStore }

func NewWorkspaceRunStore(pool *pgxpool.Pool) *WorkspaceRunStore {
	return &WorkspaceRunStore{NewBaseStore(pool)}
}

const workspaceRunCols = `id, run_key, dag_key, source_root, workspace_path, status,
	created_by, updated_by, metadata, created_at, updated_at, finished_at`

const workspaceRunFileCols = `id, run_key, relative_path, baseline_sha256, workspace_sha256,
	source_sha256_before, source_sha256_after, state, last_error, created_at, updated_at`

func (s *WorkspaceRunStore) SaveRun(ctx context.Context, run *WorkspaceRun) (*WorkspaceRun, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO workspace_runs (
			run_key, dag_key, source_root, workspace_path, status,
			created_by, updated_by, metadata, updated_at, finished_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, NOW(), $9)
		ON CONFLICT (run_key) DO UPDATE SET
			dag_key = EXCLUDED.dag_key,
			source_root = EXCLUDED.source_root,
			workspace_path = EXCLUDED.workspace_path,
			status = EXCLUDED.status,
			updated_by = EXCLUDED.updated_by,
			metadata = EXCLUDED.metadata,
			updated_at = NOW(),
			finished_at = EXCLUDED.finished_at
		RETURNING `+workspaceRunCols,
		run.RunKey,
		run.DagKey,
		run.SourceRoot,
		run.WorkspacePath,
		defaultStr(run.Status, "active"),
		run.CreatedBy,
		run.UpdatedBy,
		string(mustMarshalJSON(run.Metadata)),
		run.FinishedAt,
	)
	if err != nil {
		return nil, err
	}
	return collectOne[WorkspaceRun](rows)
}

func (s *WorkspaceRunStore) GetRun(ctx context.Context, runKey string) (*WorkspaceRun, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+workspaceRunCols+" FROM workspace_runs WHERE run_key = $1",
		runKey,
	)
	if err != nil {
		return nil, err
	}
	return collectOne[WorkspaceRun](rows)
}

func (s *WorkspaceRunStore) ListRuns(ctx context.Context, status, dagKey string, limit int) ([]WorkspaceRun, error) {
	q := NewQueryBuilder().
		Eq("status", status).
		Eq("dag_key", dagKey)
	sql, params := q.Build("SELECT "+workspaceRunCols+" FROM workspace_runs", "updated_at DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[WorkspaceRun](rows)
}

func (s *WorkspaceRunStore) updateRunStatus(ctx context.Context, runKey, expectedStatus, status, updatedBy string, metadata any) (*WorkspaceRun, error) {
	query := `
		UPDATE workspace_runs
		SET status = $1,
			updated_by = $2,
			metadata = $3::jsonb,
			updated_at = NOW(),
			finished_at = CASE
				WHEN $1 IN ('merged', 'aborted', 'failed') THEN NOW()
				WHEN $1 = 'active' THEN NULL
				ELSE finished_at
			END
		WHERE run_key = $4`
	args := []any{status, updatedBy, string(mustMarshalJSON(metadata)), runKey}
	if expectedStatus != "" {
		query += " AND status = $5"
		args = append(args, expectedStatus)
	}
	rows, err := s.pool.Query(ctx, query+"\nRETURNING "+workspaceRunCols, args...)
	if err != nil {
		return nil, err
	}
	return collectOne[WorkspaceRun](rows)
}

func (s *WorkspaceRunStore) UpdateRunStatus(ctx context.Context, runKey, status, updatedBy string, metadata any) (*WorkspaceRun, error) {
	return s.updateRunStatus(ctx, runKey, "", status, updatedBy, metadata)
}

func (s *WorkspaceRunStore) TryTransitionRunStatus(ctx context.Context, runKey, fromStatus, toStatus, updatedBy string, metadata any) (*WorkspaceRun, bool, error) {
	run, err := s.updateRunStatus(ctx, runKey, fromStatus, toStatus, updatedBy, metadata)
	if err != nil {
		return nil, false, err
	}
	if run == nil {
		return nil, false, nil
	}
	return run, true, nil
}

func (s *WorkspaceRunStore) SaveFile(ctx context.Context, f *WorkspaceRunFile) (*WorkspaceRunFile, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO workspace_run_files (
			run_key, relative_path, baseline_sha256, workspace_sha256,
			source_sha256_before, source_sha256_after, state, last_error, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (run_key, relative_path) DO UPDATE SET
			baseline_sha256 = EXCLUDED.baseline_sha256,
			workspace_sha256 = EXCLUDED.workspace_sha256,
			source_sha256_before = EXCLUDED.source_sha256_before,
			source_sha256_after = EXCLUDED.source_sha256_after,
			state = EXCLUDED.state,
			last_error = EXCLUDED.last_error,
			updated_at = NOW()
		RETURNING `+workspaceRunFileCols,
		f.RunKey,
		f.RelativePath,
		f.BaselineSHA256,
		f.WorkspaceSHA256,
		f.SourceSHA256Before,
		f.SourceSHA256After,
		defaultStr(f.State, "tracked"),
		f.LastError,
	)
	if err != nil {
		return nil, err
	}
	return collectOne[WorkspaceRunFile](rows)
}

func (s *WorkspaceRunStore) GetFile(ctx context.Context, runKey, relativePath string) (*WorkspaceRunFile, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+workspaceRunFileCols+" FROM workspace_run_files WHERE run_key = $1 AND relative_path = $2",
		runKey, relativePath,
	)
	if err != nil {
		return nil, err
	}
	return collectOne[WorkspaceRunFile](rows)
}

func (s *WorkspaceRunStore) ListFiles(ctx context.Context, runKey, state string, limit int) ([]WorkspaceRunFile, error) {
	q := NewQueryBuilder().
		Eq("run_key", runKey).
		Eq("state", state)
	sql, params := q.Build("SELECT "+workspaceRunFileCols+" FROM workspace_run_files", "updated_at DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[WorkspaceRunFile](rows)
}
