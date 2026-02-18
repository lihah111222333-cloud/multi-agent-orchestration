import { reactive, computed } from '../../lib/vue.esm-browser.prod.js';
import { callAPI } from '../services/api.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';
import {
  defaultLayoutForMode,
  normalizeChatLayout,
  normalizeCmdLayout,
  resolveMainAgent,
  deriveChatAgents,
  deriveCmdAgents,
  pickMostRecentAgent,
} from './thread-view.model.js';
import {
  buildLoadedThreadMap,
  buildLoadedStateMap,
  isThreadLoadedForSend,
  upsertLoadedThread,
  choosePreferredActiveThreadId,
} from './thread-send.guard.js';
import {
  normalizeStatus,
  statusFromEventType,
  isAssistantDeltaEvent,
  isReasoningDeltaEvent,
  extractEventText,
} from '../services/status.js';

const ACTIVE_THREAD_KEY = 'agent-orchestrator.chat.activeAgentId';
const ACTIVE_CMD_THREAD_KEY = 'agent-orchestrator.cmd.activeAgentId';
const MAIN_AGENT_KEY = 'agent-orchestrator.mainAgentId';
const AGENT_META_KEY = 'agent-orchestrator.agentMeta.v1';
const CHAT_LAYOUT_KEY = 'agent-orchestrator.layout.chat.v1';
const CMD_LAYOUT_KEY = 'agent-orchestrator.layout.cmd.v1';
const AGENT_META_MAX_BYTES = 512 * 1024;
const AGENT_META_MAX_ENTRIES = 240;

const state = reactive({
  threads: [],
  statuses: {},
  timelinesByThread: {},
  diffTextByThread: {},
  workspaceRunsByKey: {},
  workspaceFeatureEnabled: null,
  workspaceLastError: '',
  activeThreadId: localStorage.getItem(ACTIVE_THREAD_KEY) || '',
  activeCmdThreadId: localStorage.getItem(ACTIVE_CMD_THREAD_KEY) || '',
  mainAgentId: localStorage.getItem(MAIN_AGENT_KEY) || '',
  agentMetaById: loadAgentMeta(),
  viewPrefs: {
    chat: parseStorageJSON(CHAT_LAYOUT_KEY, {
      layout: defaultLayoutForMode('chat'),
      splitRatio: 64,
    }),
    cmd: parseStorageJSON(CMD_LAYOUT_KEY, {
      layout: defaultLayoutForMode('cmd'),
      splitRatio: 56,
      cardCols: 3,
    }),
  },
  loadingThreads: false,
  sending: false,
  loadedThreadIds: {},
  loadedThreadStates: {},
  loadedThreadListReady: false,
});

const runtimeByThread = {};
const inflightMessagesByThread = {};
const recentMessageLoadAtByThread = {};
const historyLoadedByThread = {};
const MESSAGE_LOAD_COOLDOWN_MS = 800;
const AGENT_META_PERSIST_DEBOUNCE_MS = 400;
const AGENT_ACTIVE_TOUCH_MS = 1200;
const AGENT_META_PRUNE_THRESHOLD = 240;
const AGENT_EVENT_LOG_SAMPLE = 120;
const lastActiveTouchByThread = {};
let persistMetaTimer = 0;
let agentEventSeq = 0;
let bridgeEventSeq = 0;

function perfNow() {
  if (typeof performance !== 'undefined' && typeof performance.now === 'function') {
    return performance.now();
  }
  return Date.now();
}

function nowISO() {
  return new Date().toISOString();
}

function uid(prefix) {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function parseStorageJSON(key, fallback) {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return fallback;
    const value = JSON.parse(raw);
    return value && typeof value === 'object' ? value : fallback;
  } catch (error) {
    logWarn('thread', 'storage.parse.failed', {
      key,
      error,
    });
    return fallback;
  }
}

function loadAgentMeta() {
  try {
    const raw = localStorage.getItem(AGENT_META_KEY);
    if (!raw) return {};
    if (raw.length > AGENT_META_MAX_BYTES) {
      localStorage.removeItem(AGENT_META_KEY);
      logWarn('thread', 'agentMeta.discarded.tooLarge', {
        raw_bytes: raw.length,
      });
      return {};
    }

    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {};
    }

    const keys = Object.keys(parsed);
    if (keys.length <= AGENT_META_MAX_ENTRIES) {
      return parsed;
    }

    const keep = keys
      .sort((a, b) => {
        const aMeta = parsed[a] || {};
        const bMeta = parsed[b] || {};
        const aAlias = (aMeta.alias || '').toString().trim();
        const bAlias = (bMeta.alias || '').toString().trim();
        if (aAlias && !bAlias) return -1;
        if (!aAlias && bAlias) return 1;
        const aTs = Date.parse(aMeta.lastActiveAt || '') || 0;
        const bTs = Date.parse(bMeta.lastActiveAt || '') || 0;
        return bTs - aTs;
      })
      .slice(0, AGENT_META_MAX_ENTRIES);

    const next = {};
    for (const key of keep) {
      next[key] = parsed[key];
    }
    persistJSON(AGENT_META_KEY, next);
    return next;
  } catch (error) {
    localStorage.removeItem(AGENT_META_KEY);
    logWarn('thread', 'agentMeta.load.failed', { error });
    return {};
  }
}

function persistJSON(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value ?? {}));
  } catch (error) {
    logWarn('thread', 'storage.persist.failed', {
      key,
      error,
    });
  }
}

function schedulePersistAgentMeta() {
  if (persistMetaTimer) return;
  persistMetaTimer = window.setTimeout(() => {
    persistMetaTimer = 0;
    persistJSON(AGENT_META_KEY, state.agentMetaById);
  }, AGENT_META_PERSIST_DEBOUNCE_MS);
}

function saveActiveThread(id) {
  const next = id || '';
  if (state.activeThreadId === next) return;
  const prev = state.activeThreadId || '';
  state.activeThreadId = next;
  localStorage.setItem(ACTIVE_THREAD_KEY, state.activeThreadId);
  logDebug('thread', 'activeChat.changed', {
    from: prev,
    to: next,
  });
}

function saveActiveCmdThread(id) {
  const next = id || '';
  if (state.activeCmdThreadId === next) return;
  const prev = state.activeCmdThreadId || '';
  state.activeCmdThreadId = next;
  localStorage.setItem(ACTIVE_CMD_THREAD_KEY, state.activeCmdThreadId);
  logDebug('thread', 'activeCmd.changed', {
    from: prev,
    to: next,
  });
}

function setMainAgent(threadId) {
  const id = (threadId || '').toString();
  if (state.mainAgentId === id) {
    return;
  }
  const prev = state.mainAgentId;
  state.mainAgentId = id;
  localStorage.setItem(MAIN_AGENT_KEY, id);
  for (const key of Object.keys(state.agentMetaById)) {
    const prev = state.agentMetaById[key] || {};
    state.agentMetaById[key] = { ...prev, isMain: id ? key === id : false };
  }
  schedulePersistAgentMeta();
  logInfo('thread', 'mainAgent.changed', {
    previous: prev,
    current: id,
  });
}

