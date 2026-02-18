-- 0010_agent_messages_dedup.sql — agent_messages 幂等去重键。
-- 为幂等事件提供原子去重能力，避免重复通知导致重复落库。

ALTER TABLE agent_messages
    ADD COLUMN IF NOT EXISTS dedup_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS ux_agent_messages_agent_dedup
    ON agent_messages(agent_id, dedup_key)
    WHERE dedup_key <> '';
