-- 0008_agent_messages.sql — Agent 消息持久化。
-- 记录所有 agent 事件消息, 支持前端按 agentID 加载历史。

CREATE TABLE IF NOT EXISTS agent_messages (
    id          BIGSERIAL   PRIMARY KEY,
    agent_id    TEXT        NOT NULL,
    role        TEXT        NOT NULL DEFAULT 'system',
    event_type  TEXT        NOT NULL,
    method      TEXT        NOT NULL DEFAULT '',
    content     TEXT        NOT NULL DEFAULT '',
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_messages_agent_id
    ON agent_messages(agent_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_messages_role
    ON agent_messages(agent_id, role);