function setAgentAlias(threadId, alias) {
  const id = (threadId || '').toString();
  if (!id) return;
  const normalized = (alias || '').toString().trim();
  const prev = state.agentMetaById[id] || {};
  state.agentMetaById[id] = { ...prev, alias: normalized };
  persistJSON(AGENT_META_KEY, state.agentMetaById);
  logInfo('thread', 'alias.changed', {
    thread_id: id,
    alias: normalized,
  });

  const target = state.threads.find((item) => item.id === id);
  if (target && normalized) {
    target.name = normalized;
  } else if (target && !normalized) {
    target.name = target.id;
  }
}

async function renameThread(threadId, name) {
  const id = (threadId || '').toString();
  const nextName = (name || '').toString().trim();
  if (!id || !nextName) return;
  try {
    await callAPI('thread/name/set', { threadId: id, name: nextName });
  } catch (error) {
    logWarn('thread', 'rename.remote.failed', {
      thread_id: id,
      error,
    });
  }
  setAgentAlias(id, nextName);
}

function markAgentActive(threadId, iso = nowISO()) {
  const id = (threadId || '').toString();
  if (!id) return;
  const now = Date.now();
  const last = lastActiveTouchByThread[id] || 0;
  const prev = state.agentMetaById[id] || {};
  if (now-last < AGENT_ACTIVE_TOUCH_MS) {
    if ((id === state.mainAgentId || prev.isMain === true) !== !!prev.isMain) {
      state.agentMetaById[id] = {
        ...prev,
        isMain: id === state.mainAgentId || prev.isMain === true,
      };
      schedulePersistAgentMeta();
    }
    return;
  }
  lastActiveTouchByThread[id] = now;
  state.agentMetaById[id] = {
    ...prev,
    lastActiveAt: iso,
    isMain: id === state.mainAgentId || prev.isMain === true,
  };
  schedulePersistAgentMeta();
}

function ensureModePrefs(mode) {
  if (mode === 'cmd') {
    if (!state.viewPrefs.cmd || typeof state.viewPrefs.cmd !== 'object') {
      state.viewPrefs.cmd = {};
    }
    const current = state.viewPrefs.cmd;
    const nextLayout = normalizeCmdLayout(current.layout);
    const nextSplitRatio = normalizeSplitRatio(current.splitRatio);
    const nextCardCols = normalizeCmdCardCols(current.cardCols);
    if (current.layout !== nextLayout) current.layout = nextLayout;
    if (current.splitRatio !== nextSplitRatio) current.splitRatio = nextSplitRatio;
    if (current.cardCols !== nextCardCols) current.cardCols = nextCardCols;
    return;
  }
  if (!state.viewPrefs.chat || typeof state.viewPrefs.chat !== 'object') {
    state.viewPrefs.chat = {};
  }
  const current = state.viewPrefs.chat;
  const nextLayout = normalizeChatLayout(current.layout);
  const nextSplitRatio = normalizeSplitRatio(current.splitRatio);
  if (current.layout !== nextLayout) current.layout = nextLayout;
  if (current.splitRatio !== nextSplitRatio) current.splitRatio = nextSplitRatio;
}

function normalizeSplitRatio(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return 60;
  return Math.max(30, Math.min(75, Math.round(n)));
}

function normalizeCmdCardCols(value) {
  return Number(value) === 2 ? 2 : 3;
}

function getLayout(mode) {
  ensureModePrefs(mode);
  return mode === 'cmd'
    ? state.viewPrefs.cmd.layout
    : state.viewPrefs.chat.layout;
}

function setLayout(mode, layout) {
  ensureModePrefs(mode);
  if (mode === 'cmd') {
    state.viewPrefs.cmd.layout = normalizeCmdLayout(layout);
    persistJSON(CMD_LAYOUT_KEY, state.viewPrefs.cmd);
    return;
  }
  state.viewPrefs.chat.layout = normalizeChatLayout(layout);
  persistJSON(CHAT_LAYOUT_KEY, state.viewPrefs.chat);
}

function getSplitRatio(mode) {
  ensureModePrefs(mode);
  return mode === 'cmd'
    ? normalizeSplitRatio(state.viewPrefs.cmd.splitRatio)
    : normalizeSplitRatio(state.viewPrefs.chat.splitRatio);
}

function setSplitRatio(mode, ratio) {
  ensureModePrefs(mode);
  const next = normalizeSplitRatio(ratio);
  if (mode === 'cmd') {
    state.viewPrefs.cmd.splitRatio = next;
    persistJSON(CMD_LAYOUT_KEY, state.viewPrefs.cmd);
    return;
  }
  state.viewPrefs.chat.splitRatio = next;
  persistJSON(CHAT_LAYOUT_KEY, state.viewPrefs.chat);
}

function getCmdCardCols() {
  ensureModePrefs('cmd');
  return normalizeCmdCardCols(state.viewPrefs.cmd.cardCols);
}

function setCmdCardCols(cols) {
  ensureModePrefs('cmd');
  state.viewPrefs.cmd.cardCols = normalizeCmdCardCols(cols);
  persistJSON(CMD_LAYOUT_KEY, state.viewPrefs.cmd);
}

function pruneAgentMetaForThreads(threads) {
  const meta = state.agentMetaById || {};
  const keys = Object.keys(meta);
  if (keys.length === 0) return 0;
  if (keys.length <= AGENT_META_PRUNE_THRESHOLD) return 0;

  const keep = new Set((threads || []).map((item) => (item?.id || '').toString()).filter(Boolean));
  const activeChat = (state.activeThreadId || '').toString();
  const activeCmd = (state.activeCmdThreadId || '').toString();
  const main = (state.mainAgentId || '').toString();
  if (activeChat) keep.add(activeChat);
  if (activeCmd) keep.add(activeCmd);
  if (main) keep.add(main);

  const next = {};
  for (const key of keys) {
    if (keep.has(key)) {
      next[key] = meta[key];
    }
  }

  const removed = keys.length - Object.keys(next).length;
  if (removed <= 0) return 0;
  state.agentMetaById = next;
  schedulePersistAgentMeta();
  return removed;
}

function getThreadsByMode(mode) {
  if (mode === 'cmd') {
    return deriveCmdAgents({
      threads: state.threads,
      mainAgentId: state.mainAgentId,
    });
  }
  return deriveChatAgents({ threads: state.threads });
}

function getCurrentThreadId(mode) {
  if (mode === 'cmd') {
    const visible = getThreadsByMode('cmd');
    const visibleIds = new Set(visible.map((item) => item.id));
    if (state.activeCmdThreadId && visibleIds.has(state.activeCmdThreadId)) {
      return state.activeCmdThreadId;
    }
    return pickMostRecentAgent({
      threads: visible,
      meta: state.agentMetaById,
    }) || visible[0]?.id || '';
  }
  return state.activeThreadId;
}

function displayName(thread) {
  if (!thread?.id) return '';
  const alias = (state.agentMetaById[thread.id]?.alias || '').toString().trim();
  return alias || thread.name || thread.id;
}

function ensureThreadState(threadId) {
  if (!state.timelinesByThread[threadId]) {
    state.timelinesByThread[threadId] = [];
  }
  if (state.diffTextByThread[threadId] == null) {
    state.diffTextByThread[threadId] = '';
  }
  if (!runtimeByThread[threadId]) {
    runtimeByThread[threadId] = {
      thinkingIndex: -1,
      assistantIndex: -1,
      commandIndex: -1,
      planIndex: -1,
      editingFiles: {},
    };
  }
}

