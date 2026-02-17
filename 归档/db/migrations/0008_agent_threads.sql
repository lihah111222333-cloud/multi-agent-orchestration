-- 0008_agent_threads.sql — Codex HTTP API 线程注册表。
--
-- codex http-api 启动时将 thread/port/pid 写入此表,
-- Go Agent 通过查此表实现多实例自动发现。

CREATE TABLE IF NOT EXISTS agent_threads (
    thread_id       TEXT    PRIMARY KEY,
    prompt          TEXT    NOT NULL,
    model           TEXT,
    cwd             TEXT,
    status          TEXT    NOT NULL DEFAULT 'running',
    port            INTEGER NOT NULL,
    pid             INTEGER NOT NULL,
    created_at      BIGINT  NOT NULL,
    updated_at      BIGINT  NOT NULL,
    finished_at     BIGINT,
    last_event_type TEXT,
    error_message   TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_threads_status ON agent_threads (status);
CREATE INDEX IF NOT EXISTS idx_agent_threads_port   ON agent_threads (port);
