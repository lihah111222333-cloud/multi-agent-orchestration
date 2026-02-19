import {
  ref,
  computed,
  watch,
  onMounted,
  nextTick,
  onBeforeUnmount,
} from '../../lib/vue.esm-browser.prod.js';
import { ProjectSelect } from '../components/ProjectSelect.js';
import { ChatTimeline } from '../components/ChatTimeline.js';
import { DiffPanel } from '../components/DiffPanel.js';
import { ComposerBar } from '../components/ComposerBar.js';
import { normalizeStatus } from '../services/status.js';
import { copyTextToClipboard, resolveThreadIdentity } from '../services/api.js';
import { logInfo, logWarn } from '../services/log.js';
import { useComposerStore } from '../stores/composer.js';

export async function requestHistoryLoad(threadStore, threadId, options = {}) {
  if (!threadId || typeof threadStore?.loadMessages !== 'function') {
    return false;
  }

  if (options.force) {
    const limit = Number.isFinite(options.limit) && options.limit > 0 ? options.limit : 300;
    await threadStore.loadMessages(threadId, limit);
    return true;
  }

  // 如果 timeline 已有数据, 跳过重复加载
  const existing = threadStore.getThreadTimeline(threadId);
  if (Array.isArray(existing) && existing.length > 0) {
    return false;
  }

  await threadStore.loadMessages(threadId);
  return true;
}

