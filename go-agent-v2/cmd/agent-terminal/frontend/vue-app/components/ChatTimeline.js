import { watch } from '../../lib/vue.esm-browser.prod.js';
import { logDebug } from '../services/log.js';

export const ChatTimeline = {
  name: 'ChatTimeline',
  props: {
    items: { type: Array, default: () => [] },
    activeStatus: { type: String, default: 'idle' },
  },
  setup(props) {
    let updateSeq = 0;
    const presenceVisibleStatuses = new Set([
      'starting',
      'thinking',
      'waiting',
      'responding',
      'running',
      'editing',
      'syncing',
    ]);
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
    watch(
      () => props.activeStatus,
      (next, prev) => {
        if (next === prev) return;
        logDebug('ui', 'timeline.presence.status', {
          previous: prev || '',
          current: next || '',
          visible: showAgentPresence(next, props.items) ? 1 : 0,
        });
      },
      { immediate: true },
    );

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
        return path;
      }
      return `file://${path}`;
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

    function hasPendingProcess(items) {
      const list = Array.isArray(items) ? items : [];
      for (let index = list.length - 1; index >= 0; index -= 1) {
        const item = list[index];
        if (!item || !item.kind) continue;
        if (item.kind === 'thinking' && !item.done) return true;
        if (item.kind === 'plan' && !item.done) return true;
        if (item.kind === 'command' && item.status === 'running') return true;
        if (item.kind === 'file' && item.status === 'editing') return true;
        if (item.kind === 'approval' && item.status === 'pending') return true;
      }
      return false;
    }

    function latestPendingLabel(items) {
      const list = Array.isArray(items) ? items : [];
      for (let index = list.length - 1; index >= 0; index -= 1) {
        const item = list[index];
        if (!item || !item.kind) continue;
        if (item.kind === 'command' && item.status === 'running') return '执行中';
        if (item.kind === 'file' && item.status === 'editing') return '修改中';
        if (item.kind === 'approval' && item.status === 'pending') return '等待确认';
        if (item.kind === 'plan' && !item.done) return '规划中';
        if (item.kind === 'thinking' && !item.done) return '思考中';
      }
      return '';
    }

    function showAgentPresence(status, items = []) {
      const value = (status || '').toString();
      if (presenceVisibleStatuses.has(value)) return true;
      return hasPendingProcess(items);
    }

    function presenceLabel(status, items = []) {
      const value = (status || '').toString();
      if (value === 'starting') return '启动中';
      if (value === 'waiting') return '等待中';
      if (value === 'responding') return '回复中';
      if (value === 'running') return '执行中';
      if (value === 'editing') return '修改中';
      if (value === 'syncing') return '同步中';
      const pending = latestPendingLabel(items);
      if (pending) return pending;
      return '思考中';
    }

    return {
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
    };
  },
  template: `
    <div class="chat-messages-vue">
      <div v-if="items.length === 0" class="chat-empty">暂无消息，先发送一句话试试。</div>

      <article
        v-for="item in items"
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
                :key="idx"
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

      <div v-if="showAgentPresence(activeStatus, items)" class="chat-presence-row">
        <div class="chat-item-avatar chat-item-avatar-presence">AI</div>
        <div class="chat-presence-pill">
          <span class="chat-presence-dot"></span>
          <span>{{ presenceLabel(activeStatus, items) }}</span>
        </div>
      </div>
    </div>
  `,
};