function resetRuntime(threadId) {
  runtimeByThread[threadId] = {
    thinkingIndex: -1,
    assistantIndex: -1,
    commandIndex: -1,
    planIndex: -1,
    editingFiles: {},
  };
}

function timeline(threadId) {
  ensureThreadState(threadId);
  return state.timelinesByThread[threadId];
}

function runtimeState(threadId) {
  ensureThreadState(threadId);
  return runtimeByThread[threadId];
}

function pushTimelineItem(threadId, item) {
  const list = timeline(threadId);
  list.push({
    id: uid(item.kind || 'item'),
    ts: nowISO(),
    ...item,
  });
  return list.length - 1;
}

function patchTimelineItem(threadId, index, patch) {
  const list = timeline(threadId);
  if (index < 0 || index >= list.length) return;
  list[index] = {
    ...list[index],
    ...patch,
  };
}

function appendUser(threadId, text, attachments = []) {
  if (!threadId) return;
  if (!text && attachments.length === 0) return;
  pushTimelineItem(threadId, {
    kind: 'user',
    text: text || '',
    attachments,
  });
}

function startThinking(threadId) {
  const rt = runtimeState(threadId);
  if (rt.thinkingIndex >= 0) return;
  rt.thinkingIndex = pushTimelineItem(threadId, {
    kind: 'thinking',
    text: '',
    done: false,
  });
}

function appendThinking(threadId, delta) {
  if (!delta) return;
  const rt = runtimeState(threadId);
  if (rt.thinkingIndex < 0) {
    startThinking(threadId);
  }
  const list = timeline(threadId);
  const current = list[rt.thinkingIndex];
  if (!current) return;
  patchTimelineItem(threadId, rt.thinkingIndex, {
    text: `${current.text || ''}${delta}`,
  });
}

function finishThinking(threadId) {
  const rt = runtimeState(threadId);
  if (rt.thinkingIndex < 0) return;
  const list = timeline(threadId);
  const item = list[rt.thinkingIndex];
  if (!item) {
    rt.thinkingIndex = -1;
    return;
  }

  if (!(item.text || '').trim()) {
    list.splice(rt.thinkingIndex, 1);
  } else {
    patchTimelineItem(threadId, rt.thinkingIndex, { done: true });
  }

  rt.thinkingIndex = -1;
}

function startAssistant(threadId) {
  finishThinking(threadId);
  const rt = runtimeState(threadId);
  if (rt.assistantIndex >= 0) return;
  rt.assistantIndex = pushTimelineItem(threadId, {
    kind: 'assistant',
    text: '',
  });
}

function appendAssistant(threadId, delta) {
  if (!delta) return;
  const rt = runtimeState(threadId);
  if (rt.assistantIndex < 0) {
    startAssistant(threadId);
  }
  const list = timeline(threadId);
  const current = list[rt.assistantIndex];
  if (!current) return;
  patchTimelineItem(threadId, rt.assistantIndex, {
    text: `${current.text || ''}${delta}`,
  });
}

function finishAssistant(threadId) {
  const rt = runtimeState(threadId);
  rt.assistantIndex = -1;
}

function startCommand(threadId, command) {
  finishThinking(threadId);
  const rt = runtimeState(threadId);
  rt.commandIndex = pushTimelineItem(threadId, {
    kind: 'command',
    command: command || '',
    output: '',
    status: 'running',
  });
}

function appendCommandOutput(threadId, output) {
  if (!output) return;
  const rt = runtimeState(threadId);
  if (rt.commandIndex < 0) {
    startCommand(threadId, '');
  }
  const list = timeline(threadId);
  const current = list[rt.commandIndex];
  if (!current) return;
  patchTimelineItem(threadId, rt.commandIndex, {
    output: `${current.output || ''}${output}`,
  });
}

function finishCommand(threadId, exitCode) {
  const rt = runtimeState(threadId);
  if (rt.commandIndex < 0) return;
  const code = typeof exitCode === 'number' ? exitCode : 0;
  patchTimelineItem(threadId, rt.commandIndex, {
    status: code === 0 ? 'completed' : 'failed',
    exitCode: code,
  });
  rt.commandIndex = -1;
}

function fileEditing(threadId, file) {
  pushTimelineItem(threadId, {
    kind: 'file',
    file: file || '',
    status: 'editing',
  });
}

function fileSaved(threadId, file) {
  const list = timeline(threadId);
  for (let i = list.length - 1; i >= 0; i -= 1) {
    const item = list[i];
    if (item.kind === 'file' && item.status === 'editing' && (item.file === file || !file)) {
      patchTimelineItem(threadId, i, { status: 'saved' });
      return;
    }
  }
  pushTimelineItem(threadId, {
    kind: 'file',
    file: file || '',
    status: 'saved',
  });
}

function rememberEditingFiles(threadId, files = []) {
  const rt = runtimeState(threadId);
  for (const file of files) {
    const value = (file || '').toString().trim();
    if (!value) continue;
    rt.editingFiles[value] = true;
  }
}

function consumeEditingFiles(threadId) {
  const rt = runtimeState(threadId);
  const files = Object.keys(rt.editingFiles || {});
  rt.editingFiles = {};
  return files;
}

function flushEditingFilesAsSaved(threadId) {
  const files = consumeEditingFiles(threadId);
  for (const file of files) {
    fileSaved(threadId, file);
  }
}

function extractFilesFromPatchDelta(delta) {
  if (!delta || typeof delta !== 'string') return [];
  const files = [];
  const lines = delta.split('\n');
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    if (trimmed.startsWith('diff --git ')) {
      const parts = trimmed.split(/\s+/);
      if (parts.length >= 4) {
        const file = parts[3].replace(/^b\//, '').trim();
        if (file) files.push(file);
      }
      continue;
    }
    if (/^[MAD]\s+/.test(trimmed)) {
      const file = trimmed.slice(2).trim();
      if (file) files.push(file);
    }
  }
  return [...new Set(files)];
}

function normalizeFiles(value) {
  if (!value) return [];
  if (Array.isArray(value)) {
    return [...new Set(value.map((item) => (item || '').toString().trim()).filter(Boolean))];
  }
  if (typeof value === 'string') {
    const v = value.trim();
    return v ? [v] : [];
  }
  return [];
}

function appendToolCall(threadId, payload) {
  const tool = (payload?.tool || payload?.tool_name || '').toString();
  if (!tool) return;
  const file = (payload?.file || payload?.file_path || '').toString();
  const preview = (payload?.resultPreview || '').toString();
  const status = payload?.success === false ? 'failed' : 'ok';
  const elapsedMs = typeof payload?.elapsedMs === 'number' ? payload.elapsedMs : undefined;

  const list = timeline(threadId);
  const lastIndex = list.length - 1;
  const last = lastIndex >= 0 ? list[lastIndex] : null;
  if (last?.kind === 'tool' && last.tool === tool) {
    const canMerge = (!last.file && !!file)
      || (!last.preview && !!preview)
      || (typeof last.elapsedMs === 'undefined' && typeof elapsedMs === 'number');
    if (canMerge) {
      patchTimelineItem(threadId, lastIndex, {
        file: file || last.file || '',
        preview: preview || last.preview || '',
        status: status === 'failed' ? 'failed' : (last.status || status),
        elapsedMs: typeof elapsedMs === 'number' ? elapsedMs : last.elapsedMs,
      });
      return;
    }
  }

  pushTimelineItem(threadId, {
    kind: 'tool',
    tool,
    file,
    status,
    elapsedMs,
    preview,
  });
}

