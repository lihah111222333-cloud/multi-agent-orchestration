import { reactive } from '../../lib/vue.esm-browser.prod.js';
import { callAPI } from '../services/api.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';
import {
  defaultLayoutForMode,
  normalizeChatLayout,
  normalizeCmdLayout,
  deriveChatAgents,
  deriveCmdAgents,
} from './thread-view.model.js';
import {
  assertThreadStoreStateWhitelist,
  THREAD_STORE_UI_LOCAL_STATE_WHITELIST,
  THREAD_STORE_RUNTIME_STATE_KEYS,
} from './thread-state-whitelist.js';
import {
  normalizeStatus,
} from '../services/status.js';

const PREF_ACTIVE_THREAD_ID = 'activeThreadId';
const PREF_ACTIVE_CMD_THREAD_ID = 'activeCmdThreadId';
const PREF_MAIN_AGENT_ID = 'mainAgentId';
const PREF_VIEW_CHAT = 'viewPrefs.chat';
const PREF_VIEW_CMD = 'viewPrefs.cmd';

const state = reactive({
  activeThreadId: '',
  activeCmdThreadId: '',
  mainAgentId: '',
  viewPrefs: {
    chat: {
      layout: defaultLayoutForMode('chat'),
      splitRatio: 64,
    },
    cmd: {
      layout: defaultLayoutForMode('cmd'),
      splitRatio: 56,
      cardCols: 3,
    },
  },
  loadingThreads: false,
  sending: false,
});

const runtimeRootState = reactive({
  threads: [],
  statuses: {},
  timelinesByThread: {},
  diffTextByThread: {},
  agentMetaById: {},
  agentRuntimeById: {},
});

for (const key of THREAD_STORE_RUNTIME_STATE_KEYS) {
  Object.defineProperty(state, key, {
    get() {
      return runtimeRootState[key];
    },
    set(value) {
      runtimeRootState[key] = value;
    },
    enumerable: false,
    configurable: false,
  });
}

assertThreadStoreStateWhitelist(state, 'thread-store.init');
logInfo('thread', 'state.whitelist.applied', {
  ui_local_keys: THREAD_STORE_UI_LOCAL_STATE_WHITELIST.length,
  runtime_accessor_keys: THREAD_STORE_RUNTIME_STATE_KEYS.length,
});

let runtimeSyncPromise = null;
let runtimeSyncPending = false;
const preferenceWriteQueueByKey = new Map();

function perfNow() {
  if (typeof performance !== 'undefined' && typeof performance.now === 'function') {
    return performance.now();
  }
  return Date.now();
}

function persistRemote(prefKey, value) {
  return callAPI('ui/preferences/set', { key: prefKey, value });
}

function persistPreferenceAndSync(prefKey, value, logMeta = {}) {
  const queueKey = (prefKey || '').toString();
  const prev = preferenceWriteQueueByKey.get(queueKey) || Promise.resolve();
  const current = prev
    .catch(() => { })
    .then(() => persistRemote(queueKey, value))
    .then(() => syncRuntimeState())
    .catch((error) => {
      logDebug('thread', 'prefs.persist.failed', { key: prefKey, error, ...logMeta });
    });
  preferenceWriteQueueByKey.set(queueKey, current);
  current.finally(() => {
    if (preferenceWriteQueueByKey.get(queueKey) === current) {
      preferenceWriteQueueByKey.delete(queueKey);
    }
  });
}

function saveActiveThread(id) {
  const next = id || '';
  if (state.activeThreadId === next) return;
  const prev = state.activeThreadId || '';
  persistPreferenceAndSync(PREF_ACTIVE_THREAD_ID, next, { previous: prev, current: next });
  logDebug('thread', 'activeChat.changed', {
    from: prev,
    to: next,
  });
}

function saveActiveCmdThread(id) {
  const next = id || '';
  if (state.activeCmdThreadId === next) return;
  const prev = state.activeCmdThreadId || '';
  persistPreferenceAndSync(PREF_ACTIVE_CMD_THREAD_ID, next, { previous: prev, current: next });
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
  persistPreferenceAndSync(PREF_MAIN_AGENT_ID, id, { previous: prev, current: id });
  logInfo('thread', 'mainAgent.changed', {
    previous: prev,
    current: id,
  });
}

