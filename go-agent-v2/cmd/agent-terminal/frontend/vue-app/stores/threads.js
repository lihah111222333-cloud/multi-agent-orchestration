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
const PREF_PINNED_THREADS_CHAT = 'threadPins.chat';
const PREF_ARCHIVED_THREADS_CHAT = 'threadArchives.chat';

const state = reactive({
  activeThreadId: '',
  activeCmdThreadId: '',
  mainAgentId: '',
  pinnedThreadAtById: {},
  archivedThreadAtById: {},
});

const compactPendingByThread = reactive({});

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
  activityStatsByThread: {},
  alertsByThread: {},
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
const threadOrderIndexById = new Map();
let threadOrderSeq = 0;

function ensureThreadOrderIndex(threadId) {
  const id = (threadId || '').toString().trim();
  if (!id) return Number.MAX_SAFE_INTEGER;
  const existing = threadOrderIndexById.get(id);
  if (Number.isFinite(existing)) return existing;
  const created = threadOrderSeq;
  threadOrderSeq += 1;
  threadOrderIndexById.set(id, created);
  return created;
}

function sortThreadsByStableFirstSeen(threads) {
  if (!Array.isArray(threads) || threads.length <= 1) {
    return Array.isArray(threads) ? threads : [];
  }
  return threads
    .map((item, index) => ({
      item,
      index,
      stableOrder: ensureThreadOrderIndex(item?.id),
    }))
    .sort((left, right) => {
      if (left.stableOrder !== right.stableOrder) {
        return left.stableOrder - right.stableOrder;
      }
      return left.index - right.index;
    })
    .map((entry) => entry.item);
}

function perfNow() {
  if (typeof performance !== 'undefined' && typeof performance.now === 'function') {
    return performance.now();
  }
  return Date.now();
}

function waitMs(ms) {
  return new Promise((resolve) => {
    globalThis.setTimeout(resolve, Math.max(0, Number(ms) || 0));
  });
}

function tokenUsageSignature(threadId) {
  const usage = state.tokenUsageByThread?.[threadId];
  if (!usage || typeof usage !== 'object') return '';
  const used = Number(usage.usedTokens);
  const limit = Number(usage.contextWindowTokens);
  const percent = Number(usage.usedPercent);
  return [
    Number.isFinite(used) ? Math.round(used) : '',
    Number.isFinite(limit) ? Math.round(limit) : '',
    Number.isFinite(percent) ? percent.toFixed(3) : '',
  ].join('|');
}

