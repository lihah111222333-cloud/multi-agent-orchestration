function normalizeEventType(eventType) {
  return (eventType || '').toString().trim().toLowerCase();
}

function isDeltaLikeEvent(eventType) {
  const normalized = normalizeEventType(eventType);
  if (!normalized) return false;
  return normalized.includes('delta');
}

export function shouldScheduleRuntimeSync({
  eventType = '',
  nowMs = Date.now(),
  lastSyncAtMs = 0,
  minIntervalMs = 0,
  timerActive = false,
} = {}) {
  if (timerActive) {
    return false;
  }

  if (!isDeltaLikeEvent(eventType)) {
    return true;
  }

  const now = Number.isFinite(nowMs) ? nowMs : Date.now();
  const last = Number.isFinite(lastSyncAtMs) ? lastSyncAtMs : 0;
  const minInterval = Math.max(0, Number(minIntervalMs) || 0);
  if (minInterval <= 0) {
    return true;
  }
  return now - last >= minInterval;
}