async function renameThread(threadId, name) {
  const id = (threadId || '').toString();
  const nextName = (name || '').toString().trim();
  if (!id || !nextName) return;
  try {
    await callAPI('thread/name/set', { threadId: id, name: nextName });
    await syncRuntimeState();
  } catch (error) {
    logWarn('thread', 'rename.remote.failed', {
      thread_id: id,
      error,
    });
  }
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
    persistRemote(PREF_VIEW_CMD, state.viewPrefs.cmd);
    return;
  }
  state.viewPrefs.chat.layout = normalizeChatLayout(layout);
  persistRemote(PREF_VIEW_CHAT, state.viewPrefs.chat);
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
    persistRemote(PREF_VIEW_CMD, state.viewPrefs.cmd);
    return;
  }
  state.viewPrefs.chat.splitRatio = next;
  persistRemote(PREF_VIEW_CHAT, state.viewPrefs.chat);
}

function getCmdCardCols() {
  ensureModePrefs('cmd');
  return normalizeCmdCardCols(state.viewPrefs.cmd.cardCols);
}

function setCmdCardCols(cols) {
  ensureModePrefs('cmd');
  state.viewPrefs.cmd.cardCols = normalizeCmdCardCols(cols);
  persistRemote(PREF_VIEW_CMD, state.viewPrefs.cmd);
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
    return state.activeCmdThreadId || '';
  }
  return state.activeThreadId;
}

function displayName(thread) {
  if (!thread?.id) return '';
  const alias = (state.agentMetaById[thread.id]?.alias || '').toString().trim();
  return alias || thread.name || thread.id;
}

function normalizeThread(item) {
  return {
    id: item?.id || '',
    name: item?.name || item?.id || '',
    state: normalizeStatus(item?.state || 'idle'),
  };
}


function applyRuntimeSnapshot(snapshot) {
  const data = snapshot && typeof snapshot === 'object' ? snapshot : {};

  const nextThreads = Array.isArray(data.threads)
    ? data.threads.map(normalizeThread)
    : [];
  const nextStatuses = {};
  if (data.statuses && typeof data.statuses === 'object') {
    for (const [key, value] of Object.entries(data.statuses)) {
      nextStatuses[key] = normalizeStatus(value);
    }
  }
  for (const thread of nextThreads) {
    if (!nextStatuses[thread.id]) {
      nextStatuses[thread.id] = normalizeStatus(thread.state || 'idle');
    }
  }

  const nextTimelines = {};
  if (data.timelinesByThread && typeof data.timelinesByThread === 'object') {
    for (const [key, value] of Object.entries(data.timelinesByThread)) {
      nextTimelines[key] = Array.isArray(value) ? value : [];
    }
  }

  const nextDiffs = {};
  if (data.diffTextByThread && typeof data.diffTextByThread === 'object') {
    for (const [key, value] of Object.entries(data.diffTextByThread)) {
      nextDiffs[key] = (value || '').toString();
    }
  }

  state.threads = nextThreads;
  state.statuses = nextStatuses;
  state.timelinesByThread = nextTimelines;
  state.diffTextByThread = nextDiffs;
  state.agentMetaById = data.agentMetaById && typeof data.agentMetaById === 'object'
    ? data.agentMetaById
    : {};
  state.agentRuntimeById = data.agentRuntimeById && typeof data.agentRuntimeById === 'object'
    ? data.agentRuntimeById
    : {};
  if (Object.prototype.hasOwnProperty.call(data, PREF_ACTIVE_THREAD_ID)) {
    state.activeThreadId = (data[PREF_ACTIVE_THREAD_ID] || '').toString();
  }
  if (Object.prototype.hasOwnProperty.call(data, PREF_ACTIVE_CMD_THREAD_ID)) {
    state.activeCmdThreadId = (data[PREF_ACTIVE_CMD_THREAD_ID] || '').toString();
  }
  if (Object.prototype.hasOwnProperty.call(data, PREF_MAIN_AGENT_ID)) {
    state.mainAgentId = (data[PREF_MAIN_AGENT_ID] || '').toString();
  }
  if (data[PREF_VIEW_CHAT] && typeof data[PREF_VIEW_CHAT] === 'object') {
    state.viewPrefs.chat = data[PREF_VIEW_CHAT];
  }
  if (data[PREF_VIEW_CMD] && typeof data[PREF_VIEW_CMD] === 'object') {
    state.viewPrefs.cmd = data[PREF_VIEW_CMD];
  }
}

