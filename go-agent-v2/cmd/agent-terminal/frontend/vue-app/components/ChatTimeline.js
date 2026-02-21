import { watch, computed, ref } from '../../lib/vue.esm-browser.prod.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';
import { renderAssistantMarkdown } from '../utils/assistant-markdown.js';
import { hasJsonRenderSpec, extractSpecBlocks } from '../services/json-render-engine.js';
import { JsonRenderer } from './JsonRenderer.js';

const VISIBLE_WINDOW = 100;

export const ChatTimeline = {
  name: 'ChatTimeline',
  components: { JsonRenderer },
  props: {
    items: { type: Array, default: () => [] },
    activeStatus: { type: String, default: 'idle' },
    activeStatusText: { type: String, default: '' },
    activeStatusMeta: { type: String, default: '' },
    pinnedPlanVisible: { type: Boolean, default: false },
  },
  emits: ['file-ref-click'],
  setup(props, { emit }) {
    let updateSeq = 0;
    const visibleCount = ref(VISIBLE_WINDOW);
    const assistantMarkdownCache = new Map();
    const attachmentHoverPreview = ref(null);

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

    function isBottomOnlyStatusItem(item) {
      const kind = (item?.kind || '').toString().trim();
      return kind === 'thinking' || kind === 'command';
    }

    const timelineItems = computed(() => {
      const all = Array.isArray(props.items) ? props.items : [];
      return all.filter((item) => !isBottomOnlyStatusItem(item));
    });

    const visibleItems = computed(() => {
      const all = timelineItems.value;
      if (all.length <= visibleCount.value) return all;
      return all.slice(all.length - visibleCount.value);
    });

    const hasMore = computed(() => timelineItems.value.length > visibleCount.value);

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

    function commandText(item) {
      if (!item || item.kind !== 'command') return '';
      const parts = [];
      if (item.command) parts.push(`$ ${item.command}`);
      if (item.output) parts.push(item.output);
      return parts.join('\n') || '$ ';
    }

    function displayFilePath(path) {
      const raw = (path || '').toString().trim();
      if (!raw) return '';
      return raw
        .replace(/^\/Users\/[^/]+\//, '~/')
        .replace(/^\/home\/[^/]+\//, '~/')
        .replace(/^C:\\Users\\[^\\]+\\/i, '~\\');
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

    function attachmentHoverPoint(event) {
      const x = Number(event?.clientX);
      const y = Number(event?.clientY);
      if (Number.isFinite(x) && Number.isFinite(y) && (x > 0 || y > 0)) {
        return { x, y };
      }
      const rect = event?.currentTarget?.getBoundingClientRect?.();
      if (rect) {
        return {
          x: rect.left + Math.min(rect.width, 32),
          y: rect.top + Math.min(rect.height, 32),
        };
      }
      return { x: 32, y: 32 };
    }

    function attachmentHoverPosition(pointX, pointY) {
      const margin = 14;
      const offset = 18;
      const previewWidth = 360;
      const previewHeight = 280;
      const viewportWidth = window.innerWidth || previewWidth + margin * 2;
      const viewportHeight = window.innerHeight || previewHeight + margin * 2;
      let left = pointX + offset;
      let top = pointY + offset;
      if (left + previewWidth > viewportWidth - margin) {
        left = Math.max(margin, pointX - previewWidth - offset);
      }
      if (top + previewHeight > viewportHeight - margin) {
        top = Math.max(margin, pointY - previewHeight - offset);
      }
      return { left, top };
    }

    function onAttachmentHoverMove(event, att) {
      const src = attachmentPreview(att);
      if (!src) {
        attachmentHoverPreview.value = null;
        return;
      }
      const point = attachmentHoverPoint(event);
      const pos = attachmentHoverPosition(point.x, point.y);
      attachmentHoverPreview.value = {
        src,
        alt: (att?.name || att?.path || 'image attachment').toString(),
        left: pos.left,
        top: pos.top,
      };
    }

    function onAttachmentHoverLeave() {
      attachmentHoverPreview.value = null;
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

    async function copyTextToClipboard(text) {
      const value = (text || '').toString();
      if (!value) return false;
      try {
        if (navigator?.clipboard?.writeText) {
          await navigator.clipboard.writeText(value);
          return true;
        }
      } catch (error) {
        logWarn('ui', 'timeline.copy.clipboard_api_failed', { error: String(error || '') });
      }
      try {
        const input = document.createElement('textarea');
        input.value = value;
        input.setAttribute('readonly', 'readonly');
        input.style.position = 'fixed';
        input.style.opacity = '0';
        input.style.pointerEvents = 'none';
        document.body.appendChild(input);
        input.select();
        const ok = document.execCommand('copy');
        document.body.removeChild(input);
        return Boolean(ok);
      } catch (error) {
        logWarn('ui', 'timeline.copy.exec_command_failed', { error: String(error || '') });
        return false;
      }
    }

    async function copyFilePath(path) {
      const target = (path || '').toString().trim();
      if (!target) return;
      const copied = await copyTextToClipboard(target);
      if (copied) {
        logInfo('ui', 'timeline.file_path_copied', { path: target });
      } else {
        logWarn('ui', 'timeline.file_path_copy_failed', { path: target });
      }
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

    function describeClickNode(node) {
      const el = node && node.nodeType === 3 ? node.parentElement : node;
      if (!el || typeof el !== 'object') return {};
      return {
        tag: (el.tagName || '').toString().toLowerCase(),
        class_name: (el.className || '').toString(),
        text_preview: ((el.textContent || '').toString().trim()).slice(0, 120),
      };
    }

    function onAssistantBodyClick(event) {
      const rawTarget = event?.target || null;
      const target = rawTarget && rawTarget.nodeType === 3 ? rawTarget.parentElement : rawTarget;
      logInfo('ui', 'chat.fileRef.click.entry', {
        target: describeClickNode(target),
      });
      let refNode = null;
      if (target && typeof target.closest === 'function') {
        refNode = target.closest('.chat-md-inline-code.is-file-ref, .chat-md-file-ref');
      }
      if (!refNode && typeof event?.composedPath === 'function') {
        const path = event.composedPath();
        refNode = path.find((node) => {
          if (!node || !node.classList || typeof node.classList.contains !== 'function') return false;
          return node.classList.contains('is-file-ref') || node.classList.contains('chat-md-file-ref');
        }) || null;
      }
      if (!refNode) {
        logWarn('ui', 'chat.fileRef.click.no_ref_node', {
          target: describeClickNode(target),
        });
        return;
      }
      const path = (refNode.getAttribute('data-file-path') || '').toString().trim();
      const lineRaw = Number(refNode.getAttribute('data-file-line') || 0);
      const line = Number.isFinite(lineRaw) && lineRaw > 0 ? Math.floor(lineRaw) : 1;
      const column = Number(refNode.getAttribute('data-file-column') || 0);
      if (!path) {
        logWarn('ui', 'chat.fileRef.click.no_path', {
          ref_text: (refNode.textContent || '').toString().trim(),
          line_raw: lineRaw,
          column_raw: column,
        });
        return;
      }
      if (typeof event.preventDefault === 'function') event.preventDefault();
      if (typeof event.stopPropagation === 'function') event.stopPropagation();
      const payload = {
        path,
        line,
        column: Number.isFinite(column) && column > 0 ? column : 0,
        raw: (refNode.textContent || '').toString().trim(),
      };
      logInfo('ui', 'chat.fileRef.click.emit', payload);
      emit('file-ref-click', payload);
    }

    /**
     * 检查文本是否包含 json-render spec 代码块。
     * @param {string} text
     * @returns {boolean}
     */
    function itemHasSpec(text) {
      return hasJsonRenderSpec(text);
    }

    /**
     * 将文本拆分为 text/spec 交替段落。
     * @param {string} text
     * @returns {Array<{ type: string, content?: string, spec?: object }>}
     */
    function splitBySpec(text) {
      return extractSpecBlocks(text);
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
      timelineItems,
      hasMore,
      showMore,
      roleLabel,
      stateLabel,
      commandText,
      displayFilePath,
      attachmentType,
      attachmentPreview,
      formatTime,
      bubbleRole,
      isDialog,
      hasAvatar,
      avatarText,
      renderAssistantBody,
      onAssistantBodyClick,
      onAttachmentHoverMove,
      onAttachmentHoverLeave,
      attachmentHoverPreview,
      copyFilePath,
      itemHasSpec,
      splitBySpec,
      showAgentPresence,
      presenceLabel,
      sharedStatusText,
      sharedStatusMeta,
    };
  },
  template: `
    <div
      class="chat-messages-vue hide-scrollbar"
      :class="{ 'has-plan-pin': pinnedPlanVisible }"
      @mouseleave="onAttachmentHoverLeave"
      @scroll.passive="onAttachmentHoverLeave"
    >
      <div v-if="timelineItems.length === 0" class="chat-empty">暂无消息，先发送一句话试试。</div>

      <div v-if="hasMore" class="chat-load-more">
        <button class="chat-load-more-btn" @click="showMore">显示更早消息 ({{ timelineItems.length - visibleItems.length }} 条)</button>
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
            <template v-if="item.kind === 'assistant'">
              <div v-if="!itemHasSpec(item.text)"
                class="chat-item-body chat-item-markdown codex-markdown-root"
                v-html="renderAssistantBody(item.text)"
                @click="onAssistantBodyClick"
              ></div>
              <div v-else class="chat-item-body chat-item-markdown codex-markdown-root jr-mixed" @click="onAssistantBodyClick">
                <template v-for="(part, pIdx) in splitBySpec(item.text)" :key="pIdx">
                  <div v-if="part.type === 'text'" v-html="renderAssistantBody(part.content)"></div>
                  <JsonRenderer v-else-if="part.spec" :spec="part.spec" />
                </template>
              </div>
            </template>
            <pre v-else class="chat-item-body chat-item-plain">{{ item.text }}</pre>
            <div v-if="(item.attachments || []).length > 0" class="chat-attachment-list">
              <span
                v-for="(att, idx) in item.attachments"
                :key="(att.path || att.name || '') + '-' + idx"
                class="chat-attachment-pill"
                :class="{ 'has-image': Boolean(attachmentPreview(att)) }"
                @mouseenter="onAttachmentHoverMove($event, att)"
                @mousemove="onAttachmentHoverMove($event, att)"
                @mouseleave="onAttachmentHoverLeave"
              >
                <span class="attachment-kind">{{ attachmentType(att) }}</span>
                <span class="attachment-name">{{ att.name || att.path }}</span>
                <img
                  v-if="attachmentPreview(att)"
                  class="chat-attachment-image"
                  :src="attachmentPreview(att)"
                  :alt="att.name || 'image attachment'"
                  loading="lazy"
                />
              </span>
            </div>
          </section>
        </template>

        <section v-else class="chat-process-line">
          <header v-if="item.kind !== 'thinking' && item.kind !== 'command'" class="chat-process-head" :class="{ 'chat-process-head-file': item.kind === 'file' }">
            <template v-if="item.kind === 'file'">
              <span class="chat-process-kind-icon" title="文件" aria-hidden="true">
                <svg viewBox="0 0 24 24" fill="none">
                  <path d="M6.75 3.75h7.5l3 3v13.5H6.75z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"></path>
                  <path d="M14.25 3.75v3h3" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"></path>
                </svg>
              </span>
            </template>
            <span v-else class="chat-process-role">{{ roleLabel(item) }}</span>
            <template v-if="item.kind === 'file' && stateLabel(item)">
              <span
                class="chat-process-state-icon"
                :class="item.status === 'saved' ? 'is-saved' : 'is-editing'"
                :title="stateLabel(item)"
                aria-hidden="true"
              >
                <svg v-if="item.status === 'saved'" viewBox="0 0 24 24" fill="none">
                  <path d="M5 12.5l4.2 4.2L19 6.9" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"></path>
                </svg>
                <svg v-else viewBox="0 0 24 24" fill="none">
                  <path d="M4.5 15.75V19.5h3.75L18.8 8.95l-3.75-3.75L4.5 15.75z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"></path>
                  <path d="M13.95 6.3l3.75 3.75" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"></path>
                </svg>
              </span>
            </template>
            <span v-else-if="stateLabel(item)" class="chat-process-status">{{ stateLabel(item) }}</span>
            <template v-if="item.kind === 'file'">
              <span class="chat-process-file-inline" :title="item.file || '(unknown file)'">
                {{ displayFilePath(item.file) || '(unknown file)' }}
              </span>
            </template>
            <span v-else class="chat-item-spacer"></span>
            <button
              v-if="item.kind === 'file'"
              class="chat-process-copy-btn"
              type="button"
              :title="item.file ? ('复制路径: ' + displayFilePath(item.file)) : '无可复制路径'"
              aria-label="复制文件路径"
              :disabled="!item.file"
              @click.stop="copyFilePath(item.file)"
            >
              <svg class="chat-process-copy-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <rect x="9" y="9" width="10" height="10" rx="2" stroke="currentColor" stroke-width="1.8"></rect>
                <rect x="5" y="5" width="10" height="10" rx="2" stroke="currentColor" stroke-width="1.8"></rect>
              </svg>
            </button>
            <time class="chat-process-time">{{ formatTime(item.ts) }}</time>
          </header>

          <template v-if="item.kind === 'thinking' || item.kind === 'plan' || item.kind === 'error'">
            <pre class="chat-process-text" :class="{ 'loading-shimmer': item.kind === 'thinking' && !item.done }">{{ item.text }}</pre>
          </template>

          <template v-else-if="item.kind === 'command'">
            <pre class="chat-process-text chat-process-terminal">{{ commandText(item) }}</pre>
          </template>

          <template v-else-if="item.kind === 'tool'">
            <div class="chat-process-row">
              <pre class="chat-process-text chat-process-code tool-call-name">{{ item.tool }}</pre>
              <div v-if="typeof item.elapsedMs !== 'undefined'" class="chat-process-foot tool-call-time">{{ item.elapsedMs }}ms</div>
            </div>
            <div v-if="item.file" class="chat-process-text chat-process-meta chat-file-path" :title="item.file">{{ displayFilePath(item.file) }}</div>
            <pre v-if="item.preview" class="chat-process-text chat-process-meta tool-preview">{{ item.preview }}</pre>
          </template>

          <template v-else-if="item.kind === 'approval'">
            <div class="chat-process-text chat-process-meta">{{ item.command || '需要用户确认' }}</div>
          </template>
        </section>
      </article>
      <div
        v-if="attachmentHoverPreview"
        class="chat-attachment-hover-preview"
        :style="{ left: attachmentHoverPreview.left + 'px', top: attachmentHoverPreview.top + 'px' }"
        aria-hidden="true"
      >
        <img :src="attachmentHoverPreview.src" :alt="attachmentHoverPreview.alt" />
      </div>
      <div v-if="showAgentPresence" class="chat-presence-row">
        <div class="chat-item-avatar chat-item-avatar-presence">AI</div>
        <div class="chat-status chat-status-presence">
          <svg
            v-if="activeStatus === 'thinking' || activeStatus === 'starting' || activeStatus === 'running' || activeStatus === 'responding'"
            class="chat-status-spinner"
            viewBox="0 0 24 24"
            fill="none"
            aria-hidden="true"
          >
            <circle class="chat-status-spinner-track" cx="12" cy="12" r="8.5"></circle>
            <circle class="chat-status-spinner-arc" cx="12" cy="12" r="8.5"></circle>
          </svg>
          <span v-else class="status-dot" :class="activeStatus"></span>
          <span :class="{ 'loading-shimmer': activeStatus === 'thinking' || activeStatus === 'responding' }">{{ presenceLabel }}</span>
          <span v-if="sharedStatusMeta" class="chat-status-meta" :class="{ 'hyperspeed-model-shimmer': activeStatus === 'thinking' }">{{ sharedStatusMeta }}</span>
        </div>
      </div>
    </div>
  `,
};
