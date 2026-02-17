-- Codex 记忆系统
-- 存储 Agent 的记忆 (fact / preference / instruction)

CREATE TABLE IF NOT EXISTS codex_memory (
    id BIGSERIAL PRIMARY KEY,
    agent_id TEXT NOT NULL DEFAULT '',
    thread_id TEXT NOT NULL DEFAULT '',
    memory_type TEXT NOT NULL DEFAULT 'fact',
    content TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'auto',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_codex_memory_agent_id ON codex_memory (agent_id);
CREATE INDEX IF NOT EXISTS idx_codex_memory_thread_id ON codex_memory (thread_id);
CREATE INDEX IF NOT EXISTS idx_codex_memory_type ON codex_memory (memory_type);
CREATE INDEX IF NOT EXISTS idx_codex_memory_created_at ON codex_memory (created_at DESC);
