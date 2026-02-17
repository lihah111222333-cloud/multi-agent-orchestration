// task_dag.go — 任务 DAG CRUD (表 task_dags + task_dag_nodes)。
// Python: agent_ops_store.py save_task_dag / save_dag_node / list_task_dags / update_dag_node_status / delete_task_dags
package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskDAGStore 任务 DAG 存储。
type TaskDAGStore struct{ BaseStore }

// NewTaskDAGStore 创建。
func NewTaskDAGStore(pool *pgxpool.Pool) *TaskDAGStore { return &TaskDAGStore{NewBaseStore(pool)} }

const dagCols = `id, dag_key, title, description, status, created_by,
	metadata, started_at, finished_at, created_at, updated_at`

const nodeCols = `id, dag_key, node_key, title, node_type, assigned_to,
	depends_on, status, command_ref, config, result,
	started_at, finished_at, created_at, updated_at`

// SaveDAG 创建或更新 DAG 主表 (对应 Python save_task_dag)。
func (s *TaskDAGStore) SaveDAG(ctx context.Context, d *TaskDAG) (*TaskDAG, error) {
	metaJSON, _ := json.Marshal(d.Metadata)
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

// ListDAGs 列表查询 (对应 Python list_task_dags)。
func (s *TaskDAGStore) ListDAGs(ctx context.Context, keyword, status string, limit int) ([]TaskDAG, error) {
	q := NewQueryBuilder().
		Eq("status", status).
		KeywordLike(keyword, "dag_key", "title", "description")
	sql, params := q.Build("SELECT "+dagCols+" FROM task_dags", "updated_at DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskDAG](rows)
}

// GetDAGDetail 获取 DAG + 所有节点 (对应 Python get_task_dag_detail)。
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

// DeleteDAGs 批量删除 DAG (级联删除节点, 对应 Python delete_task_dags)。
func (s *TaskDAGStore) DeleteDAGs(ctx context.Context, dagKeys []string) (int64, error) {
	// 先删节点
	_, _ = s.pool.Exec(ctx, "DELETE FROM task_dag_nodes WHERE dag_key = ANY($1::text[])", dagKeys)
	tag, err := s.pool.Exec(ctx, "DELETE FROM task_dags WHERE dag_key = ANY($1::text[])", dagKeys)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SaveNode 创建或更新节点 (对应 Python save_dag_node)。
func (s *TaskDAGStore) SaveNode(ctx context.Context, n *TaskDAGNode) (*TaskDAGNode, error) {
	depsJSON, _ := json.Marshal(n.DependsOn)
	cfgJSON, _ := json.Marshal(n.Config)
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

// UpdateNodeStatus 更新节点状态 (对应 Python update_dag_node_status)。
func (s *TaskDAGStore) UpdateNodeStatus(ctx context.Context, dagKey, nodeKey, status string, result any) (*TaskDAGNode, error) {
	resJSON, _ := json.Marshal(result)
	rows, err := s.pool.Query(ctx,
		`UPDATE task_dag_nodes SET status=$1, result=$2::jsonb, updated_at=NOW()
		 WHERE dag_key=$3 AND node_key=$4 RETURNING `+nodeCols,
		status, string(resJSON), dagKey, nodeKey)
	if err != nil {
		return nil, err
	}
	return collectOne[TaskDAGNode](rows)
}

// ListNodes 按 dag_key 查询所有节点。
func (s *TaskDAGStore) ListNodes(ctx context.Context, dagKey string) ([]TaskDAGNode, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+nodeCols+" FROM task_dag_nodes WHERE dag_key = $1 ORDER BY created_at", dagKey)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskDAGNode](rows)
}
