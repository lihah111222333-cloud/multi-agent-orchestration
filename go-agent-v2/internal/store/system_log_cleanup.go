package store

import "context"

// CleanupSystemLogs 删除超过 retentionDays 天的系统日志，返回删除行数。
func (s *SystemLogStore) CleanupSystemLogs(ctx context.Context, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM system_logs WHERE ts < NOW() - ($1 || ' days')::INTERVAL`,
		retentionDays)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