async function waitCompactTokenUsageRefresh(threadId, baselineSignature) {
  const checkpoints = [180, 420, 900, 1600, 2600];
  for (const delay of checkpoints) {
    await waitMs(delay);
    try {
      await syncRuntimeState();
    } catch {
      // ignore: keep waiting until timeout checkpoints exhausted
    }
    const nextSignature = tokenUsageSignature(threadId);
    if (nextSignature && nextSignature !== baselineSignature) {
      return true;
    }
  }
  return false;
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

function normalizeThreadRailWidth(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return 232;
  return Math.max(188, Math.min(420, Math.round(n)));
}

function normalizeCmdCardCols(value) {
  return Number(value) === 2 ? 2 : 3;
}

function normalizeChatPrefs(value) {
  const input = value && typeof value === 'object' ? value : {};
  return {
    layout: normalizeChatLayout(input.layout || defaultLayoutForMode('chat')),
    splitRatio: normalizeSplitRatio(input.splitRatio),
    threadRailWidth: normalizeThreadRailWidth(input.threadRailWidth),
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

function getThreadRailWidth() {
  return readChatPrefs().threadRailWidth;
}

function setThreadRailWidth(width) {
  const next = normalizeThreadRailWidth(width);
  const current = readChatPrefs();
  if (current.threadRailWidth === next) return;
  persistPreferenceAndSync(PREF_VIEW_CHAT, {
    ...current,
    threadRailWidth: next,
  }, {
    mode: 'chat',
    field: 'threadRailWidth',
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
  return sortChatThreadsByPinned(deriveChatAgents({ threads: state.threads }));
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

function normalizeThreadTimestampMap(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {};
  }
  const next = {};
  for (const [rawID, rawTime] of Object.entries(value)) {
    const id = (rawID || '').toString().trim();
    if (!id) continue;
    const ts = Number(rawTime);
    if (!Number.isFinite(ts) || ts <= 0) continue;
    next[id] = Math.round(ts);
  }
  return next;
}

function normalizePinnedThreadMap(value) {
  return normalizeThreadTimestampMap(value);
}

function normalizeArchivedThreadMap(value) {
  return normalizeThreadTimestampMap(value);
}

function sortChatThreadsByPinned(threads) {
  const list = Array.isArray(threads) ? threads.slice() : [];
  if (list.length <= 1) return list;
  const indexByID = new Map();
  for (let i = 0; i < list.length; i += 1) {
    indexByID.set((list[i]?.id || '').toString(), i);
  }
  list.sort((left, right) => {
    const leftID = (left?.id || '').toString();
    const rightID = (right?.id || '').toString();
    const leftPinnedAt = Number(state.pinnedThreadAtById?.[leftID]);
    const rightPinnedAt = Number(state.pinnedThreadAtById?.[rightID]);
    const leftPinned = Number.isFinite(leftPinnedAt) && leftPinnedAt > 0;
    const rightPinned = Number.isFinite(rightPinnedAt) && rightPinnedAt > 0;
    if (leftPinned !== rightPinned) {
      return leftPinned ? -1 : 1;
    }
    if (leftPinned && rightPinned && leftPinnedAt !== rightPinnedAt) {
      return rightPinnedAt - leftPinnedAt;
    }
    return (indexByID.get(leftID) ?? 0) - (indexByID.get(rightID) ?? 0);
  });
  return list;
}



// 增量合并 helper: 只更新 string 值变化的 key (原子草稿版)
function mergeStringMapAtomic(current, source) {
  if (!source || typeof source !== 'object') {
    return { next: current, changed: false };
  }
  let next = current;
  let changed = false;
  for (const [key, value] of Object.entries(source)) {
    const str = (value || '').toString();
    if (next[key] === str) continue;
    if (!changed) {
      next = { ...(next || {}) };
      changed = true;
    }
    next[key] = str;
  }
  return { next, changed };
}

// 增量合并 helper: 用 JSON.stringify 对比的 object map (原子草稿版)
function mergeObjectMapAtomic(current, source) {
  if (!source || typeof source !== 'object') {
    return { next: current, changed: false };
  }
  let next = current;
  let changed = false;
  for (const [key, value] of Object.entries(source)) {
    const normalized = value && typeof value === 'object' ? value : {};
    if (JSON.stringify(next[key]) === JSON.stringify(normalized)) continue;
    if (!changed) {
      next = { ...(next || {}) };
      changed = true;
    }
    Object.freeze(normalized);
    next[key] = normalized;
  }
  return { next, changed };
}

function applyRuntimeSnapshot(snapshot) {
  const data = snapshot && typeof snapshot === 'object' ? snapshot : {};
  const patch = {};

  // --- threads: 首次出现顺序固定，且仅在 ID 集变化时替换 ---
  const unorderedThreads = Array.isArray(data.threads)
    ? data.threads.map(normalizeThread)
    : [];
  const nextThreads = sortThreadsByStableFirstSeen(unorderedThreads);
  const oldIds = state.threads.map((t) => t.id).join(',');
  const newIds = nextThreads.map((t) => t.id).join(',');
  if (oldIds !== newIds) {
    patch.threads = nextThreads;
  }

  // --- statuses: 增量合并, 只更新变化的 key ---
  let nextStatuses = state.statuses;
  let statusesChanged = false;
  if (data.statuses && typeof data.statuses === 'object') {
    for (const [key, value] of Object.entries(data.statuses)) {
      const normalized = normalizeStatus(value);
      if (nextStatuses[key] === normalized) continue;
      if (!statusesChanged) {
        nextStatuses = { ...nextStatuses };
        statusesChanged = true;
      }
      nextStatuses[key] = normalized;
    }
  }
  for (const thread of nextThreads) {
    if (nextStatuses[thread.id]) continue;
    if (!statusesChanged) {
      nextStatuses = { ...nextStatuses };
      statusesChanged = true;
    }
    nextStatuses[thread.id] = normalizeStatus(thread.state || 'idle');
  }
  if (statusesChanged) {
    patch.statuses = nextStatuses;
  }

  // --- interruptibleByThread: 增量合并 ---
  let nextInterruptibleByThread = state.interruptibleByThread;
  let interruptibleChanged = false;
  if (data.interruptibleByThread && typeof data.interruptibleByThread === 'object') {
    for (const [key, value] of Object.entries(data.interruptibleByThread)) {
      const b = Boolean(value);
      if (nextInterruptibleByThread[key] === b) continue;
      if (!interruptibleChanged) {
        nextInterruptibleByThread = { ...nextInterruptibleByThread };
        interruptibleChanged = true;
      }
      nextInterruptibleByThread[key] = b;
    }
  }
  if (interruptibleChanged) {
    patch.interruptibleByThread = nextInterruptibleByThread;
  }

  // --- timelinesByThread: freeze items + 只在变化时替换 ---
  let nextTimelinesByThread = state.timelinesByThread;
  let timelinesChanged = false;
  if (data.timelinesByThread && typeof data.timelinesByThread === 'object') {
    for (const [key, value] of Object.entries(data.timelinesByThread)) {
      const newItems = Array.isArray(value) ? value : [];
      const oldItems = nextTimelinesByThread[key];
      // 快速对比: 长度相同 + 最后一条 item 的 id/text长度/output长度/command/status/done 都相同 → 跳过
      if (
        oldItems &&
        oldItems.length === newItems.length &&
        oldItems.length > 0 &&
        oldItems[oldItems.length - 1]?.id === newItems[newItems.length - 1]?.id &&
        (oldItems[oldItems.length - 1]?.text || '').length ===
        (newItems[newItems.length - 1]?.text || '').length &&
        (oldItems[oldItems.length - 1]?.output || '').length ===
        (newItems[newItems.length - 1]?.output || '').length &&
        (oldItems[oldItems.length - 1]?.command || '') ===
        (newItems[newItems.length - 1]?.command || '') &&
        (oldItems[oldItems.length - 1]?.status || '') ===
        (newItems[newItems.length - 1]?.status || '') &&
        Boolean(oldItems[oldItems.length - 1]?.done) ===
        Boolean(newItems[newItems.length - 1]?.done)
      ) {
        continue;
      }
      // freeze 每条 item, 阻止 Vue 深层追踪
      for (let i = 0; i < newItems.length; i++) {
        if (newItems[i] && typeof newItems[i] === 'object') {
          Object.freeze(newItems[i]);
        }
      }
      if (!timelinesChanged) {
        nextTimelinesByThread = { ...nextTimelinesByThread };
        timelinesChanged = true;
      }
      nextTimelinesByThread[key] = Object.freeze(newItems);
    }
  }
  if (timelinesChanged) {
    patch.timelinesByThread = nextTimelinesByThread;
  }

  // --- diffTextByThread: 增量合并 ---
  let nextDiffTextByThread = state.diffTextByThread;
  let diffChanged = false;
  if (data.diffTextByThread && typeof data.diffTextByThread === 'object') {
    for (const [key, value] of Object.entries(data.diffTextByThread)) {
      const str = (value || '').toString();
      if (nextDiffTextByThread[key] === str) continue;
      if (!diffChanged) {
        nextDiffTextByThread = { ...nextDiffTextByThread };
        diffChanged = true;
      }
      nextDiffTextByThread[key] = str;
    }
  }
  if (diffChanged) {
    patch.diffTextByThread = nextDiffTextByThread;
  }

  // --- 轻量 string 字段: 增量合并 ---
  const mergedStatusHeaders = mergeStringMapAtomic(state.statusHeadersByThread, data.statusHeadersByThread);
  if (mergedStatusHeaders.changed) {
    patch.statusHeadersByThread = mergedStatusHeaders.next;
  }
  const mergedStatusDetails = mergeStringMapAtomic(state.statusDetailsByThread, data.statusDetailsByThread);
  if (mergedStatusDetails.changed) {
    patch.statusDetailsByThread = mergedStatusDetails.next;
  }

  // --- 对象字段: JSON.stringify 对比后赋值 ---
  const mergedTokenUsage = mergeObjectMapAtomic(state.tokenUsageByThread, data.tokenUsageByThread);
  if (mergedTokenUsage.changed) {
    patch.tokenUsageByThread = mergedTokenUsage.next;
  }
  const mergedAgentMeta = mergeObjectMapAtomic(state.agentMetaById, data.agentMetaById);
  if (mergedAgentMeta.changed) {
    patch.agentMetaById = mergedAgentMeta.next;
  }
  const mergedAgentRuntime = mergeObjectMapAtomic(state.agentRuntimeById, data.agentRuntimeById);
  if (mergedAgentRuntime.changed) {
    patch.agentRuntimeById = mergedAgentRuntime.next;
  }
  const mergedActivityStats = mergeObjectMapAtomic(state.activityStatsByThread, data.activityStatsByThread);
  if (mergedActivityStats.changed) {
    patch.activityStatsByThread = mergedActivityStats.next;
  }
  const mergedAlerts = mergeObjectMapAtomic(state.alertsByThread, data.alertsByThread);
  if (mergedAlerts.changed) {
    patch.alertsByThread = mergedAlerts.next;
  }

  if (Object.prototype.hasOwnProperty.call(data, PREF_ACTIVE_THREAD_ID)) {
    const next = (data[PREF_ACTIVE_THREAD_ID] || '').toString();
    if (state.activeThreadId !== next) patch.activeThreadId = next;
  }
  if (Object.prototype.hasOwnProperty.call(data, PREF_ACTIVE_CMD_THREAD_ID)) {
    const next = (data[PREF_ACTIVE_CMD_THREAD_ID] || '').toString();
    if (state.activeCmdThreadId !== next) patch.activeCmdThreadId = next;
  }
  if (Object.prototype.hasOwnProperty.call(data, PREF_MAIN_AGENT_ID)) {
    const next = (data[PREF_MAIN_AGENT_ID] || '').toString();
    if (state.mainAgentId !== next) patch.mainAgentId = next;
  }
  if (Object.prototype.hasOwnProperty.call(data, PREF_PINNED_THREADS_CHAT)) {
    const pinnedMap = normalizePinnedThreadMap(data[PREF_PINNED_THREADS_CHAT]);
    for (const id of Object.keys(pinnedMap)) {
      ensureThreadOrderIndex(id);
    }
    patch.pinnedThreadAtById = pinnedMap;
  }
  if (Object.prototype.hasOwnProperty.call(data, PREF_ARCHIVED_THREADS_CHAT)) {
    const archivedMap = normalizeArchivedThreadMap(data[PREF_ARCHIVED_THREADS_CHAT]);
    for (const id of Object.keys(archivedMap)) {
      ensureThreadOrderIndex(id);
    }
    patch.archivedThreadAtById = archivedMap;
  }
  const nextViewPrefsChat = normalizeChatPrefs(data[PREF_VIEW_CHAT]);
  if (JSON.stringify(nextViewPrefsChat) !== JSON.stringify(state.viewPrefsChat)) {
    patch.viewPrefsChat = nextViewPrefsChat;
  }
  const nextViewPrefsCmd = normalizeCmdPrefs(data[PREF_VIEW_CMD]);
  if (JSON.stringify(nextViewPrefsCmd) !== JSON.stringify(state.viewPrefsCmd)) {
    patch.viewPrefsCmd = nextViewPrefsCmd;
  }

  if (Object.keys(patch).length > 0) {
    Object.assign(state, patch);
  }
}

async function syncRuntimeState() {
  if (runtimeSyncPromise) {
    runtimeSyncPending = true;
    // 等待当前请求完成; .finally() 的重试也会被 await
    await runtimeSyncPromise;
    // 如果重试已在进行中, 等待它 (确保拿到最新数据)
    if (runtimeSyncPromise) {
      return runtimeSyncPromise;
    }
    return;
  }

  runtimeSyncPromise = (async () => {
    try {
      const activeThreadId = (state.activeThreadId || '').toString().trim();
      logInfo('thread', 'state.sync.request', {
        active_thread_id: activeThreadId,
      });
      const res = await callAPI('ui/state/get', { threadId: activeThreadId });
      const timelines = (res && typeof res.timelinesByThread === 'object' && res.timelinesByThread)
        ? res.timelinesByThread
        : {};
      const diffs = (res && typeof res.diffTextByThread === 'object' && res.diffTextByThread)
        ? res.diffTextByThread
        : {};
      const diffLengths = Object.entries(diffs)
        .slice(0, 8)
        .map(([threadId, text]) => ({
          thread_id: threadId,
          diff_len: (text || '').toString().length,
        }));
      logInfo('thread', 'state.sync.response', {
        active_thread_id: activeThreadId,
        timeline_threads: Object.keys(timelines).length,
        diff_threads: Object.keys(diffs).length,
        diff_lengths: diffLengths,
      });
      applyRuntimeSnapshot(res || {});
    } finally {
      runtimeSyncPromise = null;
      if (runtimeSyncPending) {
        runtimeSyncPending = false;
        // 直接 await 重试 — 不再 fire-and-forget
        await syncRuntimeState().catch(() => { });
      }
    }
  })();
  return runtimeSyncPromise;
}

function handleAgentEvent() {
  // runtime sync is backend-driven by bridge event `ui/state/changed`
}

let _syncDebounceTimer = 0;
let _syncThrottleLastRun = 0;
const SYNC_THROTTLE_MS = 500;

function handleBridgeEvent(evt) {
  const eventType = (evt?.type || evt?.method || '').toString();
  if (
    eventType === 'ui/state/changed'
    || eventType === 'thread/messages/page'
    || eventType === 'thread/compacted'
    || eventType === 'thread/tokenUsage/updated'
  ) {
    const debounceMs = (eventType === 'thread/compacted' || eventType === 'thread/tokenUsage/updated')
      ? 80
      : 200;

    // Throttle: 每 500ms 至少同步一次 (避免连续事件期间永远同步不到)
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
    if (now - _syncThrottleLastRun >= SYNC_THROTTLE_MS) {
      _syncThrottleLastRun = now;
      syncRuntimeState().catch((error) => {
        logWarn('thread', 'state.sync.throttle.failed', { error, by_event: eventType });
      });
    }

    // Trailing debounce: 事件风暴结束后做最终同步
    clearTimeout(_syncDebounceTimer);
    _syncDebounceTimer = setTimeout(() => {
      _syncThrottleLastRun = typeof performance !== 'undefined' ? performance.now() : Date.now();
      syncRuntimeState().catch((error) => {
        logWarn('thread', 'state.sync.failed', { error, by_event: eventType });
      });
    }, debounceMs);
  }

  // turn 终态事件: 延迟 600ms 后强制最终同步 (兜底, 确保 UI 拿到完整数据)
  if (eventType === 'turn/completed' || eventType === 'turn/aborted') {
    setTimeout(() => {
      syncRuntimeState().catch((error) => {
        logWarn('thread', 'state.sync.turn-settle.failed', { error, by_event: eventType });
      });
    }, 600);
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

async function sendMessage(threadId, prompt, attachments = [], options = {}) {
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
    const fileName = path.split(/[\\/]/).pop() || path;
    input.push({ type: 'mention', name: fileName, path });
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
  const selectedSkills = Array.isArray(options?.selectedSkills)
    ? options.selectedSkills
      .map((item) => (item || '').toString().trim())
      .filter(Boolean)
    : [];
  const manualSkillSelection = Boolean(options?.manualSkillSelection);
  const requestPayload = { threadId, input };
  if (selectedSkills.length > 0) {
    requestPayload.selectedSkills = selectedSkills;
  }
  if (manualSkillSelection || selectedSkills.length > 0) {
    requestPayload.manualSkillSelection = manualSkillSelection;
  }
  logInfo('thread', 'send.start', {
    thread_id: threadId,
    text_len: text.length,
    attachments: attachments.length,
    local_images: localImageCount,
    inline_images: remoteImageCount,
    files: fileCount,
    dropped_attachments: droppedAttachmentCount,
    selected_skills: selectedSkills.length,
    manual_skill_selection: manualSkillSelection,
  });
  try {
    await callAPI('turn/start', requestPayload);
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
  if (compactPendingByThread[id]) return;
  const start = perfNow();
  const baselineSignature = tokenUsageSignature(id);
  let interruptAttempted = false;
  let interruptConfirmed = false;
  let interruptSettled = false;
  let interruptMode = '';
  compactPendingByThread[id] = true;
  logInfo('thread', 'compact.start', {
    thread_id: id,
    token_usage_sig_before: baselineSignature,
  });
  try {
    await syncRuntimeState();
    if (getThreadInterruptible(id)) {
      interruptAttempted = true;
      logInfo('thread', 'compact.interrupt.before', {
        thread_id: id,
      });
      const interruptResult = await stopThread(id);
      interruptMode = (interruptResult?.mode || '').toString().trim();
      interruptConfirmed = Boolean(interruptResult?.confirmed);
      interruptSettled = Boolean(interruptResult?.settled || interruptConfirmed || interruptMode === 'no_active_turn');
      logInfo('thread', 'compact.interrupt.result', {
        thread_id: id,
        interrupt_confirmed: interruptConfirmed,
        interrupt_settled: interruptSettled,
        interrupt_mode: interruptMode,
      });
      if (!interruptSettled) {
        throw new Error(`compact_interrupt_not_settled:${interruptMode || 'unknown'}`);
      }
      await waitMs(120);
    }
    await callAPI('thread/compact/start', { threadId: id });
    await syncRuntimeState();
    const refreshed = await waitCompactTokenUsageRefresh(id, baselineSignature);
    logInfo('thread', 'compact.done', {
      thread_id: id,
      token_usage_refreshed: refreshed,
      token_usage_sig_after: tokenUsageSignature(id),
      interrupt_attempted: interruptAttempted,
      interrupt_confirmed: interruptConfirmed,
      interrupt_settled: interruptSettled,
      interrupt_mode: interruptMode,
      duration_ms: Math.round(perfNow() - start),
    });
  } catch (error) {
    logWarn('thread', 'compact.failed', {
      thread_id: id,
      interrupt_attempted: interruptAttempted,
      interrupt_confirmed: interruptConfirmed,
      interrupt_settled: interruptSettled,
      interrupt_mode: interruptMode,
      error,
      duration_ms: Math.round(perfNow() - start),
    });
    throw error;
  } finally {
    delete compactPendingByThread[id];
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

function getThreadCompacting(threadId) {
  if (!threadId) return false;
  return Boolean(compactPendingByThread[threadId]);
}

function getThreadActivityStats(threadId) {
  if (!threadId) return {};
  const value = state.activityStatsByThread?.[threadId];
  if (!value || typeof value !== 'object') return {};
  return value;
}

function getThreadAlerts(threadId) {
  if (!threadId) return [];
  const value = state.alertsByThread?.[threadId];
  return Array.isArray(value) ? value : [];
}

function getThreadPinnedAt(threadId) {
  const id = (threadId || '').toString().trim();
  if (!id) return 0;
  const value = Number(state.pinnedThreadAtById?.[id]);
  if (!Number.isFinite(value) || value <= 0) return 0;
  return value;
}

function getThreadArchivedAt(threadId) {
  const id = (threadId || '').toString().trim();
  if (!id) return 0;
  const value = Number(state.archivedThreadAtById?.[id]);
  if (!Number.isFinite(value) || value <= 0) return 0;
  return value;
}

function setThreadPinned(threadId, pinned) {
  const id = (threadId || '').toString().trim();
  if (!id) return;
  ensureThreadOrderIndex(id);
  const next = { ...(state.pinnedThreadAtById || {}) };
  if (pinned) {
    next[id] = Date.now();
  } else {
    delete next[id];
  }
  state.pinnedThreadAtById = next;
  persistPreferenceAndSync(PREF_PINNED_THREADS_CHAT, next, {
    thread_id: id,
    pinned: Boolean(pinned),
  });
}

function toggleThreadPin(threadId) {
  const id = (threadId || '').toString().trim();
  if (!id) return;
  const currentPinned = getThreadPinnedAt(id) > 0;
  setThreadPinned(id, !currentPinned);
}

async function setThreadArchived(threadId, archived) {
  const id = (threadId || '').toString().trim();
  if (!id) return;
  const previous = { ...(state.archivedThreadAtById || {}) };
  const next = { ...previous };
  if (archived) {
    next[id] = Date.now();
  } else {
    delete next[id];
  }
  state.archivedThreadAtById = next;
  try {
    const response = await callAPI(archived ? 'thread/archive' : 'thread/unarchive', { threadId: id });
    await syncRuntimeState();
    if (!archived && response && response.archiveModified) {
      const warningText = (response.warning || '检测到归档文件已变化，恢复后的上下文可能与归档时不一致。').toString();
      logWarn('thread', 'unarchive.modified_warning', {
        thread_id: id,
        warning: warningText,
        modified_files: Array.isArray(response.modifiedFiles) ? response.modifiedFiles.length : 0,
      });
      if (typeof window !== 'undefined' && typeof window.alert === 'function') {
        window.alert(warningText);
      }
    }
  } catch (error) {
    state.archivedThreadAtById = previous;
    logWarn('thread', archived ? 'archive.failed' : 'unarchive.failed', {
      thread_id: id,
      error,
    });
    throw error;
  }
}

function toggleThreadArchive(threadId) {
  const id = (threadId || '').toString().trim();
  if (!id) return Promise.resolve();
  const currentArchived = getThreadArchivedAt(id) > 0;
  return setThreadArchived(id, !currentArchived);
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
    renameThread,
    promptRenameThread,
    getLayout,
    setLayout,
    getSplitRatio,
    setSplitRatio,
    getThreadRailWidth,
    setThreadRailWidth,
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
    getThreadCompacting,
    getThreadActivityStats,
    getThreadAlerts,
    getThreadPinnedAt,
    getThreadArchivedAt,
    setThreadPinned,
    toggleThreadPin,
    setThreadArchived,
    toggleThreadArchive,
    displayName,
  };
}