function showApproval(threadId, command) {
  pushTimelineItem(threadId, {
    kind: 'approval',
    command: command || '',
    status: 'pending',
  });
}

function appendPlan(threadId, delta) {
  if (!delta) return;
  const rt = runtimeState(threadId);
  if (rt.planIndex < 0) {
    rt.planIndex = pushTimelineItem(threadId, {
      kind: 'plan',
      text: '',
      done: false,
    });
  }
  const list = timeline(threadId);
  const current = list[rt.planIndex];
  if (!current) return;
  patchTimelineItem(threadId, rt.planIndex, {
    text: `${current.text || ''}${delta}`,
  });
}

function completeTurn(threadId) {
  finishThinking(threadId);
  finishAssistant(threadId);
  const rt = runtimeState(threadId);
  if (rt.commandIndex >= 0) {
    finishCommand(threadId, 0);
  }
  if (rt.planIndex >= 0) {
    patchTimelineItem(threadId, rt.planIndex, { done: true });
    rt.planIndex = -1;
  }
  flushEditingFilesAsSaved(threadId);
}

function addError(threadId, text) {
  pushTimelineItem(threadId, {
    kind: 'error',
    text: text || '发生错误',
  });
}

function setDiff(threadId, diffText) {
  ensureThreadState(threadId);
  state.diffTextByThread[threadId] = diffText || '';
}

function normalizeThread(item) {
  return {
    id: item?.id || '',
    name: item?.name || item?.id || '',
    state: normalizeStatus(item?.state || 'idle'),
  };
}

function normalizeWorkspaceRunStatus(status) {
  const raw = (status || '').toString().trim().toLowerCase();
  if (!raw) return 'active';
  switch (raw) {
    case 'active':
    case 'merging':
    case 'merged':
    case 'aborted':
    case 'failed':
      return raw;
    default:
      return raw;
  }
}

function asNumber(value, fallback = 0) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function normalizeWorkspaceRun(raw, fallback = {}) {
  if (!raw || typeof raw !== 'object') return null;
  const runKey = (raw.run_key || raw.runKey || fallback.runKey || '').toString().trim();
  if (!runKey) return null;

  const status = normalizeWorkspaceRunStatus(raw.status || fallback.status || 'active');
  const createdAt = raw.created_at || raw.createdAt || fallback.createdAt || '';
  const updatedAt = raw.updated_at || raw.updatedAt || raw.finished_at || raw.finishedAt || fallback.updatedAt || '';
  const finishedAt = raw.finished_at || raw.finishedAt || fallback.finishedAt || '';

  return {
    runKey,
    dagKey: (raw.dag_key || raw.dagKey || fallback.dagKey || '').toString(),
    sourceRoot: (raw.source_root || raw.sourceRoot || fallback.sourceRoot || '').toString(),
    workspacePath: (raw.workspace_path || raw.workspacePath || fallback.workspacePath || '').toString(),
    status,
    createdBy: (raw.created_by || raw.createdBy || fallback.createdBy || '').toString(),
    updatedBy: (raw.updated_by || raw.updatedBy || fallback.updatedBy || '').toString(),
    metadata: raw.metadata ?? fallback.metadata ?? null,
    createdAt: createdAt || '',
    updatedAt: updatedAt || createdAt || '',
    finishedAt: finishedAt || '',
    dryRun: typeof raw.dryRun === 'boolean' ? raw.dryRun : (typeof fallback.dryRun === 'boolean' ? fallback.dryRun : false),
    merged: asNumber(raw.merged, asNumber(fallback.merged, 0)),
    conflicts: asNumber(raw.conflicts, asNumber(fallback.conflicts, 0)),
    unchanged: asNumber(raw.unchanged, asNumber(fallback.unchanged, 0)),
    errors: asNumber(raw.errors, asNumber(fallback.errors, 0)),
    reason: (raw.reason || fallback.reason || '').toString(),
  };
}

function upsertWorkspaceRun(raw) {
  if (!raw || typeof raw !== 'object') return;
  const runKey = (raw.run_key || raw.runKey || '').toString().trim();
  if (!runKey) return;
  const prev = state.workspaceRunsByKey[runKey] || {};
  const candidate = normalizeWorkspaceRun(raw, prev);
  if (!candidate) return;
  state.workspaceRunsByKey[candidate.runKey] = candidate;
}

function applyWorkspaceMergeResult(result, fallbackRun = {}) {
  if (!result || typeof result !== 'object') return;
  const runKey = (result.runKey || result.run_key || fallbackRun.runKey || fallbackRun.run_key || '').toString().trim();
  if (!runKey) return;
  const current = state.workspaceRunsByKey[runKey] || {};
  const merged = normalizeWorkspaceRun({
    runKey,
    status: result.status || current.status || 'active',
    workspacePath: result.workspace || current.workspacePath,
    sourceRoot: result.sourceRoot || current.sourceRoot,
    dryRun: result.dryRun,
    merged: result.merged,
    conflicts: result.conflicts,
    unchanged: result.unchanged,
    errors: result.errors,
    finishedAt: result.finishedAt,
    updatedAt: result.finishedAt || nowISO(),
  }, current);
  if (!merged) return;
  state.workspaceRunsByKey[runKey] = merged;
}

function sortedWorkspaceRuns() {
  return Object.values(state.workspaceRunsByKey)
    .sort((left, right) => {
      const leftTs = Date.parse(left?.updatedAt || left?.createdAt || '') || 0;
      const rightTs = Date.parse(right?.updatedAt || right?.createdAt || '') || 0;
      return rightTs - leftTs;
    });
}

function sortByIDAsc(messages) {
  return [...messages].sort((a, b) => Number(a?.id || 0) - Number(b?.id || 0));
}

function extractHistoryContent(msg) {
  const raw = (msg?.content || '').toString();
  if (raw) return raw;

  let metadata = msg?.metadata;
  if (!metadata) return '';
  if (typeof metadata === 'string') {
    try {
      metadata = JSON.parse(metadata);
    } catch {
      return '';
    }
  }
  if (!metadata || typeof metadata !== 'object') return '';

  const keys = ['delta', 'content', 'message', 'text', 'command', 'diff'];
  for (const key of keys) {
    const value = metadata[key];
    if (typeof value === 'string' && value) return value;
  }

  const nested = metadata.msg;
  if (nested && typeof nested === 'object') {
    for (const key of keys) {
      const value = nested[key];
      if (typeof value === 'string' && value) return value;
    }
  }

  return '';
}