export const UnifiedChatPage = {
  name: 'UnifiedChatPage',
  components: {
    ProjectSelect,
    ChatTimeline,
    DiffPanel,
    ComposerBar,
  },
  props: {
    projectStore: { type: Object, required: true },
    threadStore: { type: Object, required: true },
    mode: { type: String, default: 'chat' },
  },
  setup(props) {
    const composer = useComposerStore();
    const composerBarRef = ref(null);
    const workspaceRef = ref(null);
    const dragging = ref(false);
    const copyState = ref('idle');
    let scrollTimer = 0;
    let copyStateTimer = 0;

    const isCmd = computed(() => props.mode === 'cmd');
    const modeKey = computed(() => (isCmd.value ? 'cmd' : 'chat'));

    const layoutMode = computed({
      get: () => props.threadStore.getLayout(modeKey.value),
      set: (value) => props.threadStore.setLayout(modeKey.value, value),
    });
    const cmdCardCols = computed({
      get: () => (typeof props.threadStore.getCmdCardCols === 'function'
        ? props.threadStore.getCmdCardCols()
        : 3),
      set: (value) => {
        if (typeof props.threadStore.setCmdCardCols === 'function') {
          props.threadStore.setCmdCardCols(value);
        }
      },
    });

    const splitRatio = ref(props.threadStore.getSplitRatio(modeKey.value));

    const threads = computed(() => props.threadStore.getThreadsByMode(modeKey.value));
    const mainAgentId = computed(() => props.threadStore.state.mainAgentId || '');

    const selectedThreadId = computed({
      get: () => props.threadStore.getCurrentThreadId(modeKey.value) || '',
      set: (value) => {
        if (isCmd.value) {
          props.threadStore.saveActiveCmdThread(value || '');
        } else {
          props.threadStore.saveActiveThread(value || '');
        }
      },
    });

    const activeThread = computed(() => threads.value.find((item) => item.id === selectedThreadId.value) || null);
    const chatThreadOptions = computed(() => {
      if (isCmd.value) return [];
      return threads.value;
    });

    const activeTimeline = computed(() => props.threadStore.getThreadTimeline(selectedThreadId.value));
    const activeDiffText = computed(() => props.threadStore.getThreadDiff(selectedThreadId.value));
    const activeStatus = computed(() => normalizeStatus(props.threadStore.getThreadStatus(selectedThreadId.value)));
    function getThreadStatusHeader(threadId) {
      if (!threadId) return '';
      if (typeof props.threadStore.getThreadStatusHeader !== 'function') return '';
      const header = (props.threadStore.getThreadStatusHeader(threadId) || '').toString().trim();
      if (header) return header;
      return '等待指示';
    }
    const activeStatusHeader = computed(() => getThreadStatusHeader(selectedThreadId.value));
    const activeStatusDetails = computed(() => {
      if (typeof props.threadStore.getThreadStatusDetails !== 'function') return '';
      return (props.threadStore.getThreadStatusDetails(selectedThreadId.value) || '').toString().trim();
    });
    const activeTokenUsage = computed(() => {
      if (typeof props.threadStore.getThreadTokenUsage !== 'function') return null;
      return props.threadStore.getThreadTokenUsage(selectedThreadId.value);
    });
    const canInterrupt = computed(() => {
      if (typeof props.threadStore.getThreadInterruptible !== 'function') return false;
      return props.threadStore.getThreadInterruptible(selectedThreadId.value);
    });
    const displayStatusText = computed(() => {
      if (!selectedThreadId.value) return '未选择会话';
      return activeStatusHeader.value || '等待指示';
    });
    const activeTokenInline = computed(() => formatTokenInline(activeTokenUsage.value));
    const activeTokenTooltip = computed(() => formatTokenTooltip(activeTokenUsage.value));
    const isStatusTimerModalPaused = computed(() => Boolean(props.projectStore?.state?.showModal));
    const statusSinceByThread = ref({});
    const statusPausedAtByThread = ref({});
    const statusTick = ref(Date.now());
    let statusTickTimer = 0;
    const activeStatusMeta = computed(() => {
      const threadId = selectedThreadId.value;
      if (!threadId) return '';
      const state = normalizeStatus(activeStatus.value);
      if (state === 'idle') return '';
      const since = Number(statusSinceByThread.value[threadId]) || Date.now();
      const elapsedSeconds = Math.max(0, Math.floor((statusTick.value - since) / 1000));
      const elapsed = formatElapsedCompact(elapsedSeconds);
      const hint = canInterrupt.value ? ' • Esc 可中断' : '';
      const detail = activeStatusDetails.value;
      if (detail) {
        return `(${elapsed}${hint}) · ${detail}`;
      }
      return `(${elapsed}${hint})`;
    });
    const activeRuntime = computed(() => {
      const map = props.threadStore.state.agentRuntimeById || {};
      return map[selectedThreadId.value] || null;
    });
    const shouldAutoScroll = ref(true);
    const timelineSignal = computed(() => {
      const list = activeTimeline.value || [];
      const last = list[list.length - 1] || null;
      const signalText = `${last?.text || ''}${last?.output || ''}${last?.preview || ''}`;
      return `${selectedThreadId.value}|${list.length}|${last?.id || ''}|${signalText.length}|${last?.status || ''}|${activeStatus.value}`;
    });

    const noActiveThread = computed(() => !selectedThreadId.value);
    const copyButtonLabel = computed(() => {
      if (copyState.value === 'done') return '已复制';
      if (copyState.value === 'error') return '复制失败';
      return '复制信息';
    });

    const showOverview = computed(() => {
      if (isCmd.value) return false;
      return layoutMode.value === 'mix';
    });

    const showWorkspace = computed(() => true);

    function resolveChatScroller() {
      const root = workspaceRef.value;
      if (root && typeof root.querySelector === 'function') {
        const within = root.querySelector('.chat-messages-vue');
        if (within) return within;
      }
      return document.querySelector('.chat-messages-vue');
    }

    function isEditableElement(node) {
      if (!node || typeof node !== 'object') return false;
      const tag = (node.tagName || '').toString().toLowerCase();
      if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
      if (Boolean(node.isContentEditable)) return true;
      if (typeof node.closest === 'function') {
        const editableRoot = node.closest('[contenteditable], [contenteditable="true"]');
        if (editableRoot) return true;
      }
      return false;
    }

    function onGlobalEscape(event) {
      if ((event?.key || '') !== 'Escape') return;
      if (event?.defaultPrevented || event?.repeat) return;
      if (!selectedThreadId.value) return;
      if (!canInterrupt.value) return;
      if (isStatusTimerModalPaused.value) return;
      const activeEl = typeof document !== 'undefined' ? document.activeElement : null;
      if (isEditableElement(event?.target) || isEditableElement(activeEl)) return;
      composerBarRef.value?.onEscape?.(event);
    }

    function distanceFromBottom(el) {
      if (!el) return 0;
      return el.scrollHeight - el.scrollTop - el.clientHeight;
    }

    function isNearBottom(el, threshold = 96) {
      return distanceFromBottom(el) <= threshold;
    }

    function scheduleScrollToBottom(force = false) {
      if (scrollTimer) {
        cancelAnimationFrame(scrollTimer);
      }
      scrollTimer = requestAnimationFrame(() => {
        const el = resolveChatScroller();
        if (!el) return;
        if (!force && !shouldAutoScroll.value) return;
        el.scrollTop = el.scrollHeight;
      });
    }

    let _lastStatsKey = '';
    let _lastStats = { total: 0, running: 0, thinking: 0, editing: 0, error: 0 };
    const stats = computed(() => {
      const ids = threads.value.map((t) => t.id);
      const key = ids.map((id) => `${id}:${normalizeStatus(props.threadStore.getThreadStatus(id))}`).join(',');
      if (key === _lastStatsKey) return _lastStats;
      _lastStatsKey = key;
      const summary = { total: ids.length, running: 0, thinking: 0, editing: 0, error: 0 };
      for (const id of ids) {
        const status = normalizeStatus(props.threadStore.getThreadStatus(id));
        if (status === 'running') summary.running += 1;
        if (status === 'thinking' || status === 'responding' || status === 'waiting') summary.thinking += 1;
        if (status === 'editing') summary.editing += 1;
        if (status === 'error') summary.error += 1;
      }
      _lastStats = summary;
      return summary;
    });

    const recentThreads = computed(() => {
      const meta = props.threadStore.state.agentMetaById || {};
      return [...threads.value]
        .sort((a, b) => {
          const aTs = Date.parse(meta[a.id]?.lastActiveAt || '') || 0;
          const bTs = Date.parse(meta[b.id]?.lastActiveAt || '') || 0;
          return bTs - aTs;
        })
        .slice(0, 6);
    });

    const cmdCards = computed(() => {
      if (!isCmd.value) return [];
      const selId = selectedThreadId.value;
      const layout = layoutMode.value;
      return threads.value.map((thread) => {
        const selected = thread.id === selId;
        const card = {
          id: thread.id,
          name: props.threadStore.displayName(thread),
          status: props.threadStore.getThreadStatus(thread.id),
          statusHeader: getThreadStatusHeader(thread.id) || '等待指示',
          selected,
          preview: [],
          diff: '',
        };
        // 只为选中的 card 计算 preview/diff (跳过未选中 card 的昂贵操作)
        if (selected) {
          if (layout !== 'overview') card.preview = timelinePreview(thread.id);
          if (layout === 'mix') card.diff = diffPreview(thread.id);
        }
        return card;
      });
    });

    watch(
      () => modeKey.value,
      () => {
        splitRatio.value = props.threadStore.getSplitRatio(modeKey.value);
      },
      { immediate: true },
    );

    watch(
      () => splitRatio.value,
      (value) => {
        props.threadStore.setSplitRatio(modeKey.value, value);
      },
    );

    watch(
      () => selectedThreadId.value,
      async (id) => {
        if (!id) return;
        shouldAutoScroll.value = true;
        try {
          await requestHistoryLoad(props.threadStore, id);
        } catch {
          // ignore: real-time stream may still backfill timeline
        }
        scheduleScrollToBottom(true);
      },
      { immediate: true },
    );

    watch(
      () => timelineSignal.value,
      () => {
        const el = resolveChatScroller();
        shouldAutoScroll.value = !el || isNearBottom(el);
        if (!shouldAutoScroll.value) return;
        scheduleScrollToBottom(true);
      },
    );

    watch(
      () => [
        selectedThreadId.value,
        activeStatus.value,
        canInterrupt.value,
        isStatusTimerModalPaused.value,
      ],
      ([threadId, status, interruptible, modalPaused]) => {
        const now = Date.now();
        statusTick.value = now;
        if (!threadId) {
          stopStatusTickTimer();
          return;
        }
        const state = normalizeStatus(status);
        if (state === 'idle') {
          statusSinceByThread.value[threadId] = 0;
          statusPausedAtByThread.value[threadId] = 0;
          stopStatusTickTimer();
          return;
        }
        if (!statusSinceByThread.value[threadId]) {
          statusSinceByThread.value[threadId] = now;
        }
        const shouldTick = Boolean(interruptible) && !modalPaused;
        const pausedAt = Number(statusPausedAtByThread.value[threadId]) || 0;
        if (shouldTick) {
          if (pausedAt > 0) {
            const since = Number(statusSinceByThread.value[threadId]) || now;
            statusSinceByThread.value[threadId] = since + Math.max(0, now - pausedAt);
            statusPausedAtByThread.value[threadId] = 0;
          }
          ensureStatusTickTimer();
          return;
        }
        if (!pausedAt) {
          statusPausedAtByThread.value[threadId] = now;
        }
        stopStatusTickTimer();
      },
      { immediate: true },
    );

    function launchOne() {
      return props.threadStore.startThread(props.projectStore.state.active || '.', {
        focusMode: modeKey.value,
      }).then((id) => {
        if (id) {
          selectedThreadId.value = id;
        }
      });
    }

    async function send() {
      const threadId = selectedThreadId.value;
      if (!threadId) return;
      const text = composer.state.text;
      const attachments = [...composer.state.attachments];
      if (!text.trim() && attachments.length === 0) return;
      composer.clearComposer();
      shouldAutoScroll.value = true;
      await props.threadStore.sendMessage(threadId, text, attachments);
      scheduleScrollToBottom(true);
    }

    async function interruptCurrent(control) {
      const threadId = (control?.threadId || selectedThreadId.value || '').toString();
      if (!threadId) {
        control?.reject?.({ reason: 'no_thread' });
        return;
      }
      logInfo('ui', 'chat.interrupt.request', {
        thread_id: threadId,
      });
      try {
        const result = await props.threadStore.stopThread(threadId);
        const confirmed = Boolean(result?.confirmed);
        const settled = Boolean(result?.settled || confirmed);
        const mode = (result?.mode || '').toString();
        logInfo('ui', 'chat.interrupt.result', {
          thread_id: threadId,
          confirmed,
          settled,
          mode,
        });
        if (settled) {
          control?.confirm?.({
            mode,
            threadId,
          });
        } else {
          control?.reject?.({
            reason: mode || 'not_confirmed',
            mode,
            threadId,
          });
        }
      } catch (error) {
        logWarn('ui', 'chat.interrupt.failed', {
          thread_id: threadId,
          error,
        });
        control?.reject?.({
          reason: 'error',
          threadId,
        });
      }
    }

    async function compactCurrent() {
      const threadId = (selectedThreadId.value || '').toString().trim();
      if (!threadId) return;
      try {
        await props.threadStore.compactThread(threadId);
      } catch (error) {
        logWarn('ui', 'chat.compact.failed', {
          thread_id: threadId,
          error,
        });
      }
    }

    async function forceCompleteCurrent() {
      const threadId = (selectedThreadId.value || '').toString().trim();
      if (!threadId) return;
      logInfo('ui', 'chat.forceComplete.request', { thread_id: threadId });
      try {
        await props.threadStore.forceCompleteThread(threadId);
      } catch (error) {
        logWarn('ui', 'chat.forceComplete.failed', {
          thread_id: threadId,
          error,
        });
      }
    }

    async function loadCurrentHistory() {
      const threadId = selectedThreadId.value;
      if (!threadId) return;
      await requestHistoryLoad(props.threadStore, threadId, { force: true, limit: 300 });
    }

    function selectThread(threadId) {
      selectedThreadId.value = threadId;
    }

    function refreshAll() {
      props.threadStore.refreshThreads();
    }

    function stopSelected() {
      props.threadStore.stopThread(selectedThreadId.value);
    }

    function renameSelected() {
      props.threadStore.promptRenameThread(selectedThreadId.value);
    }

    function setMainSelected() {
      props.threadStore.setMainAgent(selectedThreadId.value);
    }

    function loadCardHistory(cardId) {
      props.threadStore.loadMessages(cardId, 300);
    }

    function renameCard(cardId) {
      props.threadStore.promptRenameThread(cardId);
    }

    function stopCard(cardId) {
      props.threadStore.stopThread(cardId);
    }

    function getDisplayName(thread) {
      return props.threadStore.displayName(thread);
    }

    function setChatFocus() {
      layoutMode.value = 'focus';
    }

    function setChatMix() {
      layoutMode.value = 'mix';
    }

    function setCmdLayout(value) {
      layoutMode.value = value;
    }

    function setCmdCardCols(value) {
      cmdCardCols.value = value;
    }

    async function copySelectedThreadId() {
      const threadId = (selectedThreadId.value || '').toString();
      if (!threadId) return;
      const runtime = activeRuntime.value || {};
      let resolved = {};
      const existingCodexThreadID = (runtime.codexThreadId || '').toString().trim();
      if (!existingCodexThreadID) {
        try {
          resolved = await resolveThreadIdentity(threadId);
        } catch {
          resolved = {};
        }
      }
      const codexThreadID = existingCodexThreadID || (resolved.codexThreadId || '').toString().trim();
      const resolvedPort = Number.isFinite(Number(runtime.port))
        ? Number(runtime.port)
        : (Number.isFinite(Number(resolved.port)) ? Number(resolved.port) : null);
      const payload = {
        agentId: threadId,
        codexThreadId: codexThreadID,
        uuid: codexThreadID,
        name: activeThread.value ? props.threadStore.displayName(activeThread.value) : threadId,
        status: activeStatus.value,
        isMainAgent: threadId === mainAgentId.value,
        port: resolvedPort,
        copiedAt: new Date().toISOString(),
      };
      const text = JSON.stringify(payload, null, 2);
      if (copyStateTimer) {
        window.clearTimeout(copyStateTimer);
        copyStateTimer = 0;
      }
      try {
        const ok = await copyTextToClipboard(text);
        copyState.value = ok ? 'done' : 'error';
      } catch {
        copyState.value = 'error';
      }
      copyStateTimer = window.setTimeout(() => {
        copyState.value = 'idle';
        copyStateTimer = 0;
      }, 1200);
    }

    function timelinePreview(threadId) {
      const items = props.threadStore.getThreadTimeline(threadId) || [];
      return items
        .filter((item) => ['user', 'assistant', 'thinking', 'command', 'error'].includes(item.kind))
        .slice(-3)
        .map((item, index) => {
          const text = (item.text || item.command || '').toString().trim();
          if (!text) return null;
          const prefix = item.kind === 'user'
            ? '你: '
            : item.kind === 'assistant'
              ? '助手: '
              : item.kind === 'thinking'
                ? '思考: '
                : item.kind === 'command'
                  ? '$ '
                  : '错误: ';
          return {
            key: `${item.id || 'i'}-${index}`,
            text: `${prefix}${text}`.slice(0, 140),
          };
        })
        .filter(Boolean);
    }

    function diffPreview(threadId) {
      const text = (props.threadStore.getThreadDiff(threadId) || '').toString().trim();
      if (!text) return '';
      const lines = text.split('\n').slice(0, 4);
      return lines.join('\n');
    }

    function formatTokenCompact(value) {
      const number = Number(value);
      if (!Number.isFinite(number) || number < 0) return '0';
      if (number >= 1_000_000) return `${(number / 1_000_000).toFixed(1).replace(/\\.0$/, '')}m`;
      if (number >= 1_000) return `${(number / 1_000).toFixed(1).replace(/\\.0$/, '')}k`;
      return `${Math.round(number)}`;
    }

    function formatTokenPercent(value) {
      const number = Number(value);
      if (!Number.isFinite(number)) return '';
      const clamped = Math.max(0, Math.min(100, number));
      return `${Math.round(clamped)}%`;
    }

    function formatTokenInline(usage) {
      if (!usage || typeof usage !== 'object') return '';
      const used = Number(usage.usedTokens);
      const limit = Number(usage.contextWindowTokens);
      if (!Number.isFinite(used) || used <= 0) return '';
      if (Number.isFinite(limit) && limit > 0) {
        const usedPercent = Number.isFinite(Number(usage.usedPercent))
          ? Number(usage.usedPercent)
          : (used / limit) * 100;
        return `${formatTokenPercent(usedPercent)} · ${formatTokenCompact(used)} / ${formatTokenCompact(limit)}`;
      }
      return `${formatTokenCompact(used)} used`;
    }

    function formatTokenTooltip(usage) {
      if (!usage || typeof usage !== 'object') return '';
      const used = Number(usage.usedTokens);
      const limit = Number(usage.contextWindowTokens);
      if (!Number.isFinite(used) || used <= 0) return '';
      if (Number.isFinite(limit) && limit > 0) {
        const usedPercent = Number.isFinite(Number(usage.usedPercent))
          ? Number(usage.usedPercent)
          : (used / limit) * 100;
        const leftPercent = 100 - usedPercent;
        return [
          'Context window:',
          `${formatTokenPercent(usedPercent)} used (${formatTokenPercent(leftPercent)} left)`,
          `${formatTokenCompact(used)} / ${formatTokenCompact(limit)} tokens used`,
        ].join('\n');
      }
      return [
        'Context window:',
        `${formatTokenCompact(used)} tokens used`,
      ].join('\n');
    }

    function onResizeStart(event) {
      if (!showWorkspace.value) return;
      if (event.button !== 0) return;
      dragging.value = true;

      const onMove = (e) => {
        const root = workspaceRef.value;
        if (!root) return;
        const rect = root.getBoundingClientRect();
        if (!rect.width) return;
        const next = ((e.clientX - rect.left) / rect.width) * 100;
        splitRatio.value = Math.max(30, Math.min(75, Math.round(next)));
      };

      const onUp = () => {
        dragging.value = false;
        window.removeEventListener('mousemove', onMove);
        window.removeEventListener('mouseup', onUp);
      };

      window.addEventListener('mousemove', onMove);
      window.addEventListener('mouseup', onUp);
    }

    function ensureStatusTickTimer() {
      if (statusTickTimer) return;
      statusTickTimer = window.setInterval(() => {
        statusTick.value = Date.now();
      }, 1000);
    }

    function stopStatusTickTimer() {
      if (!statusTickTimer) return;
      window.clearInterval(statusTickTimer);
      statusTickTimer = 0;
    }

    function formatElapsedCompact(elapsedSeconds) {
      const seconds = Math.max(0, Math.floor(Number(elapsedSeconds) || 0));
      if (seconds < 60) return `${seconds}s`;
      if (seconds < 3600) {
        const minutes = Math.floor(seconds / 60);
        const sec = seconds % 60;
        return `${minutes}m ${sec.toString().padStart(2, '0')}s`;
      }
      const hours = Math.floor(seconds / 3600);
      const minutes = Math.floor((seconds % 3600) / 60);
      const sec = seconds % 60;
      return `${hours}h ${minutes.toString().padStart(2, '0')}m ${sec.toString().padStart(2, '0')}s`;
    }

    onMounted(() => {
      window.addEventListener('keydown', onGlobalEscape);
    });

    onBeforeUnmount(() => {
      window.removeEventListener('keydown', onGlobalEscape);
      dragging.value = false;
      if (scrollTimer) {
        cancelAnimationFrame(scrollTimer);
        scrollTimer = 0;
      }
      if (copyStateTimer) {
        window.clearTimeout(copyStateTimer);
        copyStateTimer = 0;
      }
      stopStatusTickTimer();
    });

    return {
      composer,
      isCmd,
      threads,
      mainAgentId,
      selectedThreadId,
      activeThread,
      chatThreadOptions,
      activeTimeline,
      activeDiffText,
      activeStatus,
      activeStatusHeader,
      activeStatusDetails,
      activeStatusMeta,
      activeTokenInline,
      activeTokenTooltip,
      canInterrupt,
      displayStatusText,
      noActiveThread,
      copyButtonLabel,
      layoutMode,
      cmdCardCols,
      splitRatio,
      showOverview,
      showWorkspace,
      stats,
      recentThreads,
      cmdCards,
      dragging,
      composerBarRef,
      workspaceRef,
      selectThread,
      launchOne,
      send,
      interruptCurrent,
      compactCurrent,
      forceCompleteCurrent,
      loadCurrentHistory,
      setChatFocus,
      setChatMix,
      setCmdLayout,
      setCmdCardCols,
      copySelectedThreadId,
      timelinePreview,
      diffPreview,
      onResizeStart,
      refreshAll,
      stopSelected,
      renameSelected,
      setMainSelected,
      loadCardHistory,
      renameCard,
      stopCard,
      getDisplayName,
    };
  },
  template: `
    <section class="page active unified-chat-page" :class="isCmd ? 'mode-cmd' : 'mode-chat'">
      <div class="chat-toolbar unified-toolbar">
        <ProjectSelect
          :model-value="projectStore.state.active"
          :options="projectStore.projectOptions.value"
          @update:model-value="projectStore.setActive($event)"
          @add-project="projectStore.quickAdd()"
        />

        <div class="mode-badge">{{ isCmd ? 'CMD' : 'CHAT' }}</div>

        <div class="layout-switch" v-if="isCmd">
          <button class="btn btn-ghost btn-xs" :class="{active: layoutMode==='overview'}" @click="setCmdLayout('overview')">A 紧凑</button>
          <button class="btn btn-ghost btn-xs" :class="{active: layoutMode==='chat'}" @click="setCmdLayout('chat')">B 对话</button>
          <button class="btn btn-ghost btn-xs" :class="{active: layoutMode==='mix'}" @click="setCmdLayout('mix')">C 混合</button>
        </div>

        <div class="layout-switch" v-if="isCmd">
          <button class="btn btn-ghost btn-xs" :class="{active: cmdCardCols===2}" @click="setCmdCardCols(2)">2列</button>
          <button class="btn btn-ghost btn-xs" :class="{active: cmdCardCols===3}" @click="setCmdCardCols(3)">3列</button>
        </div>

        <div class="layout-switch" v-else>
          <button class="btn btn-ghost btn-xs" :class="{active: layoutMode==='focus'}" @click="setChatFocus">对话优先</button>
          <button class="btn btn-ghost btn-xs" :class="{active: layoutMode==='mix'}" @click="setChatMix">混合</button>
        </div>

        <button class="btn btn-secondary btn-toolbar-sm" @click="launchOne">+ 启动 Agent</button>
        <button class="btn btn-ghost btn-toolbar-sm" @click="loadCurrentHistory">加载历史</button>
        <button class="btn btn-ghost btn-toolbar-sm" @click="refreshAll">刷新</button>

        <div v-if="!isCmd" class="thread-selector-group">
          <select class="agent-selector" v-model="selectedThreadId">
            <option v-for="thread in chatThreadOptions" :key="thread.id" :value="thread.id">{{ getDisplayName(thread) }}</option>
          </select>
          <button
            v-if="selectedThreadId"
            class="btn btn-ghost btn-xs"
            @click="copySelectedThreadId"
          >{{ copyButtonLabel }}</button>
        </div>
        <button
          v-if="!isCmd && selectedThreadId"
          class="btn btn-ghost btn-xs"
          @click="setMainSelected"
        >
          {{ selectedThreadId === mainAgentId ? '主Agent' : '设为主Agent' }}
        </button>
        <button
          v-if="!isCmd && selectedThreadId"
          class="btn btn-ghost btn-xs"
          @click="renameSelected"
        >改名</button>
        <button
          v-if="!isCmd && selectedThreadId"
          class="btn btn-ghost btn-xs"
          @click="stopSelected"
        >停止</button>
        <button
          v-if="!isCmd && selectedThreadId && activeStatus === 'running'"
          class="btn btn-ghost btn-xs btn-warning"
          @click="forceCompleteCurrent"
          title="强制完成当前 turn，重置状态机"
        >重链</button>
        <div v-if="!isCmd" class="chat-status" :title="selectedThreadId || '未选择会话'">
          <span class="status-dot" :class="activeStatus"></span>
          <span>{{ displayStatusText }}</span>
          <span
            v-if="activeStatusMeta"
            class="chat-status-meta"
          >{{ activeStatusMeta }}</span>
        </div>

      </div>

      <div class="unified-main">
        <div class="unified-center">
          <section v-if="isCmd" class="cmd-card-panel">
            <div class="overview-metrics">
              <div class="metric"><strong>{{ stats.total }}</strong><span>子Agent</span></div>
              <div class="metric"><strong>{{ stats.running }}</strong><span>执行中</span></div>
              <div class="metric"><strong>{{ stats.thinking }}</strong><span>思考/回复</span></div>
              <div class="metric"><strong>{{ stats.editing }}</strong><span>改文件</span></div>
              <div class="metric"><strong>{{ stats.error }}</strong><span>异常</span></div>
            </div>

            <div class="cmd-card-grid" :class="'cols-' + cmdCardCols">
              <article
                v-for="card in cmdCards"
                :key="card.id"
                class="cmd-thread-card"
                :class="['view-' + layoutMode, { active: card.selected }]"
                @click="selectThread(card.id)"
              >
                <header class="cmd-thread-card-head">
                  <div>
                    <strong>{{ card.name }}</strong>
                    <small>{{ card.id }}</small>
                  </div>
                  <span class="badge" :class="'badge-' + card.status">
                    {{ card.statusHeader }}
                  </span>
                </header>

                <div v-if="layoutMode !== 'overview'" class="cmd-thread-preview">
                  <p v-if="!card.selected" class="muted">点击卡片查看预览</p>
                  <template v-else>
                    <p v-for="line in card.preview" :key="line.key">{{ line.text }}</p>
                    <p v-if="card.preview.length === 0" class="muted">暂无消息</p>
                  </template>
                </div>

                <pre v-if="layoutMode === 'mix' && card.selected && card.diff" class="cmd-thread-diff">{{ card.diff }}</pre>

                <div class="cmd-thread-actions">
                  <button class="btn btn-ghost btn-xs" @click.stop="selectThread(card.id)">打开</button>
                  <button class="btn btn-ghost btn-xs" @click.stop="loadCardHistory(card.id)">历史</button>
                  <button class="btn btn-ghost btn-xs" @click.stop="renameCard(card.id)">改名</button>
                  <button class="btn btn-ghost btn-xs" @click.stop="stopCard(card.id)">停止</button>
                </div>
              </article>
            </div>
          </section>

          <section v-if="showOverview" class="agent-overview-panel">
            <div class="overview-metrics">
              <div class="metric"><strong>{{ stats.total }}</strong><span>子Agent</span></div>
              <div class="metric"><strong>{{ stats.running }}</strong><span>执行中</span></div>
              <div class="metric"><strong>{{ stats.thinking }}</strong><span>思考/回复</span></div>
              <div class="metric"><strong>{{ stats.editing }}</strong><span>改文件</span></div>
              <div class="metric"><strong>{{ stats.error }}</strong><span>异常</span></div>
            </div>
            <div class="overview-recent" v-if="recentThreads.length > 0">
              <span class="recent-label">最近活跃:</span>
              <button
                v-for="thread in recentThreads"
                :key="thread.id"
                class="recent-chip"
                :class="{active: thread.id === selectedThreadId}"
                @click="selectThread(thread.id)"
              >
                {{ getDisplayName(thread) }}
              </button>
            </div>
          </section>

          <div v-if="showWorkspace" class="workspace-area">
            <div ref="workspaceRef" id="agent-workspace" class="chat-workspace with-diff">
              <div id="chat-panel" class="chat-panel-only" :style="{ flex: '0 0 ' + splitRatio + '%' }">
                <div v-if="noActiveThread" class="chat-messages-vue">
                  <div class="diff-empty">选择或启动一个 Agent 开始对话</div>
                </div>
                <ChatTimeline
                  v-else
                  :items="activeTimeline"
                  :active-status="activeStatus"
                  :active-status-text="displayStatusText"
                />
              </div>

              <div class="panel-resizer" :class="{dragging}" @mousedown="onResizeStart"></div>

              <DiffPanel :diff-text="activeDiffText" :style="{ flex: '0 0 ' + (100 - splitRatio) + '%' }" />
            </div>

            <ComposerBar
              ref="composerBarRef"
              :composer="composer"
              :thread-id="selectedThreadId"
              :interruptible="canInterrupt"
              :token-inline="activeTokenInline"
              :token-tooltip="activeTokenTooltip"
              :disabled="noActiveThread"
              @send="send"
              @interrupt="interruptCurrent"
              @compact="compactCurrent"
            />
          </div>
        </div>
      </div>
    </section>
  `,
};
