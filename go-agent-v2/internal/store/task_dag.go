package store

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskDAGStore struct{ BaseStore }

func NewTaskDAGStore(pool *pgxpool.Pool) *TaskDAGStore { return &TaskDAGStore{NewBaseStore(pool)} }

const dagCols = `id, dag_key, title, description, status, created_by,
	metadata, started_at, finished_at, created_at, updated_at`

const nodeCols = `id, dag_key, node_key, title, node_type, assigned_to,
	depends_on, status, command_ref, config, result,
	started_at, finished_at, created_at, updated_at`

func (s *TaskDAGStore) SaveDAG(ctx context.Context, d *TaskDAG) (*TaskDAG, error) {
	metaJSON := mustMarshalJSON(d.Metadata)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO task_dags (dag_key, title, description, status, created_by, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		 ON CONFLICT (dag_key) DO UPDATE SET
		   title=EXCLUDED.title, description=EXCLUDED.description, status=EXCLUDED.status,
		   created_by=EXCLUDED.created_by, metadata=EXCLUDED.metadata, updated_at=NOW()
		 RETURNING `+dagCols,
		d.DagKey, d.Title, d.Description, defaultStr(d.Status, "draft"), d.CreatedBy, string(metaJSON))
	if err != nil {
		return nil, err
	}
	return collectOne[TaskDAG](rows)
}

func (s *TaskDAGStore) ListDAGs(ctx context.Context, keyword, status string, limit int) ([]TaskDAG, error) {
	sql, params := NewQueryBuilder().
		Eq("status", status).
		KeywordLike(keyword, "dag_key", "title", "description").
		Build("SELECT "+dagCols+" FROM task_dags", "updated_at DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskDAG](rows)
}

func (s *TaskDAGStore) GetDAGDetail(ctx context.Context, dagKey string) (*TaskDAG, []TaskDAGNode, error) {
	dagRows, err := s.pool.Query(ctx, "SELECT "+dagCols+" FROM task_dags WHERE dag_key = $1", dagKey)
	if err != nil {
		return nil, nil, err
	}
	dag, err := collectOne[TaskDAG](dagRows)
	if err != nil || dag == nil {
		return nil, nil, err
	}
	nodes, err := s.ListNodes(ctx, dagKey)
	return dag, nodes, err
}

func (s *TaskDAGStore) SaveNode(ctx context.Context, n *TaskDAGNode) (*TaskDAGNode, error) {
	depsJSON, cfgJSON := mustMarshalJSON(n.DependsOn), mustMarshalJSON(n.Config)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO task_dag_nodes (dag_key, node_key, title, node_type, assigned_to,
		   depends_on, command_ref, config)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8::jsonb)
		 ON CONFLICT (dag_key, node_key) DO UPDATE SET
		   title=EXCLUDED.title, node_type=EXCLUDED.node_type, assigned_to=EXCLUDED.assigned_to,
		   depends_on=EXCLUDED.depends_on, command_ref=EXCLUDED.command_ref,
		   config=EXCLUDED.config, updated_at=NOW()
		 RETURNING `+nodeCols,
		n.DagKey, n.NodeKey, n.Title, defaultStr(n.NodeType, "task"),
		n.AssignedTo, string(depsJSON), n.CommandRef, string(cfgJSON))
	if err != nil {
		return nil, err
	}
	return collectOne[TaskDAGNode](rows)
}

func (s *TaskDAGStore) UpdateNodeStatus(ctx context.Context, dagKey, nodeKey, status string, result any) (*TaskDAGNode, error) {
	resJSON := mustMarshalJSON(result)
	rows, err := s.pool.Query(ctx,
		`UPDATE task_dag_nodes SET status=$1, result=$2::jsonb, updated_at=NOW()
		 WHERE dag_key=$3 AND node_key=$4 RETURNING `+nodeCols,
		status, string(resJSON), dagKey, nodeKey)
	if err != nil {
		return nil, err
	}
	return collectOne[TaskDAGNode](rows)
}

func (s *TaskDAGStore) ListNodes(ctx context.Context, dagKey string) ([]TaskDAGNode, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+nodeCols+" FROM task_dag_nodes WHERE dag_key = $1 ORDER BY created_at", dagKey)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskDAGNode](rows)
}