function extractHistoryMetadata(msg) {
  let metadata = msg?.metadata;
  if (!metadata) return {};
  if (typeof metadata === 'string') {
    try {
      metadata = JSON.parse(metadata);
    } catch {
      return {};
    }
  }
  return metadata && typeof metadata === 'object' ? metadata : {};
}

function isUserMessageEvent(role, eventType) {
  return role === 'user' || eventType === 'user_message' || eventType === 'item/usermessage';
}

function getHistorySourceHints(records) {
  const eventTypes = new Set((records || []).map((msg) =>
    (msg?.eventType || msg?.event_type || '').toString().trim().toLowerCase()
  ));

  const hasItemAgentDelta = eventTypes.has('item/agentmessage/delta');
  const hasAgentContentDelta = [
    'agent_message_content_delta',
    'codex/event/agent_message_content_delta',
    'agent/event/agent_message_content_delta',
  ].some((t) => eventTypes.has(t));

  const hasItemReasoningDelta = [
    'item/reasoning/textdelta',
    'item/reasoning/summarytextdelta',
  ].some((t) => eventTypes.has(t));

  const hasReasoningContentDelta = [
    'reasoning_content_delta',
    'codex/event/reasoning_content_delta',
    'agent/event/reasoning_content_delta',
  ].some((t) => eventTypes.has(t));

  const hasReasoningFinal = [
    'agent_reasoning',
    'codex/event/agent_reasoning',
    'agent/event/agent_reasoning',
  ].some((t) => eventTypes.has(t));

  return {
    preferItemAgentDelta: hasItemAgentDelta,
    preferAgentContentDelta: !hasItemAgentDelta && hasAgentContentDelta,
    preferReasoningFinal: hasReasoningFinal,
    preferItemReasoningDelta: hasItemReasoningDelta,
    preferReasoningContentDelta: !hasReasoningFinal && !hasItemReasoningDelta && hasReasoningContentDelta,
  };
}

function isReasoningHistoryEvent(eventType, hints) {
  if (hints?.preferReasoningFinal) {
    return false;
  }
  if (hints?.preferItemReasoningDelta) {
    return eventType === 'item/reasoning/textdelta' || eventType === 'item/reasoning/summarytextdelta';
  }
  if (hints?.preferReasoningContentDelta) {
    return eventType === 'reasoning_content_delta' ||
      eventType === 'codex/event/reasoning_content_delta' ||
      eventType === 'agent/event/reasoning_content_delta';
  }
  return eventType.includes('reasoning') && (eventType.includes('delta') || eventType.includes('summary'));
}

function isAssistantHistoryDelta(eventType, hints) {
  if (hints?.preferItemAgentDelta) {
    return eventType === 'item/agentmessage/delta';
  }
  if (hints?.preferAgentContentDelta) {
    return eventType === 'agent_message_content_delta' ||
      eventType === 'codex/event/agent_message_content_delta' ||
      eventType === 'agent/event/agent_message_content_delta';
  }
  return eventType === 'agent_message_delta' ||
    eventType === 'codex/event/agent_message_delta' ||
    eventType === 'agent/event/agent_message_delta' ||
    (eventType.includes('agent_message') && eventType.includes('delta')) ||
    (eventType.includes('agentmessage') && eventType.includes('delta'));
}

function isAssistantHistoryFinal(eventType) {
  return eventType === 'agent_message' ||
    eventType === 'codex/event/agent_message' ||
    eventType === 'agent/event/agent_message';
}

function isReasoningHistoryFinal(eventType) {
  return eventType === 'agent_reasoning' ||
    eventType === 'codex/event/agent_reasoning' ||
    eventType === 'agent/event/agent_reasoning';
}

function isHistoryDiffEvent(eventType) {
  return eventType === 'turn_diff' ||
    eventType === 'turn/diff/updated' ||
    eventType === 'codex/event/turn_diff' ||
    eventType === 'agent/event/turn_diff';
}

function isAssistantHistoryBoundary(eventType) {
  return eventType === 'agent_message_completed' ||
    eventType === 'codex/event/agent_message_completed' ||
    eventType === 'agent/event/agent_message_completed' ||
    eventType === 'turn_complete' ||
    eventType === 'turn/completed' ||
    eventType === 'idle' ||
    eventType === 'turn_started' ||
    eventType === 'turn/started' ||
    eventType.startsWith('item/commandexecution/') ||
    eventType.startsWith('item/filechange/') ||
    eventType.includes('exec_') ||
    eventType.includes('patch_');
}

function hydrateHistory(threadId, records) {
  ensureThreadState(threadId);
  state.timelinesByThread[threadId] = [];
  state.diffTextByThread[threadId] = '';
  resetRuntime(threadId);

  const ordered = sortByIDAsc(records || []);
  const hints = getHistorySourceHints(ordered);
  let assistantDeltaBuffer = '';

  const flushAssistant = () => {
    if (!assistantDeltaBuffer) return;
    appendAssistant(threadId, assistantDeltaBuffer);
    finishAssistant(threadId);
    assistantDeltaBuffer = '';
  };

  for (const msg of ordered) {
    const role = (msg?.role || '').toString().toLowerCase();
    const eventType = (msg?.eventType || msg?.event_type || '').toString().trim().toLowerCase();
    const content = extractHistoryContent(msg);
    const metadata = extractHistoryMetadata(msg);

    if (isUserMessageEvent(role, eventType)) {
      finishThinking(threadId);
      flushAssistant();
      if (content) appendUser(threadId, content);
      continue;
    }

    if (isReasoningHistoryEvent(eventType, hints)) {
      flushAssistant();
      if (content) appendThinking(threadId, content);
      continue;
    }

    if (isReasoningHistoryFinal(eventType)) {
      flushAssistant();
      finishThinking(threadId);
      if (content) {
        startThinking(threadId);
        appendThinking(threadId, content);
        finishThinking(threadId);
      }
      continue;
    }

    if (isAssistantHistoryFinal(eventType)) {
      finishThinking(threadId);
      if (!content) {
        flushAssistant();
      } else {
        assistantDeltaBuffer = '';
        appendAssistant(threadId, content);
        finishAssistant(threadId);
      }
      continue;
    }

    if (isAssistantHistoryDelta(eventType, hints) || (role === 'assistant' && content && !eventType.includes('reasoning') && !isAssistantHistoryFinal(eventType))) {
      if (content) assistantDeltaBuffer += content;
      continue;
    }

    if (isHistoryDiffEvent(eventType)) {
      if (content) setDiff(threadId, content);
      continue;
    }

    if (eventType === 'item/filechange/started' || eventType.endsWith('/item/filechange/started') || eventType === 'patch_apply_begin') {
      let files = normalizeFiles(metadata?.files);
      if (files.length === 0) files = normalizeFiles(metadata?.file);
      for (const file of files) fileEditing(threadId, file);
      rememberEditingFiles(threadId, files);
      continue;
    }

    if (eventType === 'item/filechange/outputdelta' || eventType.endsWith('/item/filechange/outputdelta') || eventType === 'patch_apply') {
      let files = normalizeFiles(metadata?.files);
      if (files.length === 0) files = normalizeFiles(metadata?.file);
      if (files.length === 0) files = extractFilesFromPatchDelta(content);
      for (const file of files) fileEditing(threadId, file);
      rememberEditingFiles(threadId, files);
      continue;
    }

    if (eventType === 'item/filechange/completed' || eventType.endsWith('/item/filechange/completed') || eventType === 'patch_apply_end') {
      let files = normalizeFiles(metadata?.files);
      if (files.length === 0) files = normalizeFiles(metadata?.file);
      if (files.length === 0) files = consumeEditingFiles(threadId);
      for (const file of files) fileSaved(threadId, file);
      continue;
    }

    if (eventType === 'turn_complete' || eventType === 'turn/completed' || eventType === 'idle') {
      flushEditingFilesAsSaved(threadId);
      continue;
    }

    if (eventType === 'dynamic-tool/called' || eventType.endsWith('/dynamic-tool/called') || eventType === 'dynamic_tool_call') {
      const payload = {
        tool: metadata?.tool || metadata?.tool_name || '',
        file: metadata?.file || metadata?.file_path || '',
        success: metadata?.success,
        elapsedMs: metadata?.elapsedMs,
        resultPreview: content || metadata?.resultPreview || '',
      };
      appendToolCall(threadId, payload);
      continue;
    }

    if (isAssistantHistoryBoundary(eventType)) {
      finishThinking(threadId);
      flushAssistant();
    }
  }

  finishThinking(threadId);
  flushAssistant();
}

