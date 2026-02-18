export const BACKEND_THREAD_STATUSES = Object.freeze([
  'idle',
  'starting',
  'thinking',
  'responding',
  'running',
  'editing',
  'waiting',
  'syncing',
  'error',
]);

const BACKEND_THREAD_STATUS_SET = new Set(BACKEND_THREAD_STATUSES);

const STATUS_LABEL_ZH = Object.freeze({
  idle: '空闲',
  starting: '启动中',
  thinking: '思考中',
  responding: '回复中',
  running: '执行中',
  editing: '修改中',
  waiting: '等待确认',
  syncing: '同步中',
  error: '异常',
});

export function normalizeStatus(state) {
  const s = (state || '').toString().toLowerCase().trim();
  if (!s) return 'idle';
  if (BACKEND_THREAD_STATUS_SET.has(s)) return s;
  return 'idle';
}

export function statusLabel(state) {
  return STATUS_LABEL_ZH[normalizeStatus(state)] || STATUS_LABEL_ZH.idle;
}
