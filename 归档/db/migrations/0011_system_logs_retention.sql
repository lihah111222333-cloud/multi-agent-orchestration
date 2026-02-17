-- 0011_system_logs_retention.sql — 日志保留策略。
--
-- 添加 cleanup 函数 + ts 索引用于高效删除旧日志。
-- 保留策略: 默认 30 天, 通过调用 cleanup_system_logs(interval) 执行。

-- ts 索引: 加速按时间范围删除
CREATE INDEX IF NOT EXISTS idx_system_logs_ts ON system_logs (ts);

-- cleanup 函数: 删除超过指定天数的日志
CREATE OR REPLACE FUNCTION cleanup_system_logs(retention_days INT DEFAULT 30)
RETURNS INT AS $$
DECLARE
    deleted INT;
BEGIN
    DELETE FROM system_logs WHERE ts < NOW() - (retention_days || ' days')::INTERVAL;
    GET DIAGNOSTICS deleted = ROW_COUNT;
    RETURN deleted;
END;
$$ LANGUAGE plpgsql;