function parsePayload(data) {
  if (!data) return {};
  if (typeof data === 'object') return data;
  if (typeof data === 'string') {
    try {
      return JSON.parse(data);
    } catch (error) {
      logWarn('event', 'payload.parse.failed', {
        error,
        raw_len: data.length,
        preview: data.slice(0, 200),
      });
      return { text: data };
    }
  }
  return {};
}

function updateThreadState(threadId, stateKey) {
  if (!threadId || !stateKey) return;
  const normalized = normalizeStatus(stateKey);
  state.statuses[threadId] = normalized;
  const target = state.threads.find((item) => item.id === threadId);
  if (target) target.state = normalized;
}

function handleAgentEvent(evt) {
  const threadId = evt?.agent_id || evt?.threadId || '';
  const eventType = (evt?.type || '').toString();
  if (!threadId || !eventType) return;
  const seq = ++agentEventSeq;
  const sampled = seq % AGENT_EVENT_LOG_SAMPLE === 0 || !eventType.toLowerCase().includes('delta');
  if (sampled) {
    logDebug('event', 'agent.received', {
      seq,
      thread_id: threadId,
      type: eventType,
    });
  }

  ensureThreadState(threadId);
  markAgentActive(threadId);

  const payload = parsePayload(evt?.data);
  const prevStatus = state.statuses[threadId] || 'idle';
  const nextStatus = statusFromEventType(eventType, payload);
  if (nextStatus) {
    updateThreadState(threadId, nextStatus);
    const normalizedNext = normalizeStatus(nextStatus);
    if (prevStatus !== normalizedNext) {
      logInfo('thread', 'status.changed', {
        thread_id: threadId,
        from: prevStatus,
        to: normalizedNext,
        by_event: eventType,
      });
    }
  }

  switch (eventType) {
    case 'turn_started':
    case 'turn/started':
      completeTurn(threadId);
      startThinking(threadId);
      return;
    case 'idle':
    case 'turn_complete':
    case 'turn/completed':
      completeTurn(threadId);
      return;
    case 'agent_message_completed':
      finishAssistant(threadId);
      return;
    case 'exec_command_begin': {
      const command = (payload?.command || '').toString().trim();
      if (command) startCommand(threadId, command);
      return;
    }
    case 'item/started': {
      const command = (payload?.command || '').toString().trim();
      if (command) startCommand(threadId, command);
      const subType = (payload?.type || payload?.item_type || payload?.name || '').toString().toLowerCase();
      if (subType.includes('filechange') || subType.includes('file_change')) {
        let files = normalizeFiles(payload?.files);
        if (files.length === 0) files = normalizeFiles(payload?.file);
        if (files.length === 0) files = extractFilesFromPatchDelta((payload?.delta || payload?.output || '').toString());
        for (const file of files) {
          fileEditing(threadId, file);
        }
        rememberEditingFiles(threadId, files);
      }
      return;
    }
    case 'exec_command_end':
      finishCommand(threadId, payload?.exit_code);
      return;
    case 'item/completed':
      if (typeof payload?.exit_code !== 'undefined') {
        finishCommand(threadId, payload.exit_code);
      }
      {
        const subType = (payload?.type || payload?.item_type || payload?.name || '').toString().toLowerCase();
        if (subType.includes('filechange') || subType.includes('file_change')) {
          let files = normalizeFiles(payload?.files);
          if (files.length === 0) files = normalizeFiles(payload?.file);
          if (files.length === 0) files = consumeEditingFiles(threadId);
          for (const file of files) {
            fileSaved(threadId, file);
          }
        }
      }
      return;
    case 'patch_apply_begin':
    case 'item/fileChange/started': {
      let files = normalizeFiles(payload?.files);
      if (files.length === 0) files = normalizeFiles(payload?.file);
      for (const file of files) {
        fileEditing(threadId, file);
      }
      rememberEditingFiles(threadId, files);
      return;
    }
    case 'agent/event/item/fileChange/started': {
      let files = normalizeFiles(payload?.files);
      if (files.length === 0) files = normalizeFiles(payload?.file);
      for (const file of files) {
        fileEditing(threadId, file);
      }
      rememberEditingFiles(threadId, files);
      return;
    }
    case 'item/fileChange/outputDelta': {
      let files = normalizeFiles(payload?.files);
      if (files.length === 0) files = normalizeFiles(payload?.file);
      if (files.length === 0) files = extractFilesFromPatchDelta((payload?.delta || payload?.output || '').toString());
      for (const file of files) {
        fileEditing(threadId, file);
      }
      rememberEditingFiles(threadId, files);
      return;
    }
    case 'agent/event/item/fileChange/outputDelta': {
      let files = normalizeFiles(payload?.files);
      if (files.length === 0) files = normalizeFiles(payload?.file);
      if (files.length === 0) files = extractFilesFromPatchDelta((payload?.delta || payload?.output || '').toString());
      for (const file of files) {
        fileEditing(threadId, file);
      }
      rememberEditingFiles(threadId, files);
      return;
    }
    case 'patch_apply_end':
    case 'item/fileChange/completed': {
      let files = normalizeFiles(payload?.files);
      if (files.length === 0) files = normalizeFiles(payload?.file);
      if (files.length === 0) files = consumeEditingFiles(threadId);
      for (const file of files) {
        fileSaved(threadId, file);
      }
      return;
    }
    case 'agent/event/item/fileChange/completed': {
      let files = normalizeFiles(payload?.files);
      if (files.length === 0) files = normalizeFiles(payload?.file);
      if (files.length === 0) files = consumeEditingFiles(threadId);
      for (const file of files) {
        fileSaved(threadId, file);
      }
      return;
    }
    case 'dynamic-tool/called':
      appendToolCall(threadId, payload);
      return;
    case 'turn_diff':
    case 'turn/diff/updated':
      if (payload?.diff) setDiff(threadId, payload.diff);
      return;
    case 'exec_approval_request':
    case 'item/commandExecution/requestApproval':
      showApproval(threadId, payload?.command || '');
      return;
    case 'plan_delta':
    case 'item/plan/delta':
      appendPlan(threadId, payload?.delta || payload?.content || '');
      return;
    case 'error':
      addError(threadId, payload?.message || extractEventText(payload));
      return;
    default:
      break;
  }

  if (isReasoningDeltaEvent(eventType)) {
    appendThinking(threadId, extractEventText(payload));
    return;
  }

  if (isAssistantDeltaEvent(eventType)) {
    appendAssistant(threadId, extractEventText(payload));
    return;
  }

  if (eventType === 'agent_message' || eventType === 'codex/event/agent_message') {
    appendAssistant(threadId, extractEventText(payload));
    finishAssistant(threadId);
    return;
  }

  if (eventType === 'item/userMessage' || eventType === 'codex/event/user_message') {
    appendUser(threadId, extractEventText(payload));
    return;
  }

  if (eventType === 'exec_output_delta' || eventType === 'exec_command_output_delta' || eventType === 'item/commandExecution/outputDelta') {
    appendCommandOutput(threadId, extractEventText(payload));
  }
}