async function syncRuntimeState() {
  if (runtimeSyncPromise) {
    runtimeSyncPending = true;
    return runtimeSyncPromise;
  }

  runtimeSyncPromise = callAPI('ui/state/get', {})
    .then((res) => {
      applyRuntimeSnapshot(res || {});
    })
    .finally(() => {
      runtimeSyncPromise = null;
      if (!runtimeSyncPending) return;
      runtimeSyncPending = false;
      syncRuntimeState().catch(() => { });
    });
  return runtimeSyncPromise;
}

function handleAgentEvent() {
  // runtime sync is backend-driven by bridge event `ui/state/changed`
}

function handleBridgeEvent(evt) {
  const eventType = (evt?.type || evt?.method || '').toString();
  if (eventType !== 'ui/state/changed') return;
  syncRuntimeState().catch((error) => {
    logWarn('thread', 'state.sync.failed', { error, by_event: eventType });
  });
}


async function refreshThreads() {
  const start = perfNow();
  state.loadingThreads = true;
  try {
    await callAPI('thread/list', {});
    await syncRuntimeState();
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

  await syncRuntimeState();
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


async function stopThread(threadId) {
  if (!threadId) return;
  const start = perfNow();
  try {
    await callAPI('thread/abort', { threadId });
  } catch {
    // ignore remote error and update UI optimistically
  }
  syncRuntimeState().catch(() => { });
  logInfo('thread', 'stop.done', {
    thread_id: threadId,
    duration_ms: Math.round(perfNow() - start),
  });
}

async function loadMessages(threadId, limit = 300) {
  if (!threadId) return;
  const start = perfNow();
  try {
    const res = await callAPI('thread/messages', { threadId, limit });
    await syncRuntimeState();
    logInfo('thread', 'messages.loaded', {
      thread_id: threadId,
      count: Array.isArray(res?.messages) ? res.messages.length : 0,
      duration_ms: Math.round(perfNow() - start),
    });
  } catch (error) {
    logWarn('thread', 'messages.load.failed', {
      thread_id: threadId,
      error,
      duration_ms: Math.round(perfNow() - start),
    });
    throw error;
  }
}

async function sendMessage(threadId, prompt, attachments = []) {
  const text = (prompt || '').trim();
  const hasAttachments = attachments.length > 0;
  if (!threadId || (!text && !hasAttachments)) return;

  const input = [];
  let localImageCount = 0;
  let remoteImageCount = 0;
  let fileCount = 0;
  let droppedAttachmentCount = 0;
  if (text) {
    input.push({ type: 'text', text });
  }
  for (const item of attachments) {
    const path = (item?.path || '').trim();
    const previewUrl = (item?.previewUrl || '').trim();
    if (item?.kind === 'image') {
      if (path) {
        input.push({ type: 'localImage', path });
        localImageCount += 1;
        continue;
      }
      if (previewUrl) {
        input.push({ type: 'image', url: previewUrl });
        remoteImageCount += 1;
        continue;
      }
      droppedAttachmentCount += 1;
      continue;
    }
    if (!path) {
      droppedAttachmentCount += 1;
      continue;
    }
    input.push({ type: 'fileContent', path });
    fileCount += 1;
  }

  if (input.length === 0) {
    logWarn('thread', 'send.skipped.emptyInput', {
      thread_id: threadId,
      attachments: attachments.length,
      dropped_attachments: droppedAttachmentCount,
    });
    return;
  }

  const start = perfNow();
  logInfo('thread', 'send.start', {
    thread_id: threadId,
    text_len: text.length,
    attachments: attachments.length,
    local_images: localImageCount,
    inline_images: remoteImageCount,
    files: fileCount,
    dropped_attachments: droppedAttachmentCount,
  });
  state.sending = true;
  try {
    await callAPI('turn/start', { threadId, input });
    await syncRuntimeState();
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
    saveActiveThread,
    refreshThreads,
    startThread,

    stopThread,
    loadMessages,
    sendMessage,
    handleAgentEvent,
    handleBridgeEvent,

    saveActiveCmdThread,
    setMainAgent,
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
