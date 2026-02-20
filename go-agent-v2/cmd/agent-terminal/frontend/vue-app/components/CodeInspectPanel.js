import {
  ref,
  computed,
  watch,
  nextTick,
  onBeforeUnmount,
} from '../../lib/vue.esm-browser.prod.js';
import { callAPI } from '../services/api.js';
import { logDebug, logWarn } from '../services/log.js';

const PANEL_CONTEXT_LINES = 120;

export const CodeInspectPanel = {
  name: 'CodeInspectPanel',
  emits: ['close'],
  props: {
    reference: { type: Object, default: null },
    activeProject: { type: String, default: '.' },
    projectList: { type: Array, default: () => [] },
  },
  setup(props, { emit }) {
    const loading = ref(false);
    const loadError = ref('');
    const payload = ref(null);
    const codeScrollRef = ref(null);
    const jumpInputRef = ref(null);
    const jumpLineInput = ref('');
    const jumpInputEditing = ref(false);
    const hintText = ref('');
    const pulseLine = ref(0);
    const cursorLine = ref(0);
    const cursorColumn = ref(1);
    let pulseTimer = 0;
    let hintTimer = 0;
    let focusSignature = '';
    let requestSeq = 0;

    const hasReference = computed(() => Boolean(props.reference?.filePath));

    const selectedLine = computed(() => Number(cursorLine.value || payload.value?.line || props.reference?.line || 0));
    const selectedColumn = computed(() => Number(cursorColumn.value || payload.value?.column || props.reference?.column || 0));
    const displayPath = computed(() => {
      const relative = (payload.value?.relative || '').toString().trim();
      if (relative) return relative;
      return (payload.value?.filePath || props.reference?.filePath || '').toString().trim();
    });
    const languageLabel = computed(() => (payload.value?.language || '').toString().trim());
    const snippetStart = computed(() => Number(payload.value?.startLine || 0));
    const snippetEnd = computed(() => Number(payload.value?.endLine || 0));
    const totalLines = computed(() => Number(payload.value?.totalLines || 0));
    const currentRangeLabel = computed(() => {
      const start = snippetStart.value;
      const end = snippetEnd.value;
      if (!start || !end) return '';
      const total = totalLines.value;
      if (total > 0) return `${start}-${end} / ${total}`;
      return `${start}-${end}`;
    });
    const codeLines = computed(() => {
      if (!Array.isArray(payload.value?.snippet)) return [];
      return payload.value.snippet;
    });
    const diagnostics = computed(() => {
      if (!Array.isArray(payload.value?.diagnostics)) return [];
      return payload.value.diagnostics;
    });
    const diagnosticsByLine = computed(() => {
      const byLine = {};
      for (const item of diagnostics.value) {
        const line = Number(item?.line || 0);
        if (!Number.isFinite(line) || line <= 0) continue;
        if (!byLine[line]) {
          byLine[line] = item;
        }
      }
      return byLine;
    });
    const diagnosticsSorted = computed(() => diagnostics.value.slice().sort((a, b) => {
      const lineA = Number(a?.line || 0);
      const lineB = Number(b?.line || 0);
      if (lineA !== lineB) return lineA - lineB;
      return Number(a?.column || 0) - Number(b?.column || 0);
    }));
    const diagnosticsPreview = computed(() => diagnosticsSorted.value.slice(0, 6));

    function toPositiveInt(value, fallback = 1) {
      const parsed = Number(value);
      if (!Number.isFinite(parsed) || parsed <= 0) return fallback;
      return Math.round(parsed);
    }

    function clampLineToTotal(line) {
      const safe = toPositiveInt(line, 1);
      const total = totalLines.value;
      if (total > 0) return Math.min(safe, total);
      return safe;
    }

    function isLineInCurrentSnippet(line) {
      const start = snippetStart.value;
      const end = snippetEnd.value;
      if (start <= 0 || end < start) return false;
      return line >= start && line <= end;
    }

    function setCursor(line, column = 1) {
      cursorLine.value = clampLineToTotal(line);
      cursorColumn.value = toPositiveInt(column, 1);
    }

    function severityClass(item) {
      const value = (item?.severity || '').toString().toLowerCase();
      if (value.includes('error')) return 'sev-error';
      if (value.includes('warning')) return 'sev-warning';
      if (value.includes('hint')) return 'sev-hint';
      return 'sev-info';
    }

    function closePanel() {
      emit('close');
    }

    function isEditableElement(node) {
      if (!node || typeof node !== 'object') return false;
      const tag = (node.tagName || '').toString().toLowerCase();
      if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
      if (Boolean(node.isContentEditable)) return true;
      return false;
    }

    function prefersReducedMotion() {
      try {
        return Boolean(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      } catch {
        return false;
      }
    }

    function clearPulseTimer() {
      if (!pulseTimer) return;
      window.clearTimeout(pulseTimer);
      pulseTimer = 0;
    }

    function clearHintTimer() {
      if (!hintTimer) return;
      window.clearTimeout(hintTimer);
      hintTimer = 0;
    }

    function showHint(text, delay = 1800) {
      hintText.value = (text || '').toString().trim();
      if (!hintText.value) return;
      clearHintTimer();
      hintTimer = window.setTimeout(() => {
        hintText.value = '';
        hintTimer = 0;
      }, delay);
    }

    async function scrollToFocusedLine() {
      const line = selectedLine.value;
      if (!Number.isFinite(line) || line <= 0) return;
      await nextTick();
      const container = codeScrollRef.value;
      if (!container || typeof container.querySelector !== 'function') return;
      const target = container.querySelector('.code-inspect-line.focus');
      if (!target) return;
      const smooth = !prefersReducedMotion();
      const top = Math.max(0, target.offsetTop - Math.round(container.clientHeight * 0.35));
      try {
        container.scrollTo({
          top,
          behavior: smooth ? 'smooth' : 'auto',
        });
      } catch {
        container.scrollTop = top;
      }
      pulseLine.value = line;
      clearPulseTimer();
      pulseTimer = window.setTimeout(() => {
        pulseLine.value = 0;
        pulseTimer = 0;
      }, smooth ? 900 : 240);
    }

    async function loadCode(target = null) {
      const filePath = (target?.filePath || payload.value?.filePath || props.reference?.filePath || '').toString().trim();
      if (!filePath) {
        payload.value = null;
        loadError.value = '';
        loading.value = false;
        return;
      }

      const seq = ++requestSeq;
      const line = toPositiveInt(target?.line || selectedLine.value || props.reference?.line || 1, 1);
      const column = toPositiveInt(target?.column || selectedColumn.value || props.reference?.column || 1, 1);
      loading.value = true;
      loadError.value = '';

      try {
        const res = await callAPI('ui/code/open', {
          filePath,
          line,
          column,
          context: PANEL_CONTEXT_LINES,
          project: (props.activeProject || '.').toString(),
          projects: Array.isArray(props.projectList) ? props.projectList : [],
        });
        if (seq !== requestSeq) return;
        payload.value = res && typeof res === 'object' ? res : null;
        setCursor(Number(res?.line || line), Number(res?.column || column));
        if (!jumpInputEditing.value && selectedLine.value > 0) {
          jumpLineInput.value = String(selectedLine.value);
        }
        logDebug('ui', 'codeInspect.loaded', {
          file_path: filePath,
          line,
          column,
          snippet_lines: Array.isArray(res?.snippet) ? res.snippet.length : 0,
          diagnostics: Array.isArray(res?.diagnostics) ? res.diagnostics.length : 0,
        });
      } catch (error) {
        if (seq !== requestSeq) return;
        payload.value = null;
        loadError.value = (error?.message || error || '加载失败').toString();
        logWarn('ui', 'codeInspect.load.failed', {
          file_path: filePath,
          line,
          error,
        });
      } finally {
        if (seq === requestSeq) {
          loading.value = false;
        }
      }
    }

    async function jumpToLine(rawLine, rawColumn = selectedColumn.value, options = {}) {
      const parsed = Number(rawLine);
      if (!Number.isFinite(parsed) || parsed <= 0) {
        showHint('请输入有效行号');
        return;
      }
      const clampedLine = clampLineToTotal(parsed);
      const column = toPositiveInt(rawColumn, 1);
      if (totalLines.value > 0 && clampedLine !== Math.round(parsed)) {
        showHint(`已跳转到边界行 L${clampedLine}`);
      }
      setCursor(clampedLine, column);
      if (!jumpInputEditing.value) jumpLineInput.value = String(clampedLine);
      if (isLineInCurrentSnippet(clampedLine) && !options.forceReload) {
        focusSignature = '';
        await scrollToFocusedLine();
        return;
      }
      await loadCode({
        filePath: (payload.value?.filePath || props.reference?.filePath || '').toString().trim(),
        line: clampedLine,
        column,
      });
    }

    function submitJump() {
      jumpToLine(jumpLineInput.value).catch(() => { });
    }

    function focusJumpInput() {
      nextTick(() => {
        const el = jumpInputRef.value;
        if (!el || typeof el.focus !== 'function') return;
        el.focus();
        if (typeof el.select === 'function') {
          el.select();
        }
      });
    }

    function refreshCurrent() {
      loadCode({
        filePath: (payload.value?.filePath || props.reference?.filePath || '').toString().trim(),
        line: selectedLine.value || 1,
        column: selectedColumn.value || 1,
      }).catch(() => { });
    }

    function moveLine(delta) {
      const base = toPositiveInt(selectedLine.value || 1, 1);
      const nextLine = base + delta;
      jumpToLine(nextLine).catch(() => { });
    }

    function jumpToDiagnostic(item) {
      if (!item) return;
      jumpToLine(Number(item.line || 1), Number(item.column || 1)).catch(() => { });
    }

    function jumpToNextDiagnostic() {
      const list = diagnosticsSorted.value;
      if (list.length === 0) return;
      const current = toPositiveInt(selectedLine.value || 1, 1);
      const next = list.find((item) => Number(item?.line || 0) > current) || list[0];
      jumpToDiagnostic(next);
    }

    function jumpToPrevDiagnostic() {
      const list = diagnosticsSorted.value;
      if (list.length === 0) return;
      const current = toPositiveInt(selectedLine.value || 1, 1);
      let prev = list[list.length - 1];
      for (let index = list.length - 1; index >= 0; index -= 1) {
        const item = list[index];
        if (Number(item?.line || 0) < current) {
          prev = item;
          break;
        }
      }
      jumpToDiagnostic(prev);
    }

    function onPanelKeydown(event) {
      if (!event) return;
      const key = (event.key || '').toString();
      const lower = key.toLowerCase();

      if ((event.metaKey || event.ctrlKey) && lower === 'g') {
        event.preventDefault();
        focusJumpInput();
        return;
      }
      if (key === 'F8') {
        event.preventDefault();
        if (event.shiftKey) {
          jumpToPrevDiagnostic();
        } else {
          jumpToNextDiagnostic();
        }
        return;
      }

      if (isEditableElement(event.target)) return;

      if (key === 'ArrowDown' || lower === 'j') {
        event.preventDefault();
        moveLine(1);
      } else if (key === 'ArrowUp' || lower === 'k') {
        event.preventDefault();
        moveLine(-1);
      } else if (key === 'PageDown') {
        event.preventDefault();
        moveLine(20);
      } else if (key === 'PageUp') {
        event.preventDefault();
        moveLine(-20);
      }
    }

    function onLineClick(line) {
      jumpToLine(line, 1, { forceReload: false }).catch(() => { });
    }

    watch(
      () => [
        (props.reference?.filePath || '').toString(),
        Number(props.reference?.line || 0),
        Number(props.reference?.column || 0),
        Number(props.reference?.token || 0),
        (props.activeProject || '.').toString(),
      ],
      () => {
        if (!hasReference.value) {
          payload.value = null;
          cursorLine.value = 0;
          cursorColumn.value = 1;
          loadError.value = '';
          loading.value = false;
          jumpLineInput.value = '';
          return;
        }
        const line = toPositiveInt(props.reference?.line || 1, 1);
        const column = toPositiveInt(props.reference?.column || 1, 1);
        setCursor(line, column);
        if (!jumpInputEditing.value) {
          jumpLineInput.value = String(line);
        }
        focusSignature = '';
        loadCode({
          filePath: (props.reference?.filePath || '').toString().trim(),
          line,
          column,
        }).catch(() => { });
      },
      { immediate: true },
    );

    watch(
      () => selectedLine.value,
      (line) => {
        if (jumpInputEditing.value) return;
        if (!line || !Number.isFinite(line)) return;
        jumpLineInput.value = String(line);
      },
      { immediate: true },
    );

    watch(
      () => [loading.value, selectedLine.value, codeLines.value.length, displayPath.value],
      ([isLoading, line, length, path]) => {
        if (isLoading || !line || length <= 0 || !path) return;
        const signature = `${path}|${line}|${length}`;
        if (signature === focusSignature) return;
        focusSignature = signature;
        scrollToFocusedLine().catch(() => { });
      },
      { immediate: true },
    );

    onBeforeUnmount(() => {
      clearPulseTimer();
      clearHintTimer();
    });

    return {
      loading,
      loadError,
      displayPath,
      selectedLine,
      selectedColumn,
      languageLabel,
      snippetStart,
      snippetEnd,
      totalLines,
      currentRangeLabel,
      codeLines,
      diagnostics,
      diagnosticsByLine,
      diagnosticsSorted,
      diagnosticsPreview,
      severityClass,
      jumpInputRef,
      jumpLineInput,
      jumpInputEditing,
      hintText,
      submitJump,
      refreshCurrent,
      moveLine,
      jumpToDiagnostic,
      jumpToPrevDiagnostic,
      jumpToNextDiagnostic,
      onPanelKeydown,
      onLineClick,
      closePanel,
      codeScrollRef,
      pulseLine,
    };
  },
  template: `
    <section class="code-inspect-panel" tabindex="0" @keydown="onPanelKeydown">
      <header class="code-inspect-head">
        <div class="code-inspect-title">
          <div class="code-inspect-title-top">
            <strong>LSP 代码定位</strong>
            <span v-if="languageLabel" class="code-inspect-lang">{{ languageLabel }}</span>
          </div>
          <small v-if="displayPath" :title="displayPath">{{ displayPath }}</small>
        </div>
        <div class="code-inspect-meta">
          <span v-if="selectedLine > 0" class="code-inspect-pos">L{{ selectedLine }}<template v-if="selectedColumn > 0">:C{{ selectedColumn }}</template></span>
          <span v-if="currentRangeLabel" class="code-inspect-range">{{ currentRangeLabel }}</span>
        </div>
      </header>

      <div class="code-inspect-toolbar">
        <button type="button" class="code-inspect-btn" :disabled="diagnosticsSorted.length === 0" @click="jumpToPrevDiagnostic">上一个问题</button>
        <button type="button" class="code-inspect-btn" :disabled="diagnosticsSorted.length === 0" @click="jumpToNextDiagnostic">下一个问题</button>
        <label class="code-inspect-jump">
          <span>跳转</span>
          <input
            ref="jumpInputRef"
            v-model="jumpLineInput"
            class="code-inspect-jump-input"
            type="text"
            inputmode="numeric"
            pattern="[0-9]*"
            placeholder="行号"
            @keydown.enter.prevent="submitJump"
            @focus="jumpInputEditing = true"
            @blur="jumpInputEditing = false"
          />
        </label>
        <button type="button" class="code-inspect-btn" @click="submitJump">Go</button>
        <button type="button" class="code-inspect-btn" @click="refreshCurrent">刷新</button>
        <button type="button" class="code-inspect-close" @click="closePanel">返回变更</button>
      </div>
      <div v-if="hintText" class="code-inspect-hint">{{ hintText }}</div>

      <div class="code-inspect-body">
        <div v-if="loading" class="code-inspect-empty">正在加载代码...</div>
        <div v-else-if="loadError" class="code-inspect-empty is-error">{{ loadError }}</div>
        <div v-else-if="codeLines.length === 0" class="code-inspect-empty">未读取到代码内容</div>
        <template v-else>
          <div v-if="diagnosticsPreview.length > 0" class="code-inspect-diags">
            <div
              v-for="(item, idx) in diagnosticsPreview"
              :key="String(item.line) + '-' + String(item.column) + '-' + idx"
              class="code-inspect-diag"
              :class="severityClass(item)"
              :title="item.message"
              type="button"
              @click="jumpToDiagnostic(item)"
            >
              <span class="code-inspect-diag-pos">L{{ item.line }}</span>
              <span class="code-inspect-diag-msg">{{ item.message }}</span>
            </button>
          </div>

          <div ref="codeScrollRef" class="code-inspect-code">
            <div
              v-for="line in codeLines"
              :key="'line-' + line.line"
              class="code-inspect-line"
              :class="{ focus: Number(line.line) === selectedLine, 'has-diag': Boolean(diagnosticsByLine[Number(line.line)]), 'focus-pulse': Number(line.line) === pulseLine }"
              :title="diagnosticsByLine[Number(line.line)]?.message || ''"
            >
              <button type="button" class="code-inspect-line-no" @click.stop="onLineClick(Number(line.line))">{{ line.line }}</button>
              <span class="code-inspect-line-text">{{ line.text || ' ' }}</span>
            </div>
          </div>
        </template>
      </div>
      <footer class="code-inspect-footer">
        <span class="code-inspect-shortcuts">快捷键: Ctrl/Cmd+G 跳转 · F8 问题导航 · J/K 行间移动</span>
      </footer>
    </section>
  `,
};