function handleBridgeEvent(evt) {
  const eventType = (evt?.type || evt?.method || '').toString();
  if (!eventType) return;
  const seq = ++bridgeEventSeq;
  if (seq % AGENT_EVENT_LOG_SAMPLE === 0 || !eventType.toLowerCase().includes('delta')) {
    logDebug('event', 'bridge.received', {
      seq,
      type: eventType,
    });
  }

  const payload = evt?.payload != null
    ? parsePayload(evt.payload)
    : parsePayload(evt?.data);

  switch (eventType) {
    case 'workspace/run/created':
      state.workspaceFeatureEnabled = true;
      state.workspaceLastError = '';
      upsertWorkspaceRun(payload?.run || payload);
      return;
    case 'workspace/run/merged':
      state.workspaceFeatureEnabled = true;
      state.workspaceLastError = '';
      applyWorkspaceMergeResult(payload?.result || payload, payload?.run || {});
      return;
    case 'workspace/run/aborted': {
      state.workspaceFeatureEnabled = true;
      state.workspaceLastError = '';
      const run = normalizeWorkspaceRun(payload?.run || payload);
      if (!run) return;
      run.reason = (payload?.reason || run.reason || '').toString();
      upsertWorkspaceRun(run);
      return;
    }
    default:
      return;
  }
}

async function refreshWorkspaceRuns(limit = 100) {
  const start = perfNow();
  try {
    const res = await callAPI('workspace/run/list', { limit });
    const runs = Array.isArray(res?.runs) ? res.runs : [];
    const next = {};
    for (const raw of runs) {
      if (!raw || typeof raw !== 'object') continue;
      const runKey = (raw.run_key || raw.runKey || '').toString().trim();
      if (!runKey) continue;
      const previous = state.workspaceRunsByKey[runKey] || {};
      const normalized = normalizeWorkspaceRun(raw, previous);
      if (!normalized) continue;
      next[normalized.runKey] = normalized;
    }
    state.workspaceRunsByKey = next;
    state.workspaceFeatureEnabled = true;
    state.workspaceLastError = '';
    logDebug('thread', 'workspaceRuns.refreshed', {
      count: Object.keys(next).length,
      duration_ms: Math.round(perfNow() - start),
    });
  } catch (error) {
    const message = (error?.message || error || '').toString();
    if (message.includes('workspace manager not initialized') || message.includes('method not found')) {
      state.workspaceFeatureEnabled = false;
      state.workspaceLastError = '';
      logWarn('thread', 'workspaceRuns.unavailable', {
        error: message,
        duration_ms: Math.round(perfNow() - start),
      });
      return;
    }
    state.workspaceLastError = message;
    logWarn('thread', 'workspaceRuns.refresh.failed', {
      error: message,
      duration_ms: Math.round(perfNow() - start),
    });
  }
}

async function refreshThreads() {
  const start = perfNow();
  state.loadingThreads = true;
  try {
    const [res, loadedRes] = await Promise.all([
      callAPI('thread/list', {}),
      callAPI('thread/loaded/list', {}).catch((error) => {
        logWarn('thread', 'loadedList.refresh.failed', { error });
        return null;
      }),
    ]);

    const loadedThreads = Array.isArray(loadedRes?.threads) ? loadedRes.threads : null;
    if (loadedThreads) {
      state.loadedThreadIds = buildLoadedThreadMap(loadedThreads);
      state.loadedThreadStates = buildLoadedStateMap(loadedThreads);
      state.loadedThreadListReady = true;
    } else {
      state.loadedThreadIds = {};
      state.loadedThreadStates = {};
      state.loadedThreadListReady = false;
    }

    state.threads = (res?.threads || []).map(normalizeThread);
    pruneAgentMetaForThreads(state.threads);
    for (const thread of state.threads) {
      ensureThreadState(thread.id);
      if (!state.statuses[thread.id]) {
        state.statuses[thread.id] = thread.state;
      }
      const alias = (state.agentMetaById[thread.id]?.alias || '').toString().trim();
      if (alias) {
        thread.name = alias;
      }
    }

    const resolvedMain = resolveMainAgent({
      mainAgentId: state.mainAgentId,
      threads: state.threads,
      meta: state.agentMetaById,
    });
    setMainAgent(resolvedMain);

    const preferredChatActive = choosePreferredActiveThreadId({
      currentActiveId: state.activeThreadId,
      threads: state.threads,
      loadedThreadMap: state.loadedThreadIds,
    });
    if (preferredChatActive !== (state.activeThreadId || '')) {
      saveActiveThread(preferredChatActive || '');
    }
    if (!state.activeThreadId && state.threads.length > 0) {
      saveActiveThread(preferredChatActive || state.threads[0].id);
    }

    const cmdThreads = getThreadsByMode('cmd');
    const preferredCmdActive = choosePreferredActiveThreadId({
      currentActiveId: state.activeCmdThreadId,
      threads: cmdThreads,
      loadedThreadMap: state.loadedThreadIds,
    });
    if (preferredCmdActive !== (state.activeCmdThreadId || '')) {
      saveActiveCmdThread(preferredCmdActive || '');
    }
    if (!state.activeCmdThreadId && cmdThreads.length > 0) {
      saveActiveCmdThread(preferredCmdActive || getCurrentThreadId('cmd'));
    }
    logDebug('thread', 'list.refreshed', {
      count: state.threads.length,
      active_chat: state.activeThreadId,
      active_cmd: state.activeCmdThreadId,
      main_agent: state.mainAgentId,
      duration_ms: Math.round(perfNow() - start),
    });
  } catch (error) {
    logWarn('thread', 'list.refresh.failed', {
      error,
      duration_ms: Math.round(perfNow() - start),
    });
  } finally {
    state.loadingThreads = false;
  }
}

