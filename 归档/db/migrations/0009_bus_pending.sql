-- 0009_bus_pending.sql — 总线降级消息暂存表。
--
-- 当 MessageBus 异常时, ResilientPublisher 将消息写入此表。
-- 后台恢复协程定期扫描, 恢复后补发并删除。

CREATE TABLE IF NOT EXISTS bus_pending (
    seq         BIGSERIAL PRIMARY KEY,
    topic       TEXT      NOT NULL,
    from_id     TEXT      NOT NULL DEFAULT 'system',
    to_id       TEXT      NOT NULL DEFAULT '*',
    msg_type    TEXT      NOT NULL,
    payload     JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bus_pending_created ON bus_pending (created_at);
