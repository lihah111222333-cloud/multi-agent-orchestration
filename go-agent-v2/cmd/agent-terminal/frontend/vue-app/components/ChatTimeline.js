import { watch, computed, ref } from '../../lib/vue.esm-browser.prod.js';
import { logDebug } from '../services/log.js';
import { renderAssistantMarkdown } from '../utils/assistant-markdown.js';

const VISIBLE_WINDOW = 100;
const COMMAND_GROUP_PREVIEW_LIMIT = 3;
const COMMAND_LABEL_MAX_CHARS = 46;
const COMMAND_GROUP_GAP_MS = 90 * 1000;

export const ChatTimeline = {
  name: 'ChatTimeline',
  emits: ['open-code-ref'],
  props: {
    items: { type: Array, default: () => [] },
    activeStatus: { type: String, default: 'idle' },
    activeStatusText: { type: String, default: '' },
    activeStatusMeta: { type: String, default: '' },
  },
  setup(props, { emit }) {
    let updateSeq = 0;
    const visibleCount = ref(VISIBLE_WINDOW);
    const assistantMarkdownCache = new Map();

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

    const windowedItems = computed(() => {
      const all = props.items;
      return all.length <= visibleCount.value
        ? all
        : all.slice(all.length - visibleCount.value);
    });

    const visibleItems = computed(() => compactTimeline(windowedItems.value));

    const hiddenCount = computed(() => Math.max(0, props.items.length - windowedItems.value.length));
    const hasMore = computed(() => hiddenCount.value > 0);

    function showMore() {
      visibleCount.value += VISIBLE_WINDOW;
    }

    function commandStatusValue(item) {
      const status = (item?.status || '').toString().trim().toLowerCase();
      if (status === 'running') return 'running';
      if (status === 'failed') return 'failed';
      return 'completed';
    }

    function commandMinuteBucket(ts) {
      if (!ts) return '';
      const date = new Date(ts);
      if (Number.isNaN(date.getTime())) return '';
      const year = date.getFullYear();
      const month = String(date.getMonth() + 1).padStart(2, '0');
      const day = String(date.getDate()).padStart(2, '0');
      const hour = String(date.getHours()).padStart(2, '0');
      const minute = String(date.getMinutes()).padStart(2, '0');
      return `${year}-${month}-${day}T${hour}:${minute}`;
    }

    function commandTimestampMs(ts) {
      if (!ts) return Number.NaN;
      const ms = Date.parse(ts);
      return Number.isFinite(ms) ? ms : Number.NaN;
    }

    function compactCommandLabel(command) {
      const oneLine = (command || '').toString().replace(/\s+/g, ' ').trim();
      if (!oneLine) return '';
      if (oneLine.length <= COMMAND_LABEL_MAX_CHARS) return oneLine;
      return `${oneLine.slice(0, COMMAND_LABEL_MAX_CHARS)}…`;
    }

    function commandGroupId(item, segmentKey, minuteBucket, sequence) {
      const base = (item?.id || '').toString().trim();
      const ts = (item?.ts || '').toString().trim();
      const scope = minuteBucket || ts || 'no-ts';
      const ref = base || `idx-${scope}`;
      return `command-group-${segmentKey}-${scope}-${ref}-${sequence}`;
    }

    function makeCommandGroup(item, segmentKey, sequence) {
      const minuteBucket = commandMinuteBucket(item?.ts);
      const tsMs = commandTimestampMs(item?.ts);
      const group = {
        id: commandGroupId(item, segmentKey, minuteBucket, sequence),
        kind: 'command-group',
        ts: item?.ts || '',
        lastTs: item?.ts || '',
        lastTsMs: Number.isFinite(tsMs) ? tsMs : Number.NaN,
        minuteBucket,
        segmentKey,
        totalCommands: 0,
        completedCommands: 0,
        failedCommands: 0,
        runningCommands: 0,
        commandPreviewItems: [],
      };
      mergeIntoCommandGroup(group, item);
      return group;
    }

    function mergeIntoCommandGroup(group, item) {
      if (!group || !item) return group;
      const status = commandStatusValue(item);
      group.totalCommands += 1;
      if (status === 'running') group.runningCommands += 1;
      else if (status === 'failed') group.failedCommands += 1;
      else group.completedCommands += 1;

      const tsMs = commandTimestampMs(item.ts);
      if (Number.isFinite(tsMs)) {
        if (!Number.isFinite(group.lastTsMs) || tsMs >= group.lastTsMs) {
          group.lastTsMs = tsMs;
          group.lastTs = item.ts || group.lastTs || '';
        }
      }

      const preview = compactCommandLabel(item.command);
      if (
        preview
        && !group.commandPreviewItems.includes(preview)
        && group.commandPreviewItems.length < COMMAND_GROUP_PREVIEW_LIMIT
      ) {
        group.commandPreviewItems.push(preview);
      }
      return group;
    }

    function canMergeCommandGroup(group, item, segmentKey) {
      if (!group || group.kind !== 'command-group') return false;
      if (group.segmentKey !== segmentKey) return false;
      const nextBucket = commandMinuteBucket(item?.ts);
      if (group.minuteBucket && nextBucket && group.minuteBucket !== nextBucket) return false;
      const nextTsMs = commandTimestampMs(item?.ts);
      if (Number.isFinite(group.lastTsMs) && Number.isFinite(nextTsMs)) {
        if (Math.abs(nextTsMs - group.lastTsMs) > COMMAND_GROUP_GAP_MS) return false;
      }
      return true;
    }

    function compactTimeline(items) {
      if (!Array.isArray(items) || items.length === 0) return [];
      const output = [];
      let commandGroupIndex = -1;
      let dialogSegment = 0;
      let commandGroupSequence = 0;
      for (const item of items) {
        const kind = (item?.kind || '').toString();
        if (kind === 'user' || kind === 'assistant') {
          output.push(item);
          commandGroupIndex = -1;
          dialogSegment += 1;
          continue;
        }
        if (kind !== 'command') {
          output.push(item);
          continue;
        }
        const activeGroup = commandGroupIndex >= 0 ? output[commandGroupIndex] : null;
        if (canMergeCommandGroup(activeGroup, item, dialogSegment)) {
          mergeIntoCommandGroup(activeGroup, item);
          continue;
        }
        const nextGroup = makeCommandGroup(item, dialogSegment, commandGroupSequence);
        commandGroupSequence += 1;
        output.push(nextGroup);
        commandGroupIndex = output.length - 1;
      }
      return output;
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
        case 'command-group':
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
      if (item.kind === 'command-group') {
        const running = Number(item.runningCommands) || 0;
        const failed = Number(item.failedCommands) || 0;
        const completed = Number(item.completedCommands) || 0;
        if (running > 0) return '执行中';
        if (failed > 0 && completed > 0) return '部分失败';
        if (failed > 0) return '失败';
        return '完成';
      }
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

    function commandSummary(item) {
      if (!item || (item.kind !== 'command' && item.kind !== 'command-group')) return '';
      if (item.kind === 'command-group') {
        const total = Math.max(1, Number(item.totalCommands) || 0);
        const running = Math.max(0, Number(item.runningCommands) || 0);
        const failed = Math.max(0, Number(item.failedCommands) || 0);
        const completed = Math.max(0, Number(item.completedCommands) || 0);
        if (running > 0) {
          return total <= 1
            ? '命令执行中，详细记录已移至右下角面板'
            : `已触发 ${total} 条命令，${running} 条执行中，详细记录已移至右下角面板`;
        }
        if (failed > 0) {
          if (completed > 0) {
            return `已执行 ${total} 条命令，成功 ${completed} 条，失败 ${failed} 条，详细记录已移至右下角面板`;
          }
          return total <= 1
            ? '命令执行失败，详细记录已移至右下角面板'
            : `已执行 ${total} 条命令，失败 ${failed} 条，详细记录已移至右下角面板`;
        }
        return total <= 1
          ? '命令执行完成，详细记录已移至右下角面板'
          : `已执行 ${total} 条命令，全部完成，详细记录已移至右下角面板`;
      }
      const status = (item.status || '').toString().trim();
      const hasExitCode = Number.isFinite(Number(item.exitCode));
      const exitCode = hasExitCode ? Number(item.exitCode) : null;
      if (status === 'running') return '命令执行中，详细记录已移至右下角面板';
      if (status === 'failed') {
        return hasExitCode
          ? `命令执行失败（exit ${exitCode}），详细记录已移至右下角面板`
          : '命令执行失败，详细记录已移至右下角面板';
      }
      return hasExitCode
        ? `命令执行完成（exit ${exitCode}），详细记录已移至右下角面板`
        : '命令执行完成，详细记录已移至右下角面板';
    }

    function commandPreview(item) {
      if (!item || (item.kind !== 'command' && item.kind !== 'command-group')) return '';
      if (item.kind === 'command') {
        const label = compactCommandLabel(item.command);
        return label ? `执行命令：${label}` : '';
      }
      const labels = Array.isArray(item.commandPreviewItems) ? item.commandPreviewItems : [];
      if (labels.length === 0) return '';
      const total = Math.max(0, Number(item.totalCommands) || 0);
      const hidden = Math.max(0, total - labels.length);
      const base = `执行命令：${labels.join(' · ')}`;
      return hidden > 0 ? `${base} 等 ${hidden} 条` : base;
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

    function processTime(item) {
      if (!item) return '';
      if (item.kind !== 'command-group') return formatTime(item.ts);
      const start = formatTime(item.ts);
      const end = formatTime(item.lastTs);
      if (!start) return end;
      if (!end || end === start) return start;
      return `${start}-${end}`;
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

    function renderAssistantBody(text) {
      const key = (text || '').toString();
      if (!key) return '';
      if (assistantMarkdownCache.has(key)) {
        return assistantMarkdownCache.get(key) || '';
      }
      const html = renderAssistantMarkdown(key);
      assistantMarkdownCache.set(key, html);
      if (assistantMarkdownCache.size > 280) {
        const first = assistantMarkdownCache.keys().next().value;
        assistantMarkdownCache.delete(first);
      }
      return html;
    }

    function onMarkdownClick(event) {
      const target = event?.target;
      if (!target || typeof target.closest !== 'function') return;
      const link = target.closest('.chat-md-code-ref');
      if (!link) return;
      event.preventDefault();
      const filePath = (link.getAttribute('data-file-path') || '').toString().trim();
      const line = Number(link.getAttribute('data-line') || 0);
      const column = Number(link.getAttribute('data-column') || 0);
      if (!filePath || !Number.isFinite(line) || line <= 0) return;
      emit('open-code-ref', {
        filePath,
        line,
        column: Number.isFinite(column) && column > 0 ? column : 1,
      });
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
      hiddenCount,
      hasMore,
      showMore,
      roleLabel,
      stateLabel,
      commandSummary,
      commandPreview,
      attachmentType,
      attachmentPreview,
      formatTime,
      processTime,
      bubbleRole,
      isDialog,
      hasAvatar,
      avatarText,
      renderAssistantBody,
      onMarkdownClick,
      showAgentPresence,
      presenceLabel,
      sharedStatusText,
      sharedStatusMeta,
    };
  },
  template: `
    <div class="chat-messages-vue">
      <div v-if="items.length === 0" class="chat-empty">暂无消息，先发送一句话试试。</div>

      <div v-if="hasMore" class="chat-load-more">
        <button class="chat-load-more-btn" @click="showMore">显示更早消息 ({{ hiddenCount }} 条)</button>
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
            <div
              v-if="item.kind === 'assistant'"
              class="chat-item-body chat-item-markdown"
              @click="onMarkdownClick"
              v-html="renderAssistantBody(item.text)"
            ></div>
            <pre v-else class="chat-item-body chat-item-plain">{{ item.text }}</pre>
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
            <time class="chat-process-time">{{ processTime(item) }}</time>
          </header>

          <template v-if="item.kind === 'thinking' || item.kind === 'plan' || item.kind === 'error'">
            <pre class="chat-process-text">{{ item.text }}</pre>
          </template>

          <template v-else-if="item.kind === 'command' || item.kind === 'command-group'">
            <div class="chat-process-text chat-process-meta">{{ commandSummary(item) }}</div>
            <div v-if="commandPreview(item)" class="chat-process-text chat-process-meta chat-process-command-preview">{{ commandPreview(item) }}</div>
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