async function startThread(cwd = '.', options = {}) {
  const start = perfNow();
  const res = await callAPI('thread/start', { cwd });
  const id = res?.thread?.id;
  if (!id) return '';

  state.threads.unshift({ id, name: id, state: 'starting' });
  state.statuses[id] = 'starting';
  upsertLoadedThread(state.loadedThreadIds, state.loadedThreadStates, id, 'starting');
  state.loadedThreadListReady = true;
  ensureThreadState(id);
  const focusMode = options?.focusMode === 'cmd' ? 'cmd' : 'chat';
  if (focusMode === 'cmd') {
    saveActiveCmdThread(id);
  } else {
    saveActiveThread(id);
  }
  if (!state.mainAgentId) {
    setMainAgent(id);
  }
  logInfo('thread', 'start.done', {
    thread_id: id,
    focus_mode: focusMode,
    cwd,
    duration_ms: Math.round(perfNow() - start),
  });
  return id;
}

async function launchBatch(count, cwd = '.', options = {}) {
  const total = Math.max(1, Math.min(Number(count) || 1, 32));
  for (let i = 0; i < total; i += 1) {
    await startThread(cwd, options);
  }
}

async function stopThread(threadId) {
  if (!threadId) return;
  const start = perfNow();
  try {
    await callAPI('thread/abort', { threadId });
  } catch {
    // ignore remote error and update UI optimistically
  }
  updateThreadState(threadId, 'idle');
  logInfo('thread', 'stop.done', {
    thread_id: threadId,
    duration_ms: Math.round(perfNow() - start),
  });
}

async function loadMessages(threadId, limit = 300, options = {}) {
  if (!threadId) return;
  const start = perfNow();
  const force = options?.force === true;
  if (!force && historyLoadedByThread[threadId]) {
    logDebug('thread', 'messages.skip.cached', { thread_id: threadId });
    return;
  }
  if (inflightMessagesByThread[threadId]) {
    logDebug('thread', 'messages.skip.inflight', { thread_id: threadId });
    return inflightMessagesByThread[threadId];
  }

  const now = Date.now();
  const last = recentMessageLoadAtByThread[threadId] || 0;
  if (now - last < MESSAGE_LOAD_COOLDOWN_MS) {
    logDebug('thread', 'messages.skip.cooldown', {
      thread_id: threadId,
      elapsed_ms: now - last,
    });
    return;
  }
  recentMessageLoadAtByThread[threadId] = now;

  const task = callAPI('thread/messages', { threadId, limit })
    .then((res) => {
      hydrateHistory(threadId, res?.messages || []);
      historyLoadedByThread[threadId] = true;
      logInfo('thread', 'messages.loaded', {
        thread_id: threadId,
        count: Array.isArray(res?.messages) ? res.messages.length : 0,
        duration_ms: Math.round(perfNow() - start),
      });
    })
    .catch((error) => {
      logWarn('thread', 'messages.load.failed', {
        thread_id: threadId,
        error,
        duration_ms: Math.round(perfNow() - start),
      });
      throw error;
    })
    .finally(() => {
      delete inflightMessagesByThread[threadId];
    });

  inflightMessagesByThread[threadId] = task;
  return task;
}

function formatAttachmentForTimeline(attachments) {
  return (attachments || []).map((item) => ({
    kind: item.kind,
    name: item.name,
    path: item.path,
    previewUrl: item.previewUrl || '',
  }));
}

function buildTurnInput(prompt, attachments = []) {
  const input = [];
  const text = (prompt || '').trim();
  if (text) {
    input.push({ type: 'text', text });
  }

  for (const item of attachments) {
    const path = (item?.path || '').trim();
    if (!path) continue;
    if (item.kind === 'image') {
      input.push({ type: 'localImage', path });
    } else {
      input.push({ type: 'fileContent', path });
    }
  }
  return input;
}

async function sendMessage(threadId, prompt, attachments = []) {
  const text = (prompt || '').trim();
  const hasAttachments = attachments.length > 0;
  if (!threadId || (!text && !hasAttachments)) return;

  if (state.loadedThreadListReady && !isThreadLoadedForSend(state.loadedThreadIds, threadId)) {
    const message = '当前会话未加载到运行器，后端无法接收消息。请新建会话或选择已加载会话。';
    logWarn('thread', 'send.blocked.unloaded', {
      thread_id: threadId,
      loaded_count: Object.keys(state.loadedThreadIds || {}).length,
    });
    addError(threadId, message);
    updateThreadState(threadId, 'idle');
    throw new Error(message);
  }

  const start = perfNow();
  logInfo('thread', 'send.start', {
    thread_id: threadId,
    text_len: text.length,
    attachments: attachments.length,
  });
  appendUser(threadId, text, formatAttachmentForTimeline(attachments));
  updateThreadState(threadId, 'thinking');
  state.sending = true;
  try {
    await callAPI('turn/start', {
      threadId,
      input: buildTurnInput(text, attachments),
    });
    logInfo('thread', 'send.done', {
      thread_id: threadId,
      duration_ms: Math.round(perfNow() - start),
    });
  } catch (error) {
    logWarn('thread', 'send.failed', {
      thread_id: threadId,
      error,
      duration_ms: Math.round(perfNow() - start),
    });
    throw error;
  } finally {
    state.sending = false;
  }
}

function clearThreadTimeline(threadId) {
  if (!threadId) return;
  state.timelinesByThread[threadId] = [];
  state.diffTextByThread[threadId] = '';
  resetRuntime(threadId);
  delete historyLoadedByThread[threadId];
}

function getThreadTimeline(threadId) {
  if (!threadId) return [];
  return state.timelinesByThread[threadId] || [];
}

function getThreadDiff(threadId) {
  if (!threadId) return '';
  return state.diffTextByThread[threadId] || '';
}

function getThreadStatus(threadId) {
  if (!threadId) return 'idle';
  return state.statuses[threadId] || 'idle';
}

function promptRenameThread(threadId) {
  const id = (threadId || '').toString();
  if (!id) return;
  const target = state.threads.find((item) => item.id === id);
  const current = displayName(target || { id });
  const next = window.prompt('输入新的 Agent 名称', current);
  if (!next || !next.trim()) return;
  renameThread(id, next.trim()).catch((error) => {
    logWarn('thread', 'rename.failed', {
      thread_id: id,
      error,
    });
  });
}

export function useThreadStore() {
  return {
    state,
    threads: computed(() => state.threads),
    workspaceRuns: computed(() => sortedWorkspaceRuns()),
    activeThread: computed(() => state.threads.find((item) => item.id === state.activeThreadId) || null),
    activeStatus: computed(() => state.statuses[state.activeThreadId] || 'idle'),
    activeTimeline: computed(() => state.timelinesByThread[state.activeThreadId] || []),
    activeDiffText: computed(() => state.diffTextByThread[state.activeThreadId] || ''),
    saveActiveThread,
    refreshThreads,
    startThread,
    launchBatch,
    stopThread,
    loadMessages,
    sendMessage,
    clearThreadTimeline,
    handleAgentEvent,
    handleBridgeEvent,
    refreshWorkspaceRuns,
    saveActiveCmdThread,
    setMainAgent,
    renameThread,
    promptRenameThread,
    getLayout,
    setLayout,
    getSplitRatio,
    setSplitRatio,
    getCmdCardCols,
    setCmdCardCols,
    getThreadsByMode,
    getCurrentThreadId,
    getThreadTimeline,
    getThreadDiff,
    getThreadStatus,
    displayName,
  };
}
