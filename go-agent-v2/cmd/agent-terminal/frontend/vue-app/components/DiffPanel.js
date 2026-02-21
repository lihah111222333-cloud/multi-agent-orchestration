import { computed, watch, nextTick, ref } from '../../lib/vue.esm-browser.prod.js';
import { parseUnifiedDiff, diffStats } from '../services/diff.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';
import { renderAssistantMarkdown } from '../utils/assistant-markdown.js';

export const DiffPanel = {
  name: 'DiffPanel',
  props: {
    diffText: { type: String, default: '' },
    mediaPreview: { type: Object, default: null },
    markdownPreview: { type: Object, default: null },
    focusFile: { type: String, default: '' },
    focusLine: { type: Number, default: 0 },
  },
  setup(props) {
    const panelRef = ref(null);
    const lightboxOpen = ref(false);
    const files = computed(() => parseUnifiedDiff(props.diffText));
    const fileCountText = computed(() => `${files.value.length} file${files.value.length === 1 ? '' : 's'}`);
    const totals = computed(() => files.value.reduce(
      (acc, file) => {
        const stats = diffStats(file);
        acc.add += stats.add;
        acc.del += stats.del;
        return acc;
      },
      { add: 0, del: 0 },
    ));
    const hasMediaPreview = computed(() => {
      const src = (props.mediaPreview?.src || '').toString().trim();
      const fullSrc = (props.mediaPreview?.fullSrc || '').toString().trim();
      return Boolean(src || fullSrc);
    });
    const mediaThumbSrc = computed(() => {
      const src = (props.mediaPreview?.src || '').toString().trim();
      return src || (props.mediaPreview?.fullSrc || '').toString().trim();
    });
    const mediaFullSrc = computed(() => {
      const full = (props.mediaPreview?.fullSrc || '').toString().trim();
      return full || (props.mediaPreview?.src || '').toString().trim();
    });
    const mediaPath = computed(() => (props.mediaPreview?.path || '').toString().trim());
    const mediaType = computed(() => (props.mediaPreview?.mediaType || '').toString().trim());
    const mediaBytes = computed(() => {
      const size = Number(props.mediaPreview?.sizeBytes);
      return Number.isFinite(size) && size > 0 ? Math.floor(size) : 0;
    });
    const mediaMeta = computed(() => {
      const parts = [];
      if (mediaType.value) parts.push(mediaType.value);
      if (mediaBytes.value > 0) parts.push(formatBytes(mediaBytes.value));
      return parts.join(' · ');
    });
    const hasMarkdownPreview = computed(() => {
      const text = (props.markdownPreview?.text || '').toString();
      return Boolean(text.trim());
    });
    const markdownPath = computed(() => (props.markdownPreview?.path || '').toString().trim());
    const markdownMeta = computed(() => {
      if (!hasMarkdownPreview.value) return '';
      const startLineRaw = Number(props.markdownPreview?.startLine);
      const endLineRaw = Number(props.markdownPreview?.endLine);
      const totalLinesRaw = Number(props.markdownPreview?.totalLines);
      const startLine = Number.isFinite(startLineRaw) && startLineRaw > 0 ? Math.floor(startLineRaw) : 0;
      const endLine = Number.isFinite(endLineRaw) && endLineRaw >= startLine ? Math.floor(endLineRaw) : 0;
      const totalLines = Number.isFinite(totalLinesRaw) && totalLinesRaw > 0 ? Math.floor(totalLinesRaw) : 0;
      if (startLine <= 0 || endLine < startLine) return '';
      const range = startLine === endLine
        ? `第 ${startLine} 行`
        : `第 ${startLine}-${endLine} 行`;
      if (totalLines > 0 && endLine < totalLines) {
        return `${range}（片段，共 ${totalLines} 行）`;
      }
      if (totalLines > 0) {
        return `${range}（共 ${totalLines} 行）`;
      }
      return range;
    });
    const markdownHtml = computed(() => {
      if (!hasMarkdownPreview.value) return '';
      return renderAssistantMarkdown((props.markdownPreview?.text || '').toString());
    });
    const hasDiffPreview = computed(() => !hasMediaPreview.value && !hasMarkdownPreview.value);
    const headerTitle = computed(() => (hasMarkdownPreview.value ? 'Markdown 预览' : '代码变更'));
    const headerSubText = computed(() => (
      hasMarkdownPreview.value
        ? (markdownPath.value || 'markdown')
        : fileCountText.value
    ));

    const normalizedFocusFile = computed(() => normalizePath(props.focusFile));
    const normalizedFocusLine = computed(() => {
      const line = Number(props.focusLine);
      return Number.isFinite(line) && line > 0 ? Math.floor(line) : 0;
    });

    watch(
      () => props.diffText,
      (next, prev) => {
        if (next === prev) return;
        logDebug('ui', 'diffPanel.updated', {
          text_len: (next || '').length,
          files: files.value.length,
        });
      },
      { immediate: true },
    );

    watch(
      () => props.mediaPreview,
      () => {
        lightboxOpen.value = false;
      },
      { deep: true },
    );

    watch(
      () => [props.focusFile, props.focusLine, props.diffText, hasMediaPreview.value, hasMarkdownPreview.value],
      () => {
        if (!hasDiffPreview.value) return;
        const requestedPath = (props.focusFile || '').toString().trim();
        const requestedLine = Number(props.focusLine);
        if (requestedPath || (Number.isFinite(requestedLine) && requestedLine > 0)) {
          logInfo('ui', 'chat.diff.panel.focus.request', {
            focus_file: requestedPath,
            focus_line: requestedLine,
            diff_len: (props.diffText || '').length,
            file_count: files.value.length,
          });
        }
        syncFocus().catch(() => {});
      },
      { immediate: true },
    );

    function normalizePath(value) {
      return (value || '')
        .toString()
        .trim()
        .replace(/\\/g, '/')
        .replace(/^\.\/+/, '')
        .replace(/^(a|b)\//, '')
        .toLowerCase();
    }

    function baseName(path) {
      const normalized = normalizePath(path);
      if (!normalized) return '';
      const segments = normalized.split('/').filter(Boolean);
      return segments[segments.length - 1] || '';
    }

    function fileMatchesTarget(filePath, targetPath) {
      const file = normalizePath(filePath);
      const target = normalizePath(targetPath);
      if (!file || !target) return false;
      if (file === target) return true;
      if (file.endsWith(`/${target}`)) return true;
      if (target.endsWith(`/${file}`)) return true;
      const fileBase = baseName(file);
      const targetBase = baseName(target);
      return Boolean(fileBase && targetBase && fileBase === targetBase);
    }

    function isFocusedFile(file) {
      const target = normalizedFocusFile.value;
      if (!target) return false;
      return fileMatchesTarget(file?.filename, target);
    }

    function isFocusedLine(file, line) {
      if (!isFocusedFile(file)) return false;
      const target = normalizedFocusLine.value;
      if (!target) return false;
      const oldNo = Number(line?.oldNo);
      const newNo = Number(line?.newNo);
      return (Number.isFinite(oldNo) && oldNo === target)
        || (Number.isFinite(newNo) && newNo === target);
    }

    async function syncFocus() {
      if (!hasDiffPreview.value) return;
      if (!panelRef.value) {
        logWarn('ui', 'chat.diff.panel.focus.no_panel', {
          focus_file: normalizedFocusFile.value,
          focus_line: normalizedFocusLine.value,
        });
        return;
      }
      await nextTick();

      const root = panelRef.value;
      if (!root || typeof root.querySelector !== 'function') {
        logWarn('ui', 'chat.diff.panel.focus.invalid_root', {
          focus_file: normalizedFocusFile.value,
          focus_line: normalizedFocusLine.value,
        });
        return;
      }

      const line = root.querySelector('.diff-line.is-focused-line');
      if (line && typeof line.scrollIntoView === 'function') {
        logInfo('ui', 'chat.diff.panel.focus.line_hit', {
          focus_file: normalizedFocusFile.value,
          focus_line: normalizedFocusLine.value,
          line_text: ((line.textContent || '').toString().trim()).slice(0, 120),
        });
        line.scrollIntoView({ behavior: 'smooth', block: 'center' });
        return;
      }

      const file = root.querySelector('.diff-file-group.is-focused .diff-file-header');
      if (file && typeof file.scrollIntoView === 'function') {
        logInfo('ui', 'chat.diff.panel.focus.file_hit', {
          focus_file: normalizedFocusFile.value,
          focus_line: normalizedFocusLine.value,
          file_text: ((file.textContent || '').toString().trim()).slice(0, 120),
        });
        file.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        return;
      }

      logWarn('ui', 'chat.diff.panel.focus.miss', {
        focus_file: normalizedFocusFile.value,
        focus_line: normalizedFocusLine.value,
        file_count: files.value.length,
        file_sample: files.value.slice(0, 8).map((item) => (item?.filename || '').toString()),
      });
    }

    function linePrefix(type) {
      if (type === 'add') return '+';
      if (type === 'del') return '-';
      if (type === 'hunk') return '@';
      if (type === 'meta') return '·';
      return ' ';
    }

    function formatBytes(value) {
      const size = Number(value);
      if (!Number.isFinite(size) || size <= 0) return '';
      if (size >= 1024 * 1024) {
        return `${(size / (1024 * 1024)).toFixed(2)} MB`;
      }
      if (size >= 1024) {
        return `${(size / 1024).toFixed(1)} KB`;
      }
      return `${size} B`;
    }

    function openLightbox() {
      if (!mediaFullSrc.value) return;
      lightboxOpen.value = true;
    }

    function closeLightbox() {
      lightboxOpen.value = false;
    }

    return {
      panelRef,
      files,
      fileCountText,
      totals,
      hasMediaPreview,
      hasMarkdownPreview,
      hasDiffPreview,
      markdownPath,
      markdownMeta,
      markdownHtml,
      headerTitle,
      headerSubText,
      mediaThumbSrc,
      mediaFullSrc,
      mediaPath,
      mediaMeta,
      lightboxOpen,
      diffStats,
      linePrefix,
      isFocusedFile,
      isFocusedLine,
      openLightbox,
      closeLightbox,
    };
  },
  template: `
    <div id="diff-panel" ref="panelRef">
      <div class="diff-header">
        <div class="diff-header-main">
          <strong>{{ headerTitle }}</strong>
          <small>{{ headerSubText }}</small>
        </div>
        <div v-if="hasDiffPreview" class="diff-header-metrics">
          <span class="diff-metric add">+{{ totals.add }}</span>
          <span class="diff-metric del">-{{ totals.del }}</span>
        </div>
      </div>

      <div id="diff-content">
        <div v-if="hasMediaPreview" class="diff-media-card">
          <button class="diff-media-thumb-btn" type="button" @click="openLightbox" :title="mediaPath || '点击放大预览'" aria-label="放大图片预览">
            <img class="diff-media-thumb" :src="mediaThumbSrc" :alt="mediaPath || 'image preview'" />
          </button>
          <div class="diff-media-caption">
            <div class="diff-media-path" :title="mediaPath">{{ mediaPath || 'image' }}</div>
            <div v-if="mediaMeta" class="diff-media-meta">{{ mediaMeta }}</div>
          </div>
        </div>

        <div
          v-else-if="hasMarkdownPreview"
          class="diff-media-card chat-item-markdown codex-markdown-root"
          style="font-family: -apple-system, 'SF Pro Text', sans-serif; font-size: 13px; line-height: 1.62;"
        >
          <div class="diff-media-caption">
            <div class="diff-media-path" :title="markdownPath">{{ markdownPath || 'markdown' }}</div>
            <div v-if="markdownMeta" class="diff-media-meta">{{ markdownMeta }}</div>
          </div>
          <div style="padding: 12px 14px 14px;" v-html="markdownHtml"></div>
        </div>

        <div v-if="files.length === 0 && hasDiffPreview" class="diff-empty">暂无代码变更</div>

        <div
          v-if="hasDiffPreview"
          v-for="file in files"
          :key="file.filename"
          class="diff-file-group"
          :class="{ 'is-focused': isFocusedFile(file) }"
        >
          <div class="diff-file-header">
            <div class="diff-file-title">
              <span class="diff-file-caret">▾</span>
              <span class="diff-file-name">{{ file.filename }}</span>
            </div>
            <div class="diff-file-stats">
              <span class="diff-metric add">+{{ diffStats(file).add }}</span>
              <span class="diff-metric del">-{{ diffStats(file).del }}</span>
            </div>
          </div>
          <div class="diff-file-lines">
            <div
              v-for="(line, idx) in file.lines"
              :key="line.type + '-' + (line.oldNo || line.newNo || idx)"
              class="diff-line"
              :class="[line.type, { 'is-focused-line': isFocusedLine(file, line) }]"
            >
              <span class="diff-line-num old">{{ line.oldNo }}</span>
              <span class="diff-line-num new">{{ line.newNo }}</span>
              <span class="diff-line-prefix">{{ linePrefix(line.type) }}</span>
              <span class="diff-line-content">{{ line.text }}</span>
            </div>
          </div>
        </div>

        <div v-if="hasMediaPreview && lightboxOpen" class="diff-media-lightbox" @click.self="closeLightbox">
          <div class="diff-media-lightbox-inner">
            <button class="diff-media-lightbox-close" type="button" @click="closeLightbox" aria-label="关闭预览">×</button>
            <img class="diff-media-full" :src="mediaFullSrc" :alt="mediaPath || 'image preview'" />
            <div class="diff-media-lightbox-path" :title="mediaPath">{{ mediaPath || 'image' }}</div>
          </div>
        </div>
      </div>
    </div>
  `,
};
