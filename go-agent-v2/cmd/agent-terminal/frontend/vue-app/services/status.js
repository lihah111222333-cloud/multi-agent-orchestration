export const STATUS_LABEL_ZH = Object.freeze({
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
  const alias = {
    booting: 'starting',
    starting_up: 'starting',
    streaming: 'responding',
    executing: 'running',
    working: 'running',
    awaiting_approval: 'waiting',
    in_progress: 'running',
    completed: 'idle',
    success: 'idle',
    done: 'idle',
    resumed: 'idle',
    ready: 'idle',
    pending: 'waiting',
    failed: 'error',
    stopped: 'idle',
  };
  if (alias[s]) return alias[s];
  if (STATUS_LABEL_ZH[s]) return s;
  if (s.includes('error') || s.includes('fail')) return 'error';
  return 'idle';
}

export function statusLabel(state) {
  return STATUS_LABEL_ZH[normalizeStatus(state)] || STATUS_LABEL_ZH.idle;
}

export function statusFromEventType(type, data = {}) {
  const raw = (type || '').toString();
  if (!raw) return null;

  if (raw.startsWith('codex/event/')) {
    return statusFromEventType(raw.replace(/^codex\/event\//, ''), data);
  }
  if (raw.startsWith('agent/event/')) {
    return statusFromEventType(raw.replace(/^agent\/event\//, ''), data);
  }

  if (raw.startsWith('item/reasoning/')) return 'thinking';
  if (raw.startsWith('item/agentMessage/')) return 'responding';

  if (raw.startsWith('item/fileChange/')) {
    if (raw.endsWith('/requestApproval')) return 'waiting';
    if (raw.endsWith('/completed')) return 'responding';
    return 'editing';
  }

  if (raw.startsWith('item/commandExecution/')) {
    if (raw.endsWith('/requestApproval') || raw.endsWith('/terminalInteraction')) return 'waiting';
    if (raw.endsWith('/completed')) return 'responding';
    return 'running';
  }

  switch (raw) {
    case 'turn_started':
    case 'turn/started':
    case 'reasoning':
    case 'reasoning_delta':
    case 'agent_reasoning_delta':
    case 'agent_reasoning_raw_delta':
    case 'item/reasoning/textDelta':
      return 'thinking';
    case 'agent_message':
    case 'agent_message_delta':
    case 'agent_message_content_delta':
    case 'item/agentMessage/delta':
    case 'item/agentMessage/started':
      return 'responding';
    case 'thread/started':
      return 'starting';
    case 'exec_command_begin':
    case 'exec_output_delta':
    case 'exec_command_output_delta':
    case 'item/commandExecution/outputDelta':
      return 'running';
    case 'item/fileChange/started':
    case 'patch_apply_begin':
      return 'editing';
    case 'item/fileChange/completed':
    case 'patch_apply_end':
      return 'responding';
    case 'exec_approval_request':
    case 'item/commandExecution/requestApproval':
    case 'item/fileChange/requestApproval':
      return 'waiting';
    case 'mcpServer/startupUpdate':
    case 'mcp_startup_update':
      return 'syncing';
    case 'mcpServer/startupComplete':
    case 'mcp_startup_complete':
      return 'idle';
    case 'error':
      return 'error';
    case 'idle':
    case 'task_complete':
    case 'turn_completed':
    case 'turn_complete':
    case 'turn/completed':
      return 'idle';
    case 'item/started':
      return inferItemStatus(data);
    case 'item/completed':
      return 'responding';
    default:
      return null;
  }
}

function inferItemStatus(data) {
  const command = (data?.command || '').toString().trim();
  const file = (data?.file || '').toString().trim();
  const subType = (data?.type || data?.item_type || data?.name || '').toString().toLowerCase();
  if (command) return 'running';
  if (file) return 'editing';
  if (subType.includes('reason')) return 'thinking';
  if (subType.includes('message')) return 'responding';
  if (subType.includes('file')) return 'editing';
  if (subType.includes('approval')) return 'waiting';
  if (subType.includes('command') || subType.includes('tool') || subType.includes('mcp')) return 'running';
  return 'responding';
}

export function isAssistantDeltaEvent(type) {
  const t = (type || '').toString();
  return t === 'item/agentMessage/delta' ||
    t === 'agent_message_delta' ||
    t === 'agent_message_content_delta' ||
    t === 'codex/event/agent_message_delta' ||
    t === 'codex/event/agent_message_content_delta';
}

export function isReasoningDeltaEvent(type) {
  const t = (type || '').toString();
  return t === 'item/reasoning/textDelta' ||
    t === 'reasoning_delta' ||
    t === 'agent_reasoning_delta' ||
    t === 'agent_reasoning_raw_delta' ||
    t === 'codex/event/agent_reasoning_delta' ||
    t === 'codex/event/agent_reasoning_raw_delta';
}

export function extractEventText(payload) {
  const candidates = [
    payload?.delta,
    payload?.text,
    payload?.content,
    payload?.message,
    payload?.output,
    payload?.stdout,
    payload?.stderr,
    payload?.reasoning,
    payload?.summary,
    payload?.chunk,
  ];
  for (const item of candidates) {
    if (typeof item === 'string' && item.length > 0) return item;
  }
  return '';
}
