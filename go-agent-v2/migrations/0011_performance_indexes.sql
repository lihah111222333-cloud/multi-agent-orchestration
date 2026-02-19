-- 0011_performance_indexes.sql — 热路径查询索引优化。
--
-- agent_messages: ListByAgent 使用 ORDER BY id DESC 分页,
-- 现有索引 (agent_id, created_at DESC) 不覆盖, 添加 (agent_id, id DESC)。

CREATE INDEX IF NOT EXISTS idx_agent_messages_agent_id_id_desc
    ON agent_messages(agent_id, id DESC);
