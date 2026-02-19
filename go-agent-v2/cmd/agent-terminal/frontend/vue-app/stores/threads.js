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
});

const runtimeRootState = reactive({
  threads: [],
  statuses: {},
  interruptibleByThread: {},
  viewPrefsChat: null,
  viewPrefsCmd: null,
  statusHeadersByThread: {},
  statusDetailsByThread: {},
  timelinesByThread: {},
  diffTextByThread: {},
  tokenUsageByThread: {},
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
    .then(() => { syncRuntimeState().catch(() => { }); })  // 非阻塞: 乐观更新已生效
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
  state.activeThreadId = next;                       // 乐观更新: 立即切换
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
  state.activeCmdThreadId = next;                    // 乐观更新: 立即切换
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

function normalizeSplitRatio(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return 60;
  return Math.max(30, Math.min(75, Math.round(n)));
}

function normalizeCmdCardCols(value) {
  return Number(value) === 2 ? 2 : 3;
}

function normalizeChatPrefs(value) {
  const input = value && typeof value === 'object' ? value : {};
  return {
    layout: normalizeChatLayout(input.layout || defaultLayoutForMode('chat')),
    splitRatio: normalizeSplitRatio(input.splitRatio),
  };
}

function normalizeCmdPrefs(value) {
  const input = value && typeof value === 'object' ? value : {};
  return {
    layout: normalizeCmdLayout(input.layout || defaultLayoutForMode('cmd')),
    splitRatio: normalizeSplitRatio(input.splitRatio),
    cardCols: normalizeCmdCardCols(input.cardCols),
  };
}

function readChatPrefs() {
  return normalizeChatPrefs(state.viewPrefsChat);
}

function readCmdPrefs() {
  return normalizeCmdPrefs(state.viewPrefsCmd);
}

function getLayout(mode) {
  return mode === 'cmd'
    ? readCmdPrefs().layout
    : readChatPrefs().layout;
}

function setLayout(mode, layout) {
  if (mode === 'cmd') {
    const current = readCmdPrefs();
    const next = {
      ...current,
      layout: normalizeCmdLayout(layout),
    };
    persistPreferenceAndSync(PREF_VIEW_CMD, next, {
      mode: 'cmd',
      field: 'layout',
    });
    return;
  }
  const current = readChatPrefs();
  const next = {
    ...current,
    layout: normalizeChatLayout(layout),
  };
  persistPreferenceAndSync(PREF_VIEW_CHAT, next, {
    mode: 'chat',
    field: 'layout',
  });
}

function getSplitRatio(mode) {
  return mode === 'cmd'
    ? readCmdPrefs().splitRatio
    : readChatPrefs().splitRatio;
}

function setSplitRatio(mode, ratio) {
  const next = normalizeSplitRatio(ratio);
  if (mode === 'cmd') {
    const current = readCmdPrefs();
    persistPreferenceAndSync(PREF_VIEW_CMD, {
      ...current,
      splitRatio: next,
    }, {
      mode: 'cmd',
      field: 'splitRatio',
    });
    return;
  }
  const current = readChatPrefs();
  persistPreferenceAndSync(PREF_VIEW_CHAT, {
    ...current,
    splitRatio: next,
  }, {
    mode: 'chat',
    field: 'splitRatio',
  });
}

function getCmdCardCols() {
  return readCmdPrefs().cardCols;
}

function setCmdCardCols(cols) {
  const current = readCmdPrefs();
  persistPreferenceAndSync(PREF_VIEW_CMD, {
    ...current,
    cardCols: normalizeCmdCardCols(cols),
  }, {
    mode: 'cmd',
    field: 'cardCols',
  });
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



// 增量合并 helper: 只更新 string 值变化的 key
function mergeStringMap(target, source) {
  if (!source || typeof source !== 'object') return;
  for (const [key, value] of Object.entries(source)) {
    const str = (value || '').toString();
    if (target[key] !== str) target[key] = str;
  }
}

// 增量合并 helper: 用 JSON.stringify 对比的 object map
function mergeObjectMap(target, source) {
  if (!source || typeof source !== 'object') return;
  for (const [key, value] of Object.entries(source)) {
    const next = value && typeof value === 'object' ? value : {};
    if (JSON.stringify(target[key]) !== JSON.stringify(next)) {
      Object.freeze(next);
      target[key] = next;
    }
  }
}

function applyRuntimeSnapshot(snapshot) {
  const data = snapshot && typeof snapshot === 'object' ? snapshot : {};

  // --- threads: 只在列表长度或 ID 变化时替换 ---
  const nextThreads = Array.isArray(data.threads)
    ? data.threads.map(normalizeThread)
    : [];
  const oldIds = state.threads.map((t) => t.id).join(',');
  const newIds = nextThreads.map((t) => t.id).join(',');
  if (oldIds !== newIds) {
    state.threads = nextThreads;
  }

  // --- statuses: 增量合并, 只更新变化的 key ---
  if (data.statuses && typeof data.statuses === 'object') {
    for (const [key, value] of Object.entries(data.statuses)) {
      const normalized = normalizeStatus(value);
      if (state.statuses[key] !== normalized) {
        state.statuses[key] = normalized;
      }
    }
  }
  for (const thread of nextThreads) {
    if (!state.statuses[thread.id]) {
      state.statuses[thread.id] = normalizeStatus(thread.state || 'idle');
    }
  }

  // --- interruptibleByThread: 增量合并 ---
  if (data.interruptibleByThread && typeof data.interruptibleByThread === 'object') {
    for (const [key, value] of Object.entries(data.interruptibleByThread)) {
      const b = Boolean(value);
      if (state.interruptibleByThread[key] !== b) {
        state.interruptibleByThread[key] = b;
      }
    }
  }

  // --- timelinesByThread: freeze items + 只在变化时替换 ---
  if (data.timelinesByThread && typeof data.timelinesByThread === 'object') {
    for (const [key, value] of Object.entries(data.timelinesByThread)) {
      const newItems = Array.isArray(value) ? value : [];
      const oldItems = state.timelinesByThread[key];
      // 快速对比: 长度相同 + 最后一条 item 的 id/content 相同 → 跳过
      if (
        oldItems &&
        oldItems.length === newItems.length &&
        oldItems.length > 0 &&
        oldItems[oldItems.length - 1]?.id === newItems[newItems.length - 1]?.id
      ) {
        continue;
      }
      // freeze 每条 item, 阻止 Vue 深层追踪
      for (let i = 0; i < newItems.length; i++) {
        if (newItems[i] && typeof newItems[i] === 'object') {
          Object.freeze(newItems[i]);
        }
      }
      state.timelinesByThread[key] = Object.freeze(newItems);
    }
  }

  // --- diffTextByThread: 增量合并 ---
  if (data.diffTextByThread && typeof data.diffTextByThread === 'object') {
    for (const [key, value] of Object.entries(data.diffTextByThread)) {
      const str = (value || '').toString();
      if (state.diffTextByThread[key] !== str) {
        state.diffTextByThread[key] = str;
      }
    }
  }

  // --- 轻量 string 字段: 增量合并 ---
  mergeStringMap(state.statusHeadersByThread, data.statusHeadersByThread);
  mergeStringMap(state.statusDetailsByThread, data.statusDetailsByThread);

  // --- 对象字段: JSON.stringify 对比后赋值 ---
  mergeObjectMap(state.tokenUsageByThread, data.tokenUsageByThread);
  mergeObjectMap(state.agentMetaById, data.agentMetaById);
  mergeObjectMap(state.agentRuntimeById, data.agentRuntimeById);

  if (Object.prototype.hasOwnProperty.call(data, PREF_ACTIVE_THREAD_ID)) {
    const next = (data[PREF_ACTIVE_THREAD_ID] || '').toString();
    if (state.activeThreadId !== next) state.activeThreadId = next;
  }
  if (Object.prototype.hasOwnProperty.call(data, PREF_ACTIVE_CMD_THREAD_ID)) {
    const next = (data[PREF_ACTIVE_CMD_THREAD_ID] || '').toString();
    if (state.activeCmdThreadId !== next) state.activeCmdThreadId = next;
  }
  if (Object.prototype.hasOwnProperty.call(data, PREF_MAIN_AGENT_ID)) {
    const next = (data[PREF_MAIN_AGENT_ID] || '').toString();
    if (state.mainAgentId !== next) state.mainAgentId = next;
  }
  state.viewPrefsChat = normalizeChatPrefs(data[PREF_VIEW_CHAT]);
  state.viewPrefsCmd = normalizeCmdPrefs(data[PREF_VIEW_CMD]);
}

async function syncRuntimeState() {
  if (runtimeSyncPromise) {
    runtimeSyncPending = true;
    return runtimeSyncPromise;
  }

  runtimeSyncPromise = callAPI('ui/state/get', { threadId: state.activeThreadId || '' })
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

let _syncDebounceTimer = 0;

function handleBridgeEvent(evt) {
  const eventType = (evt?.type || evt?.method || '').toString();
  if (eventType === 'ui/state/changed' || eventType === 'thread/messages/page') {
    // 防抖: 合并短时间内的多次触发, 避免事件风暴导致 UI 卡顿
    clearTimeout(_syncDebounceTimer);
    _syncDebounceTimer = setTimeout(() => {
      syncRuntimeState().catch((error) => {
        logWarn('thread', 'state.sync.failed', { error, by_event: eventType });
      });
    }, 200);
  }
}


async function refreshThreads() {
  const start = perfNow();
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
  if (!threadId) {
    return {
      confirmed: false,
      mode: 'no_thread',
      interruptSent: false,
      settled: false,
    };
  }
  const start = perfNow();
  let interruptSent = false;
  let confirmed = false;
  let mode = 'failed';
  let settled = false;
  logInfo('thread', 'stop.request', {
    thread_id: threadId,
  });
  try {
    const interruptResult = await callAPI('turn/interrupt', { threadId });
    interruptSent = Boolean(interruptResult?.interruptSent);
    confirmed = Boolean(interruptResult?.confirmed);
    mode = (interruptResult?.mode || '').toString().trim() || (confirmed ? 'interrupt_confirmed' : 'interrupt_not_confirmed');
    settled = confirmed ||
      mode === 'interrupt_terminal_completed' ||
      mode === 'interrupt_terminal_failed' ||
      mode === 'no_active_turn';
    logInfo('thread', 'stop.interrupt.sent', {
      thread_id: threadId,
      confirmed,
      mode,
      settled,
      interrupt_sent: interruptSent,
      state_before: (interruptResult?.stateBefore || '').toString(),
      state_after: (interruptResult?.stateAfter || '').toString(),
      waited_ms: Number(interruptResult?.waitedMs || 0),
      duration_ms: Math.round(perfNow() - start),
    });
  } catch (interruptError) {
    logWarn('thread', 'stop.interrupt.failed', {
      thread_id: threadId,
      error: interruptError,
      duration_ms: Math.round(perfNow() - start),
    });
  }
  try {
    await syncRuntimeState();
  } catch (syncError) {
    logWarn('thread', 'stop.sync.failed', {
      thread_id: threadId,
      error: syncError,
      duration_ms: Math.round(perfNow() - start),
    });
  }
  logInfo('thread', 'stop.done', {
    thread_id: threadId,
    confirmed,
    mode,
    settled,
    interrupt_sent: interruptSent,
    duration_ms: Math.round(perfNow() - start),
  });
  return {
    confirmed,
    mode,
    interruptSent,
    settled,
  };
}

async function loadMessages(threadId, limit = 300) {
  if (!threadId) return;
  const start = perfNow();
  try {
    const res = await callAPI('thread/messages', { threadId, limit });
    syncRuntimeState().catch(() => { });  // 非阻塞: 后端已 hydrate snapshot
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
    const previewLower = previewUrl.toLowerCase();
    if (item?.kind === 'image') {
      if (path) {
        const payload = { type: 'localImage', path };
        if (previewLower.startsWith('data:image/')) {
          payload.url = previewUrl;
        }
        input.push(payload);
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
  }
}

async function compactThread(threadId) {
  const id = (threadId || '').toString().trim();
  if (!id) return;
  const start = perfNow();
  logInfo('thread', 'compact.start', {
    thread_id: id,
  });
  try {
    await callAPI('thread/compact/start', { threadId: id });
    await syncRuntimeState();
    logInfo('thread', 'compact.done', {
      thread_id: id,
      duration_ms: Math.round(perfNow() - start),
    });
  } catch (error) {
    logWarn('thread', 'compact.failed', {
      thread_id: id,
      error,
      duration_ms: Math.round(perfNow() - start),
    });
    throw error;
  }
}

async function forceCompleteThread(threadId) {
  const id = (threadId || '').toString().trim();
  if (!id) return;
  const start = perfNow();
  logInfo('thread', 'forceComplete.start', { thread_id: id });
  try {
    const result = await callAPI('turn/forceComplete', { threadId: id });
    await syncRuntimeState();
    logInfo('thread', 'forceComplete.done', {
      thread_id: id,
      confirmed: Boolean(result?.confirmed),
      duration_ms: Math.round(perfNow() - start),
    });
    return result;
  } catch (error) {
    logWarn('thread', 'forceComplete.failed', {
      thread_id: id,
      error,
      duration_ms: Math.round(perfNow() - start),
    });
    throw error;
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

function getThreadInterruptible(threadId) {
  if (!threadId) return false;
  return Boolean(state.interruptibleByThread[threadId]);
}

function getThreadStatusHeader(threadId) {
  if (!threadId) return '';
  return (state.statusHeadersByThread?.[threadId] || '').toString();
}

function getThreadStatusDetails(threadId) {
  if (!threadId) return '';
  return (state.statusDetailsByThread?.[threadId] || '').toString();
}

function getThreadTokenUsage(threadId) {
  if (!threadId) return null;
  const value = state.tokenUsageByThread?.[threadId];
  if (!value || typeof value !== 'object') return null;
  return value;
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
    compactThread,
    forceCompleteThread,
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
    getThreadInterruptible,
    getThreadStatusHeader,
    getThreadStatusDetails,
    getThreadTokenUsage,
    displayName,
  };
}
