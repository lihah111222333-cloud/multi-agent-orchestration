import { watch, computed, ref } from '../../lib/vue.esm-browser.prod.js';
import { logDebug } from '../services/log.js';

const VISIBLE_WINDOW = 100;

export const ChatTimeline = {
  name: 'ChatTimeline',
  props: {
    items: { type: Array, default: () => [] },
    activeStatus: { type: String, default: 'idle' },
    activeStatusText: { type: String, default: '' },
    activeStatusMeta: { type: String, default: '' },
    pinnedPlanVisible: { type: Boolean, default: false },
  },
  setup(props) {
    let updateSeq = 0;
    const visibleCount = ref(VISIBLE_WINDOW);

    // items 引用变化时重置窗口
    watch(
      () => props.items,
      () => { visibleCount.value = VISIBLE_WINDOW; },
    );

    watch(
      () => props.items.length,
      (next, prev) => {
        updateSeq += 1;
        const delta = Math.abs((Number(next) || 0) - (Number(prev) || 0));
        if (updateSeq % 20 !== 0 && delta <= 1) return;
        const last = props.items[props.items.length - 1] || null;
        logDebug('ui', 'timeline.updated', {
          seq: updateSeq,
          length: next || 0,
          last_kind: last?.kind || '',
        });
      },
      { immediate: true },
    );

    const visibleItems = computed(() => {
      const all = props.items;
      if (all.length <= visibleCount.value) return all;
      return all.slice(all.length - visibleCount.value);
    });

    const hasMore = computed(() => props.items.length > visibleCount.value);

    function showMore() {
      visibleCount.value += VISIBLE_WINDOW;
    }
    function roleLabel(item) {
      switch (item?.kind) {
        case 'user':
          return '你';
        case 'assistant':
          return '助手';
        case 'thinking':
          return '思考';
        case 'command':
          return '命令';
        case 'tool':
          return '工具';
        case 'file':
          return '文件';
        case 'approval':
          return '审批';
        case 'plan':
          return '计划';
        case 'error':
          return '错误';
        default:
          return '事件';
      }
    }

    function stateLabel(item) {
      if (!item) return '';
      if (item.kind === 'thinking') return item.done ? '完成' : '处理中';
      if (item.kind === 'command') {
        if (item.status === 'running') return '执行中';
        if (item.status === 'failed') return '失败';
        return '完成';
      }
      if (item.kind === 'tool') return item.status === 'failed' ? '失败' : '调用';
      if (item.kind === 'file') return item.status === 'saved' ? '已保存' : '修改中';
      if (item.kind === 'plan') return item.done ? '完成' : '进行中';
      if (item.kind === 'approval') return '待确认';
      return '';
    }

    function attachmentType(att) {
      return att?.kind === 'image' ? 'IMG' : 'FILE';
    }

    function attachmentPreview(att) {
      if (!att || att.kind !== 'image') return '';
      const preview = (att.previewUrl || '').toString().trim();
      if (preview) return preview;
      const path = (att.path || '').toString().trim();
      if (!path) return '';
      const lower = path.toLowerCase();
      if (lower.startsWith('http://')
        || lower.startsWith('https://')
        || lower.startsWith('data:image/')
        || lower.startsWith('file://')) {
        if (lower.startsWith('file://') && window.__WAILS_SHIM_DEBUG__) {
          return '';
        }
        return path;
      }
      if (window.__WAILS_SHIM_DEBUG__) {
        return '';
      }
      return encodeURI(`file://${path}`);
    }

    function formatTime(ts) {
      if (!ts) return '';
      const date = new Date(ts);
      if (Number.isNaN(date.getTime())) return '';
      return date.toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
      });
    }

    function bubbleRole(item) {
      if (item?.kind === 'user') return 'role-user';
      if (item?.kind === 'assistant') return 'role-assistant';
      return 'role-system';
    }

    function isDialog(item) {
      return item?.kind === 'user' || item?.kind === 'assistant';
    }

    function hasAvatar(item) {
      return isDialog(item);
    }

    function avatarText(item) {
      if (item?.kind === 'user') return 'U';
      if (item?.kind === 'assistant') return 'AI';
      return '';
    }

    const sharedStatusText = computed(() => (props.activeStatusText || '').toString().trim());

    const showAgentPresence = computed(() => {
      const text = sharedStatusText.value;
      if (!text || text === '未选择会话') return false;
      return true;
    });

    const presenceLabel = computed(() => sharedStatusText.value);
    const sharedStatusMeta = computed(() => (props.activeStatusMeta || '').toString().trim());

    return {
      visibleItems,
      hasMore,
      showMore,
      roleLabel,
      stateLabel,
      attachmentType,
      attachmentPreview,
      formatTime,
      bubbleRole,
      isDialog,
      hasAvatar,
      avatarText,
      showAgentPresence,
      presenceLabel,
      sharedStatusText,
      sharedStatusMeta,
    };
  },
  template: `
    <div class="chat-messages-vue" :class="{ 'has-plan-pin': pinnedPlanVisible }">
      <div v-if="items.length === 0" class="chat-empty">暂无消息，先发送一句话试试。</div>

      <div v-if="hasMore" class="chat-load-more">
        <button class="chat-load-more-btn" @click="showMore">显示更早消息 ({{ items.length - visibleItems.length }} 条)</button>
      </div>

      <article
        v-for="item in visibleItems"
        :key="item.id"
        class="chat-item"
        :class="['kind-' + item.kind, isDialog(item) ? 'dialog' : 'process', bubbleRole(item)]"
      >
        <template v-if="isDialog(item)">
          <div v-if="hasAvatar(item)" class="chat-item-avatar">{{ avatarText(item) }}</div>

          <section class="chat-item-bubble">
            <header class="chat-item-head">
              <span class="chat-item-role">{{ roleLabel(item) }}</span>
              <span v-if="stateLabel(item)" class="chat-item-status">{{ stateLabel(item) }}</span>
              <span class="chat-item-spacer"></span>
              <time class="chat-item-time">{{ formatTime(item.ts) }}</time>
            </header>
            <pre class="chat-item-body">{{ item.text }}</pre>
            <div v-if="(item.attachments || []).length > 0" class="chat-attachment-list">
              <span
                v-for="(att, idx) in item.attachments"
                :key="(att.path || att.name || '') + '-' + idx"
                class="chat-attachment-pill"
                :class="{ 'has-image': Boolean(attachmentPreview(att)) }"
              >
                <span class="attachment-kind">{{ attachmentType(att) }}</span>
                <span class="attachment-name">{{ att.name || att.path }}</span>
                <img
                  v-if="attachmentPreview(att)"
                  class="chat-attachment-image"
                  :src="attachmentPreview(att)"
                  :alt="att.name || 'image attachment'"
                />
              </span>
            </div>
          </section>
        </template>

        <section v-else class="chat-process-line">
          <header class="chat-process-head">
            <span class="chat-process-role">{{ roleLabel(item) }}</span>
            <span v-if="stateLabel(item)" class="chat-process-status">{{ stateLabel(item) }}</span>
            <span class="chat-item-spacer"></span>
            <time class="chat-process-time">{{ formatTime(item.ts) }}</time>
          </header>

          <template v-if="item.kind === 'thinking' || item.kind === 'plan' || item.kind === 'error'">
            <pre class="chat-process-text">{{ item.text }}</pre>
          </template>

          <template v-else-if="item.kind === 'command'">
            <pre class="chat-process-text chat-process-code">$ {{ item.command }}</pre>
            <pre v-if="item.output" class="chat-process-text chat-process-output">{{ item.output }}</pre>
            <div v-if="typeof item.exitCode !== 'undefined'" class="chat-process-foot">exit {{ item.exitCode }}</div>
          </template>

          <template v-else-if="item.kind === 'tool'">
            <div class="chat-process-row">
              <pre class="chat-process-text chat-process-code tool-call-name">{{ item.tool }}</pre>
              <div v-if="typeof item.elapsedMs !== 'undefined'" class="chat-process-foot tool-call-time">{{ item.elapsedMs }}ms</div>
            </div>
            <div v-if="item.file" class="chat-process-text chat-process-meta chat-item-truncate" :title="item.file">{{ item.file }}</div>
            <pre v-if="item.preview" class="chat-process-text chat-process-meta tool-preview">{{ item.preview }}</pre>
          </template>

          <template v-else-if="item.kind === 'file'">
            <div class="chat-process-text chat-process-meta chat-item-truncate" :title="item.file || '(unknown file)'">
              {{ item.file || '(unknown file)' }}
            </div>
          </template>

          <template v-else-if="item.kind === 'approval'">
            <div class="chat-process-text chat-process-meta">{{ item.command || '需要用户确认' }}</div>
          </template>
        </section>
      </article>
      <div v-if="showAgentPresence" class="chat-presence-row">
        <div class="chat-item-avatar chat-item-avatar-presence">AI</div>
        <div class="chat-status chat-status-presence">
          <span class="status-dot" :class="activeStatus"></span>
          <span>{{ presenceLabel }}</span>
          <span v-if="sharedStatusMeta" class="chat-status-meta">{{ sharedStatusMeta }}</span>
        </div>
      </div>
    </div>
  `,
};
