CREATE TABLE IF NOT EXISTS agent_status (
    agent_id TEXT PRIMARY KEY,
    agent_name TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unknown',
    stagnant_sec INT NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    output_tail JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_agent_status_status
        CHECK (status IN ('running', 'idle', 'stuck', 'error', 'disconnected', 'unknown')),
    CONSTRAINT chk_agent_status_stagnant_sec
        CHECK (stagnant_sec >= 0)
);

CREATE INDEX IF NOT EXISTS idx_agent_status_status_updated_at
    ON agent_status (status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_status_updated_at
    ON agent_status (updated_at DESC);
