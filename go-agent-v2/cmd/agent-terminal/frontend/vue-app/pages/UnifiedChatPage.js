import {
  ref,
  computed,
  watch,
  nextTick,
  onBeforeUnmount,
} from '../../lib/vue.esm-browser.prod.js';
import { ProjectSelect } from '../components/ProjectSelect.js';
import { ChatTimeline } from '../components/ChatTimeline.js';
import { DiffPanel } from '../components/DiffPanel.js';
import { ComposerBar } from '../components/ComposerBar.js';
import { statusLabel, normalizeStatus } from '../services/status.js';
import { copyTextToClipboard, resolveThreadIdentity } from '../services/api.js';
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

    function distanceFromBottom(el) {
      if (!el) return 0;
      return el.scrollHeight - el.scrollTop - el.clientHeight;
    }

    function isNearBottom(el, threshold = 96) {
      return distanceFromBottom(el) <= threshold;
    }

    function scheduleScrollToBottom(force = false) {
      if (scrollTimer) {
        window.clearTimeout(scrollTimer);
      }
      scrollTimer = window.setTimeout(async () => {
        await nextTick();
        const el = resolveChatScroller();
        if (!el) return;
        if (!force && !shouldAutoScroll.value) return;
        el.scrollTop = el.scrollHeight;
      }, 40);
    }

    const stats = computed(() => {
      const summary = {
        total: threads.value.length,
        running: 0,
        thinking: 0,
        editing: 0,
        error: 0,
      };
      for (const thread of threads.value) {
        const status = normalizeStatus(props.threadStore.getThreadStatus(thread.id));
        if (status === 'running') summary.running += 1;
        if (status === 'thinking' || status === 'responding' || status === 'waiting') summary.thinking += 1;
        if (status === 'editing') summary.editing += 1;
        if (status === 'error') summary.error += 1;
      }
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
      return threads.value.map((thread) => {
        const selected = thread.id === selectedThreadId.value;
        return {
          id: thread.id,
          name: props.threadStore.displayName(thread),
          status: props.threadStore.getThreadStatus(thread.id),
          selected,
          preview: layoutMode.value !== 'overview' && selected
            ? timelinePreview(thread.id)
            : [],
          diff: layoutMode.value === 'mix' && selected
            ? diffPreview(thread.id)
            : '',
        };
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

    async function loadCurrentHistory() {
      const threadId = selectedThreadId.value;
      if (!threadId) return;
      await requestHistoryLoad(props.threadStore, threadId, { force: true, limit: 300 });
    }

    function selectThread(threadId) {
      selectedThreadId.value = threadId;
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

    onBeforeUnmount(() => {
      dragging.value = false;
      if (scrollTimer) {
        window.clearTimeout(scrollTimer);
        scrollTimer = 0;
      }
      if (copyStateTimer) {
        window.clearTimeout(copyStateTimer);
        copyStateTimer = 0;
      }
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
      workspaceRef,
      statusLabel,
      selectThread,
      launchOne,
      send,
      loadCurrentHistory,
      setChatFocus,
      setChatMix,
      setCmdLayout,
      setCmdCardCols,
      copySelectedThreadId,
      timelinePreview,
      diffPreview,
      onResizeStart,
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

        <button class="btn btn-secondary" style="font-size:11px;padding:4px 10px" @click="launchOne">+ 启动 Agent</button>
        <button class="btn btn-ghost" style="font-size:11px;padding:4px 10px" @click="loadCurrentHistory">加载历史</button>
        <button class="btn btn-ghost" style="font-size:11px;padding:4px 10px" @click="threadStore.refreshThreads">刷新</button>

        <div v-if="!isCmd" class="thread-selector-group">
          <select class="agent-selector" v-model="selectedThreadId">
            <option v-for="thread in chatThreadOptions" :key="thread.id" :value="thread.id">{{ threadStore.displayName(thread) }}</option>
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
          @click="threadStore.setMainAgent(selectedThreadId)"
        >
          {{ selectedThreadId === mainAgentId ? '主Agent' : '设为主Agent' }}
        </button>
        <button
          v-if="!isCmd && selectedThreadId"
          class="btn btn-ghost btn-xs"
          @click="threadStore.promptRenameThread(selectedThreadId)"
        >改名</button>
        <button
          v-if="!isCmd && selectedThreadId"
          class="btn btn-ghost btn-xs"
          @click="threadStore.stopThread(selectedThreadId)"
        >停止</button>
        <div v-if="!isCmd" class="chat-status" :title="selectedThreadId || '未选择会话'">
          <span class="status-dot" :class="activeStatus"></span>
          <span>{{ noActiveThread ? '未选择会话' : statusLabel(activeStatus) }}</span>
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
                    {{ statusLabel(card.status) }}
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
                  <button class="btn btn-ghost btn-xs" @click.stop="threadStore.loadMessages(card.id, 300)">历史</button>
                  <button class="btn btn-ghost btn-xs" @click.stop="threadStore.promptRenameThread(card.id)">改名</button>
                  <button class="btn btn-ghost btn-xs" @click.stop="threadStore.stopThread(card.id)">停止</button>
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
                {{ threadStore.displayName(thread) }}
              </button>
            </div>
          </section>

          <div v-if="showWorkspace" class="workspace-area">
            <div ref="workspaceRef" id="agent-workspace" class="chat-workspace with-diff">
              <div id="chat-panel" class="chat-panel-only" :style="{ flex: '0 0 ' + splitRatio + '%' }">
                <div v-if="noActiveThread" class="chat-messages-vue">
                  <div class="diff-empty">选择或启动一个 Agent 开始对话</div>
                </div>
                <ChatTimeline v-else :items="activeTimeline" :active-status="activeStatus" />
              </div>

              <div class="panel-resizer" :class="{dragging}" @mousedown="onResizeStart"></div>

              <DiffPanel :diff-text="activeDiffText" :style="{ flex: '0 0 ' + (100 - splitRatio) + '%' }" />
            </div>

            <ComposerBar :composer="composer" :disabled="noActiveThread || threadStore.state.sending" @send="send" />
          </div>
        </div>
      </div>
    </section>
  `,
};
