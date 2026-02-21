import { computed, watch, nextTick, ref } from '../../lib/vue.esm-browser.prod.js';
import { parseUnifiedDiff, diffStats } from '../services/diff.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';

export const DiffPanel = {
  name: 'DiffPanel',
  props: {
    diffText: { type: String, default: '' },
    focusFile: { type: String, default: '' },
    focusLine: { type: Number, default: 0 },
  },
  setup(props) {
    const panelRef = ref(null);
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
      () => [props.focusFile, props.focusLine, props.diffText],
      () => {
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

    return {
      panelRef,
      files,
      fileCountText,
      totals,
      diffStats,
      linePrefix,
      isFocusedFile,
      isFocusedLine,
    };
  },
  template: `
    <div id="diff-panel" ref="panelRef">
      <div class="diff-header">
        <div class="diff-header-main">
          <strong>代码变更</strong>
          <small>{{ fileCountText }}</small>
        </div>
        <div class="diff-header-metrics">
          <span class="diff-metric add">+{{ totals.add }}</span>
          <span class="diff-metric del">-{{ totals.del }}</span>
        </div>
      </div>

      <div id="diff-content">
        <div v-if="files.length === 0" class="diff-empty">暂无代码变更</div>

        <div
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
      </div>
    </div>
  `,
};
