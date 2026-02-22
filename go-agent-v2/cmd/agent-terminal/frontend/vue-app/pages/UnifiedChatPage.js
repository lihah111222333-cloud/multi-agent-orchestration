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
import { ActivityPanel } from '../components/ActivityPanel.js';
import { normalizeStatus } from '../services/status.js';
import { parseUnifiedDiff } from '../services/diff.js';
import { callAPI, copyTextToClipboard, onFilesDropped, resolveThreadIdentity } from '../services/api.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';
import { useComposerStore } from '../stores/composer.js';

/**
 * @typedef {'force' | 'explicit' | 'trigger'} SkillMatchType
 */

/**
 * @typedef {Object} SkillPreviewMatch
 * @property {string} name
 * @property {SkillMatchType} matchedBy
 * @property {string[]} matchedTerms
 */

/**
 * @typedef {Object} SkillPreviewQueuedRequest
 * @property {number} requestSeq
 * @property {string} threadId
 * @property {string} text
 */

/**
 * @typedef {Object} DiffLine
 * @property {string} type
 * @property {string} text
 * @property {string | number} [oldNo]
 * @property {string | number} [newNo]
 */

/**
 * @typedef {Object} DiffFile
 * @property {string} filename
 * @property {DiffLine[]} lines
 */

/**
 * @typedef {Object} FocusedDiffSelection
 * @property {string} filename
 * @property {string} diffText
 */

/**
 * @typedef {Object} CrossThreadDiffSelection
 * @property {string} threadId
 * @property {string} path
 */

/**
 * @typedef {Object} CodeOpenSnippetLine
 * @property {number} [line]
 * @property {string} [text]
 */

/**
 * @typedef {Object} CodeOpenResult
 * @property {boolean} [ok]
 * @property {string} [relative]
 * @property {string} [filePath]
 * @property {boolean} [image]
 * @property {string} [plugin]
 * @property {string} [mediaType]
 * @property {string} [previewURL]
 * @property {string} [thumbnailURL]
 * @property {number} [sizeBytes]
 * @property {number} [startLine]
 * @property {number} [endLine]
 * @property {number} [totalLines]
 * @property {string} [language]
 * @property {string | CodeOpenSnippetLine[]} [snippet]
 */

/**
 * @typedef {Object} ImagePreviewState
 * @property {string} src
 * @property {string} fullSrc
 * @property {string} path
 * @property {string} mediaType
 * @property {number} sizeBytes
 */

/**
 * @typedef {Object} MarkdownPreviewState
 * @property {string} path
 * @property {string} text
 * @property {number} startLine
 * @property {number} endLine
 * @property {number} totalLines
 */

/**
 * @typedef {Object} PendingFileRefFocus
 * @property {string} threadId
 * @property {string} path
 * @property {number} line
 * @property {string} requestedPath
 */

/**
 * @typedef {Object} ProcessActivityItem
 * @property {string} id
 * @property {string} time
 * @property {string} message
 * @property {'active' | 'done' | 'failed'} status
 * @property {'thinking' | 'command'} [kind]
 * @property {string} [title]
 * @property {string} [command]
 * @property {string} [output]
 * @property {number} [exitCode]
 * @property {boolean} [multiline]
 */

/**
 * @typedef {Object} ThreadIdentityInfo
 * @property {string} [codexThreadId]
 * @property {number | string | null} [port]
 */

/**
 * @param {{ loadMessages?: (threadId: string, limit?: number) => Promise<any>, getThreadTimeline?: (threadId: string) => any[] }} threadStore
 * @param {string} threadId
 */
export async function requestHistoryLoad(threadStore, threadId) {
  if (!threadId || typeof threadStore?.loadMessages !== 'function') {
    return false;
  }

  // 如果 timeline 已有数据, 跳过重复加载
  const existing = threadStore.getThreadTimeline(threadId);
  if (Array.isArray(existing) && existing.length > 0) {
    return false;
  }

  await threadStore.loadMessages(threadId);
  return true;
}

/**
 * @param {string} rawPath
 * @returns {string}
 */
function normalizeDiffPath(rawPath) {
  return (rawPath || '')
    .toString()
    .trim()
    .replace(/\\/g, '/')
    .replace(/^\.\/+/, '')
    .replace(/^(a|b)\//, '')
    .toLowerCase();
}

/**
 * @param {string} path
 * @returns {string}
 */
function basename(path) {
  const normalized = normalizeDiffPath(path);
  if (!normalized) return '';
  const segments = normalized.split('/').filter(Boolean);
  return segments[segments.length - 1] || '';
}

/**
 * @param {DiffFile[] | null | undefined} files
 * @param {string} targetPath
 * @returns {DiffFile | null}
 */
function pickDiffFile(files, targetPath) {
  const target = normalizeDiffPath(targetPath);
  const list = /** @type {DiffFile[]} */ (Array.isArray(files) ? files : []);
  if (!target || list.length === 0) return null;
  /** @type {DiffFile | null} */
  let best = null;
  let bestScore = -1;
  list.forEach((file) => {
    const filename = (file?.filename || '').toString();
    const normalizedFile = normalizeDiffPath(filename);
    if (!normalizedFile) return;
    let score = -1;
    if (normalizedFile === target) {
      score = 10_000 + normalizedFile.length;
    } else if (normalizedFile.endsWith(`/${target}`)) {
      score = 9_000 + target.length;
    } else if (target.endsWith(`/${normalizedFile}`)) {
      score = 8_000 + normalizedFile.length;
    } else {
      const fileBase = basename(normalizedFile);
      const targetBase = basename(target);
      if (fileBase && targetBase && fileBase === targetBase) {
        score = 2_000 + fileBase.length;
      }
    }
    if (score > bestScore) {
      bestScore = score;
      best = /** @type {DiffFile} */ (file);
    }
  });
  return bestScore < 0 ? null : best;
}

/**
 * @param {DiffFile | null | undefined} file
 * @returns {string}
 */
function serializeDiffFile(file) {
  if (!file || typeof file !== 'object') return '';
  const filename = (file.filename || '').toString().trim();
  if (!filename) return '';
  const lines = Array.isArray(file.lines) ? file.lines : [];
  const out = [
    `diff --git a/${filename} b/${filename}`,
    `--- a/${filename}`,
    `+++ b/${filename}`,
  ];
  lines.forEach((line) => {
    const type = (line?.type || '').toString();
    const text = (line?.text || '').toString();
    if (type === 'hunk') {
      out.push(text || '@@');
      return;
    }
    if (type === 'add') {
      out.push(`+${text}`);
      return;
    }
    if (type === 'del') {
      out.push(`-${text}`);
      return;
    }
    if (type === 'ctx') {
      out.push(` ${text}`);
      return;
    }
    if (type === 'meta') {
      out.push(text);
    }
  });
  return out.join('\n');
}

/**
 * @param {string} rawDiffText
 * @param {string} targetPath
 * @returns {FocusedDiffSelection | null}
 */
function buildFocusedDiffSelection(rawDiffText, targetPath) {
  const text = (rawDiffText || '').toString().trim();
  if (!text) return null;
  const files = /** @type {DiffFile[]} */ (parseUnifiedDiff(text));
  const target = /** @type {DiffFile | null} */ (pickDiffFile(files, targetPath));
  if (!target) return null;
  const focusedText = serializeDiffFile(target);
  if (!focusedText) return null;
  return {
    filename: (target.filename || '').toString().trim(),
    diffText: focusedText,
  };
}

/**
 * @param {CodeOpenResult | null | undefined} codeOpenResult
 * @returns {string}
 */
function buildSyntheticDiffFromCodeOpen(codeOpenResult) {
  const path = (codeOpenResult?.relative || codeOpenResult?.filePath || '').toString().trim();
  if (!path) return '';
  const snippetRaw = codeOpenResult?.snippet;
  const snippetLines = codeOpenSnippetLines(codeOpenResult);
  if (!Array.isArray(snippetLines) || snippetLines.length === 0) return '';
  const startLineRaw = Number(codeOpenResult?.startLine);
  const fallbackStartLine = Array.isArray(snippetRaw) ? Number(snippetRaw?.[0]?.line) : 0;
  const startLine = Number.isFinite(startLineRaw) && startLineRaw > 0
    ? Math.floor(startLineRaw)
    : (Number.isFinite(fallbackStartLine) && fallbackStartLine > 0 ? Math.floor(fallbackStartLine) : 1);
  const span = Math.max(1, snippetLines.length);
  return [
    `diff --git a/${path} b/${path}`,
    `--- a/${path}`,
    `+++ b/${path}`,
    `@@ -${startLine},${span} +${startLine},${span} @@`,
    ...snippetLines.map((line) => ` ${line}`),
  ].join('\n');
}

/**
 * @param {CodeOpenResult | null | undefined} codeOpenResult
 * @returns {string[]}
 */
function codeOpenSnippetLines(codeOpenResult) {
  const snippetRaw = codeOpenResult?.snippet;
  return Array.isArray(snippetRaw)
    ? snippetRaw.map((item) => (item?.text || '').toString())
    : ((snippetRaw || '').toString().split('\n'));
}

/**
 * @param {string} path
 * @returns {boolean}
 */
function isMarkdownPath(path) {
  return /\.md$/i.test((path || '').toString().trim());
}

/**
 * @param {CodeOpenResult | null | undefined} codeOpenResult
 * @returns {MarkdownPreviewState | null}
 */
function buildMarkdownPreviewFromCodeOpen(codeOpenResult) {
  if (!codeOpenResult || codeOpenResult.ok !== true) return null;
  const language = (codeOpenResult.language || '').toString().trim().toLowerCase();
  const resolvedPath = (codeOpenResult.relative || codeOpenResult.filePath || '').toString().trim();
  if (language !== 'markdown' && !isMarkdownPath(resolvedPath)) return null;
  const snippetLines = codeOpenSnippetLines(codeOpenResult);
  if (!Array.isArray(snippetLines) || snippetLines.length === 0) return null;
  const text = snippetLines.join('\n');
  if (!text.trim()) return null;

  const startLineRaw = Number(codeOpenResult?.startLine);
  const fallbackStartLine = Array.isArray(codeOpenResult?.snippet)
    ? Number(codeOpenResult?.snippet?.[0]?.line)
    : 0;
  const startLine = Number.isFinite(startLineRaw) && startLineRaw > 0
    ? Math.floor(startLineRaw)
    : (Number.isFinite(fallbackStartLine) && fallbackStartLine > 0 ? Math.floor(fallbackStartLine) : 1);
  const endLineRaw = Number(codeOpenResult?.endLine);
  const fallbackEndLine = startLine + Math.max(0, snippetLines.length - 1);
  const endLine = Number.isFinite(endLineRaw) && endLineRaw >= startLine
    ? Math.floor(endLineRaw)
    : fallbackEndLine;
  const totalLinesRaw = Number(codeOpenResult?.totalLines);
  const totalLines = Number.isFinite(totalLinesRaw) && totalLinesRaw > 0
    ? Math.floor(totalLinesRaw)
    : Math.max(endLine, snippetLines.length);

  return {
    path: resolvedPath,
    text,
    startLine,
    endLine,
    totalLines,
  };
}

/**
 * @param {string} path
 * @returns {boolean}
 */
function isPreviewableImagePath(path) {
  const value = (path || '').toString().trim().toLowerCase();
  if (!value) return false;
  return /\.(png|jpg|jpeg|svg)$/.test(value);
}

/**
 * @param {string} path
 * @returns {string}
 */
function toFilePreviewURL(path) {
  const raw = (path || '').toString().trim();
  if (!raw) return '';
  const lower = raw.toLowerCase();
  if (lower.startsWith('file://') || lower.startsWith('http://') || lower.startsWith('https://') || lower.startsWith('data:image/')) {
    return raw;
  }
  if (/^[a-z]:[\\/]/i.test(raw)) {
    return encodeURI(`file:///${raw.replace(/\\/g, '/')}`);
  }
  return encodeURI(`file://${raw}`);
}

/**
 * @param {CodeOpenResult | null | undefined} codeOpenResult
 * @returns {ImagePreviewState | null}
 */
function buildImagePreviewFromCodeOpen(codeOpenResult) {
  if (!codeOpenResult || codeOpenResult.ok !== true) return null;
  const mediaType = (codeOpenResult.mediaType || '').toString().trim().toLowerCase();
  const resolvedPath = (codeOpenResult.relative || codeOpenResult.filePath || '').toString().trim();
  const imageByType = mediaType === 'image/png'
    || mediaType === 'image/jpeg'
    || mediaType === 'image/svg+xml';
  const imageByPath = isPreviewableImagePath(resolvedPath);
  if (!codeOpenResult.image && !imageByType && !imageByPath) return null;

  const thumb = (codeOpenResult.thumbnailURL || '').toString().trim();
  const preview = (codeOpenResult.previewURL || '').toString().trim();
  const src = thumb || preview || toFilePreviewURL((codeOpenResult.filePath || '').toString().trim());
  const fullSrc = preview || src;
  if (!src || !fullSrc) return null;

  const size = Number(codeOpenResult.sizeBytes);
  return {
    src,
    fullSrc,
    path: resolvedPath,
    mediaType: mediaType || 'image/*',
    sizeBytes: Number.isFinite(size) && size > 0 ? Math.floor(size) : 0,
  };
}

/**
 * @param {string} targetPath
 * @param {string} candidatePath
 * @returns {number}
 */
function scoreDiffPathMatch(targetPath, candidatePath) {
  const target = normalizeDiffPath(targetPath);
  const candidate = normalizeDiffPath(candidatePath);
  if (!target || !candidate) return -1;
  if (candidate === target) return 10_000 + candidate.length;
  if (candidate.endsWith(`/${target}`)) return 9_000 + target.length;
  if (target.endsWith(`/${candidate}`)) return 8_000 + candidate.length;
  const targetBase = basename(target);
  const candidateBase = basename(candidate);
  if (targetBase && candidateBase && targetBase === candidateBase) {
    return 2_000 + targetBase.length;
  }
  return -1;
}

/**
 * @param {Record<string, string> | null | undefined} diffTextByThread
 * @param {string} targetPath
 * @param {string} [preferredThreadId]
 * @returns {CrossThreadDiffSelection | null}
 */
function findCrossThreadDiffSelection(diffTextByThread, targetPath, preferredThreadId = '') {
  const target = normalizeDiffPath(targetPath);
  if (!target || !diffTextByThread || typeof diffTextByThread !== 'object') return null;
  let bestScore = -1;
  /** @type {CrossThreadDiffSelection | null} */
  let bestSelection = null;
  for (const [threadIdRaw, rawDiffText] of Object.entries(diffTextByThread)) {
    const threadId = (threadIdRaw || '').toString().trim();
    const diffText = (rawDiffText || '').toString();
    if (!threadId || !diffText) continue;
    const files = /** @type {DiffFile[]} */ (parseUnifiedDiff(diffText));
    const matchedFile = /** @type {DiffFile | null} */ (pickDiffFile(files, targetPath));
    const resolvedPath = (matchedFile?.filename || '').toString().trim();
    if (!resolvedPath) continue;
    let score = scoreDiffPathMatch(targetPath, resolvedPath);
    if (score < 0) continue;
    if (threadId === preferredThreadId) {
      score += 100;
    }
    if (score > bestScore) {
      bestScore = score;
      bestSelection = {
        threadId,
        path: resolvedPath,
      };
    }
  }
  return bestSelection;
}

export const UnifiedChatPage = {
  name: 'UnifiedChatPage',
  components: {
    ProjectSelect,
    ChatTimeline,
    DiffPanel,
    ComposerBar,
    ActivityPanel,
  },
  props: {
    projectStore: { type: Object, required: true },
    threadStore: { type: Object, required: true },
    mode: { type: String, default: 'chat' },
  },
  /**
   * @param {{
   *  projectStore: any,
   *  threadStore: any,
   *  mode?: string,
   * }} props
   */
  setup(props) {
    const composer = useComposerStore();
    const composerBarRef = ref(null);
    const composerSkillMatches = /** @type {{ value: SkillPreviewMatch[] }} */ (ref([]));
    const composerSelectedSkillNames = /** @type {{ value: string[] }} */ (ref([]));
    const composerSkillPreviewLoading = ref(false);
    let composerSkillPreviewTimer = 0;
    let composerSkillPreviewSeq = 0;
    /** @type {SkillPreviewQueuedRequest} */
    let composerSkillPreviewQueued = { requestSeq: 0, threadId: '', text: '' };
    let hasComposerSkillPreviewQueued = false;
    let composerSkillPreviewLastSignature = '';
    let composerSkillPreviewLastWarnAt = 0;
    const workspaceRef = ref(null);
    const dragging = ref(false);
    const activityPanelDragging = ref(false);
    const copyState = ref('idle');
    let scrollTimer = 0;
    let copyStateTimer = 0;
    let offFilesDropped = () => { };
    let clearActivityPanelResizeListeners = () => { };
    const editingThreadId = ref('');
    const editingAlias = ref('');
    const renamingThreadId = ref('');
    const renameInputRefByThread = new Map();
    const focusedDiffPath = ref('');
    const focusedDiffLine = ref(0);
    const pendingFileRefFocus = /** @type {{ value: PendingFileRefFocus | null }} */ (ref(null));
    const fallbackDiffText = ref('');
    const fallbackMediaPreview = /** @type {{ value: ImagePreviewState | null }} */ (ref(null));
    const fallbackMarkdownPreview = /** @type {{ value: MarkdownPreviewState | null }} */ (ref(null));

    const isCmd = computed(() => props.mode === 'cmd');
    const modeKey = computed(() => (isCmd.value ? 'cmd' : 'chat'));

    const layoutMode = computed({
      get: () => props.threadStore.getLayout(modeKey.value),
      set: (/** @type {string} */ value) => props.threadStore.setLayout(modeKey.value, value),
    });
    const cmdCardCols = computed({
      get: () => (typeof props.threadStore.getCmdCardCols === 'function'
        ? props.threadStore.getCmdCardCols()
        : 3),
      set: (/** @type {number} */ value) => {
        if (typeof props.threadStore.setCmdCardCols === 'function') {
          props.threadStore.setCmdCardCols(value);
        }
      },
    });

    const splitRatio = ref(props.threadStore.getSplitRatio(modeKey.value));
    const ACTIVITY_PANEL_DEFAULT_HEIGHT = 124;
    const ACTIVITY_PANEL_MIN_HEIGHT = 124;
    const ACTIVITY_PANEL_MAX_HEIGHT = 460;
    const activityPanelHeight = ref(ACTIVITY_PANEL_DEFAULT_HEIGHT);

    const threads = computed(() => props.threadStore.getThreadsByMode(modeKey.value));
    const mainAgentId = computed(() => props.threadStore.state.mainAgentId || '');

    const selectedThreadId = computed({
      get: () => props.threadStore.getCurrentThreadId(modeKey.value) || '',
      set: (/** @type {string} */ value) => {
        if (isCmd.value) {
          props.threadStore.saveActiveCmdThread(value || '');
        } else {
          props.threadStore.saveActiveThread(value || '');
        }
      },
    });

    const activeThread = computed(() => threads.value.find((/** @type {any} */ item) => item.id === selectedThreadId.value) || null);
    const chatThreadOptions = computed(() => {
      if (isCmd.value) return [];
      return threads.value;
    });
    const chatThreadCards = computed(() => {
      if (isCmd.value) return [];
      const cards = chatThreadOptions.value.map((/** @type {any} */ thread) => {
        const threadID = thread.id;
        const displayName = (props.threadStore.displayName(thread) || '').toString().trim() || threadID;
        const pinnedAt = (typeof props.threadStore.getThreadPinnedAt === 'function')
          ? Number(props.threadStore.getThreadPinnedAt(threadID))
          : 0;
        const archivedAt = (typeof props.threadStore.getThreadArchivedAt === 'function')
          ? Number(props.threadStore.getThreadArchivedAt(threadID))
          : 0;
        const isArchived = Number.isFinite(archivedAt) && archivedAt > 0;
        return {
          id: threadID,
          name: displayName,
          showId: displayName === threadID,
          status: isArchived ? 'idle' : normalizeStatus(props.threadStore.getThreadStatus(threadID)),
          statusHeader: isArchived ? '已归档' : (getThreadStatusHeader(threadID) || '等待指示'),
          interruptible: isThreadInterruptible(threadID),
          pinnedAt,
          archivedAt,
          isArchived,
          isPinned: Number.isFinite(pinnedAt) && pinnedAt > 0,
          selected: threadID === selectedThreadId.value,
          isMain: threadID === mainAgentId.value,
        };
      });
      return cards.sort((/** @type {any} */ left, /** @type {any} */ right) => {
        const leftArchived = left.isArchived ? 1 : 0;
        const rightArchived = right.isArchived ? 1 : 0;
        return leftArchived - rightArchived;
      });
    });
    const showArchivedThreadList = ref(false);
    const chatActiveThreadCards = computed(() => chatThreadCards.value.filter((thread) => !thread.isArchived));
    const chatArchivedThreadCards = computed(() => chatThreadCards.value.filter((thread) => thread.isArchived));
    const visibleChatThreadCards = computed(() => (
      showArchivedThreadList.value ? chatArchivedThreadCards.value : chatActiveThreadCards.value
    ));
    const activeChatThreadCount = computed(() => chatThreadCards.value.filter((/** @type {any} */ thread) => !thread.isArchived).length);
    const archivedChatThreadCount = computed(() => chatThreadCards.value.filter((/** @type {any} */ thread) => thread.isArchived).length);

    const activeTimeline = computed(() => props.threadStore.getThreadTimeline(selectedThreadId.value));
    const activeThreadDiffText = computed(() => props.threadStore.getThreadDiff(selectedThreadId.value));
    const activeMediaPreview = computed(() => fallbackMediaPreview.value);
    const activeMarkdownPreview = computed(() => fallbackMarkdownPreview.value);
    const activeDiffText = computed(() => {
      if (activeMediaPreview.value?.src) return '';
      if (activeMarkdownPreview.value?.text) return '';
      const rawDiffText = (activeThreadDiffText.value || '').toString();
      const targetPath = (focusedDiffPath.value || '').toString().trim();
      if (!targetPath) return rawDiffText;
      const selection = buildFocusedDiffSelection(rawDiffText, targetPath);
      if (selection) return selection.diffText;
      const fallbackText = (fallbackDiffText.value || '').toString();
      if (!fallbackText) return rawDiffText;
      const fallbackSelection = buildFocusedDiffSelection(fallbackText, targetPath);
      if (fallbackSelection) return fallbackSelection.diffText;
      return fallbackText;
    });
    const activeDiffFocusFile = computed(() => (focusedDiffPath.value || '').toString().trim());
    const activeDiffFocusLine = computed(() => {
      const line = Number(focusedDiffLine.value);
      return Number.isFinite(line) && line > 0 ? Math.floor(line) : 0;
    });
    const activeStatus = computed(() => normalizeStatus(props.threadStore.getThreadStatus(selectedThreadId.value)));
    const dismissedPlanKeyByThread = ref({});
    /**
     * @param {string} threadId
     * @returns {boolean}
     */
    function isThreadInterruptible(threadId) {
      if (!threadId) return false;
      if (typeof props.threadStore.getThreadInterruptible !== 'function') return false;
      return Boolean(props.threadStore.getThreadInterruptible(threadId));
    }
    /**
     * @param {string} threadId
     * @returns {string}
     */
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
    const canInterrupt = computed(() => isThreadInterruptible(selectedThreadId.value));
    const compacting = computed(() => {
      if (typeof props.threadStore.getThreadCompacting !== 'function') return false;
      return props.threadStore.getThreadCompacting(selectedThreadId.value);
    });
    const displayStatusText = computed(() => {
      if (!selectedThreadId.value) return '未选择会话';
      return activeStatusHeader.value || '等待指示';
    });
    const activeTokenInline = computed(() => formatTokenInline(activeTokenUsage.value));
    const activeTokenTooltip = computed(() => formatTokenTooltip(activeTokenUsage.value));
    const activeActivityStats = computed(() => {
      if (typeof props.threadStore.getThreadActivityStats !== 'function') return {};
      return props.threadStore.getThreadActivityStats(selectedThreadId.value);
    });
    const activeAlerts = computed(() => {
      if (typeof props.threadStore.getThreadAlerts !== 'function') return [];
      return props.threadStore.getThreadAlerts(selectedThreadId.value);
    });
    /**
     * @param {string | number | Date | null | undefined} ts
     * @returns {string}
     */
    function formatTimelineTime(ts) {
      const raw = (ts || '').toString().trim();
      if (!raw) return '';
      const date = new Date(raw);
      if (Number.isNaN(date.getTime())) return '';
      return date.toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
      });
    }

    /**
     * @param {unknown} value
     * @returns {string}
     */
    function normalizeActivityOutput(value) {
      const text = (value || '').toString();
      if (!text.trim()) return '';
      const maxLen = 420;
      if (text.length <= maxLen) return text;
      return `${text.slice(0, maxLen)}\n...[truncated]`;
    }
    /**
     * @param {any} item
     * @param {number} index
     * @returns {ProcessActivityItem | null}
     */
    function toProcessActivityItem(item, index) {
      if (!item || typeof item !== 'object') return null;
      const kind = (item.kind || '').toString().trim();
      if (!kind) return null;
      if (kind === 'thinking') {
        const done = Boolean(item.done);
        return {
          id: (item.id || `${kind}-${index}`).toString(),
          time: formatTimelineTime(item.ts),
          message: done ? '思考完成' : '思考中',
          kind: 'thinking',
          status: done ? 'done' : 'active',
        };
      }
      if (kind === 'command') {
        const status = (item.status || '').toString().trim().toLowerCase();
        const commandText = (item.command || '').toString().trim();
        const title = commandText ? `$ ${commandText}` : 'Terminal command';
        const output = normalizeActivityOutput(item.output);
        const rawExitCode = Number(item.exitCode);
        const hasExitCode = Number.isFinite(rawExitCode);
        const exitCode = hasExitCode ? Math.trunc(rawExitCode) : undefined;
        if (status === 'running') {
          return {
            id: (item.id || `${kind}-${index}`).toString(),
            time: formatTimelineTime(item.ts),
            message: title,
            kind: 'command',
            title,
            command: commandText,
            output,
            status: 'active',
            multiline: Boolean(commandText || output),
          };
        }
        if (status === 'failed') {
          return {
            id: (item.id || `${kind}-${index}`).toString(),
            time: formatTimelineTime(item.ts),
            message: title,
            kind: 'command',
            title,
            command: commandText,
            output,
            status: 'failed',
            exitCode,
            multiline: Boolean(output),
          };
        }
        return {
          id: (item.id || `${kind}-${index}`).toString(),
          time: formatTimelineTime(item.ts),
          message: title,
          kind: 'command',
          title,
          command: commandText,
          output,
          status: 'done',
          exitCode,
          multiline: Boolean(output),
        };
      }
      return null;
    }
    const activeProcessActivity = computed(() => {
      const list = Array.isArray(activeTimeline.value) ? activeTimeline.value : [];
      const items = /** @type {ProcessActivityItem[]} */ ([]);
      let lastSignature = '';
      for (let index = 0; index < list.length; index += 1) {
        const entry = toProcessActivityItem(list[index], index);
        if (!entry) continue;
        const signature = `${entry.message}|${entry.status}`;
        if (signature === lastSignature) continue;
        lastSignature = signature;
        items.push(entry);
      }
      return items.slice(-12).reverse();
    });
    const isStatusTimerModalPaused = computed(() => Boolean(props.projectStore?.state?.showModal));
    const statusSinceByThread = /** @type {{ value: Record<string, number> }} */ (ref({}));
    const statusPausedAtByThread = /** @type {{ value: Record<string, number> }} */ (ref({}));
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
    const chatComposerShellStyle = computed(() => {
      if (isCmd.value) return {};
      const ratio = Math.max(30, Math.min(75, Math.round(Number(splitRatio.value) || 60)));
      return {
        width: `calc(${ratio}% - 6px)`,
      };
    });
    function clampActivityPanelHeight(value, maxHeight = ACTIVITY_PANEL_MAX_HEIGHT) {
      const number = Number(value);
      const fallback = ACTIVITY_PANEL_DEFAULT_HEIGHT;
      const normalized = Number.isFinite(number) ? Math.round(number) : fallback;
      const cappedMax = Math.max(
        ACTIVITY_PANEL_MIN_HEIGHT,
        Math.floor(Number(maxHeight) || ACTIVITY_PANEL_MAX_HEIGHT),
      );
      return Math.max(ACTIVITY_PANEL_MIN_HEIGHT, Math.min(cappedMax, normalized));
    }
    const activityPanelRowStyle = computed(() => {
      if (isCmd.value) return {};
      return {
        '--activity-panel-base-height': `${ACTIVITY_PANEL_MIN_HEIGHT}px`,
        '--activity-panel-overlay-height': `${clampActivityPanelHeight(activityPanelHeight.value)}px`,
        '--activity-panel-fixed-height': `${ACTIVITY_PANEL_MIN_HEIGHT}px`,
      };
    });
    const latestPlanItem = computed(() => {
      if (isCmd.value) return null;
      const list = activeTimeline.value || [];
      for (let index = list.length - 1; index >= 0; index -= 1) {
        const item = list[index];
        if (item?.kind !== 'plan') continue;
        const text = (item.text || '').toString().trim();
        if (!text) continue;
        return item;
      }
      return null;
    });
    function resolvePlanItemKey(item) {
      if (!item || typeof item !== 'object') return '';
      const id = (item.id || '').toString().trim();
      if (id) return `id:${id}`;
      const timestamp = (item.ts || '').toString().trim();
      const done = item.done ? '1' : '0';
      const text = (item.text || '').toString().trim();
      if (!text) return '';
      if (timestamp) return `ts:${timestamp}|done:${done}|${text}`;
      return `done:${done}|${text}`;
    }
    const activePinnedPlan = computed(() => {
      const threadId = (selectedThreadId.value || '').toString().trim();
      if (!threadId) return null;
      const item = latestPlanItem.value;
      if (!item) return null;
      const key = resolvePlanItemKey(item);
      if (!key) return null;
      const dismissedKey = (dismissedPlanKeyByThread.value?.[threadId] || '').toString();
      if (dismissedKey && dismissedKey === key) return null;
      const text = (item.text || '').toString().trim();
      if (!text) return null;
      return {
        key,
        threadId,
        done: Boolean(item.done),
        statusText: item.done ? '完成' : '进行中',
        text,
      };
    });
    function dismissPinnedPlan() {
      const plan = activePinnedPlan.value;
      if (!plan) return;
      dismissedPlanKeyByThread.value = {
        ...dismissedPlanKeyByThread.value,
        [plan.threadId]: plan.key,
      };
    }

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

    function isComposerTextarea(node) {
      if (!node || typeof node !== 'object') return false;
      const tag = (node.tagName || '').toString().toLowerCase();
      if (tag !== 'textarea') return false;
      const id = (node.id || '').toString().trim();
      if (id === 'chatInput') return true;
      if (typeof node.closest === 'function') {
        return Boolean(node.closest('#chat-input-bar'));
      }
      return false;
    }

    function isEscapeKeyEvent(event) {
      const key = (event?.key || '').toString();
      if (key === 'Escape' || key === 'Esc') return true;
      const code = (event?.code || '').toString();
      if (code === 'Escape') return true;
      const keyCode = Number(event?.keyCode || event?.which || 0);
      return keyCode === 27;
    }

    function onGlobalEscape(event) {
      if (!isEscapeKeyEvent(event)) return;
      if (event?.repeat) return;
      if (!selectedThreadId.value) return;
      if (!canInterrupt.value) return;
      if (isStatusTimerModalPaused.value) return;
      const activeEl = typeof document !== 'undefined' ? document.activeElement : null;
      const inComposerTextarea = isComposerTextarea(event?.target) || isComposerTextarea(activeEl);
      if (!inComposerTextarea && (isEditableElement(event?.target) || isEditableElement(activeEl))) return;
      if (event && event.__aoGlobalEscapeHandled) return;
      if (event) event.__aoGlobalEscapeHandled = true;
      if (typeof event?.preventDefault === 'function') event.preventDefault();
      stopSelected();
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

    function clearComposerSkillPreviewTimer() {
      if (!composerSkillPreviewTimer) return;
      window.clearTimeout(composerSkillPreviewTimer);
      composerSkillPreviewTimer = 0;
    }

    /**
     * @param {any} rawMatches
     * @returns {SkillPreviewMatch[]}
     */
    function normalizeSkillPreviewMatches(rawMatches) {
      if (!Array.isArray(rawMatches)) return [];
      const deduped = /** @type {SkillPreviewMatch[]} */ ([]);
      const seenNames = /** @type {Set<string>} */ (new Set());
      rawMatches.forEach((raw) => {
        const name = (raw?.name || raw?.skill || '').toString().trim();
        if (!name) return;
        const lowerName = name.toLowerCase();
        if (seenNames.has(lowerName)) return;
        seenNames.add(lowerName);
        const matchedByRaw = (raw?.matched_by || raw?.matchedBy || '').toString().trim().toLowerCase();
        /** @type {SkillMatchType} */
        const matchedBy = matchedByRaw === 'force'
          ? 'force'
          : (matchedByRaw === 'explicit' ? 'explicit' : 'trigger');
        const sourceTermsRaw = Array.isArray(raw?.matched_terms)
          ? raw.matched_terms
          : (Array.isArray(raw?.matchedTerms) ? raw.matchedTerms : []);
        const sourceTerms = /** @type {any[]} */ (sourceTermsRaw);
        const terms = /** @type {string[]} */ ([]);
        const seenTerms = /** @type {Set<string>} */ (new Set());
        sourceTerms.forEach((rawTerm) => {
          const term = (rawTerm || '').toString().trim();
          if (!term) return;
          const lowerTerm = term.toLowerCase();
          if (seenTerms.has(lowerTerm)) return;
          seenTerms.add(lowerTerm);
          terms.push(term);
        });
        /** @type {SkillPreviewMatch} */
        const match = {
          name,
          matchedBy,
          matchedTerms: terms,
        };
        deduped.push(match);
      });
      return deduped;
    }

    function skillNameKey(rawName) {
      return (rawName || '').toString().trim().toLowerCase();
    }

    function isComposerSkillSelected(rawName) {
      const nameKey = skillNameKey(rawName);
      if (!nameKey) return false;
      return composerSelectedSkillNames.value.some((name) => skillNameKey(name) === nameKey);
    }

    function setComposerSelectedSkill(rawName, selected) {
      const normalized = (rawName || '').toString().trim();
      const nameKey = skillNameKey(normalized);
      if (!nameKey) return;
      const next = composerSelectedSkillNames.value.filter((name) => skillNameKey(name) !== nameKey);
      if (selected) {
        next.push(normalized);
      }
      composerSelectedSkillNames.value = next;
    }

    function toggleComposerSelectedSkill(rawName) {
      const selected = isComposerSkillSelected(rawName);
      setComposerSelectedSkill(rawName, !selected);
    }

    function clearComposerSelectedSkills() {
      if (composerSelectedSkillNames.value.length === 0) return;
      composerSelectedSkillNames.value = [];
    }

    function selectAllComposerSuggestedSkills() {
      if (!Array.isArray(composerSkillMatches.value) || composerSkillMatches.value.length === 0) return;
      const next = /** @type {string[]} */ ([]);
      const seen = new Set();
      composerSkillMatches.value.forEach((match) => {
        const name = (match?.name || '').toString().trim();
        const key = skillNameKey(name);
        if (!key || seen.has(key)) return;
        seen.add(key);
        next.push(name);
      });
      composerSelectedSkillNames.value = next;
    }

    function normalizeComposerSkillMatchType(rawType) {
      const type = (rawType || '').toString().trim().toLowerCase();
      if (type === 'force') return 'force';
      if (type === 'explicit') return 'explicit';
      return 'trigger';
    }

    function composerSkillMatchClass(match) {
      return normalizeComposerSkillMatchType(match?.matchedBy);
    }

    function composerSkillMatchReason(match) {
      const type = normalizeComposerSkillMatchType(match?.matchedBy);
      const label = type === 'force' ? '强制词' : (type === 'explicit' ? '显式提及' : '触发词');
      const terms = Array.isArray(match?.matchedTerms)
        ? match.matchedTerms.map((term) => (term || '').toString().trim()).filter(Boolean)
        : [];
      if (terms.length === 0) return label;
      return `${label}: ${terms.join(' / ')}`;
    }

    function buildSkillPreviewSignature(matches) {
      if (!Array.isArray(matches) || matches.length === 0) return '';
      return matches
        .map((item) => {
          const name = (item?.name || '').toString().trim().toLowerCase();
          const type = (item?.matchedBy || '').toString().trim().toLowerCase();
          const terms = Array.isArray(item?.matchedTerms)
            ? item.matchedTerms.map((term) => (term || '').toString().trim().toLowerCase()).filter(Boolean).join('|')
            : '';
          return `${name}:${type}:${terms}`;
        })
        .join(';');
    }

    /**
     * @param {any} meta
     */
    function maybeWarnSkillPreviewFailure(meta) {
      const now = Date.now();
      if (now - composerSkillPreviewLastWarnAt < 2000) return;
      composerSkillPreviewLastWarnAt = now;
      logWarn('ui', 'chat.skillPreview.failed', meta);
    }

    function runQueuedComposerSkillPreviewIfNeeded() {
      if (!hasComposerSkillPreviewQueued || composerSkillPreviewLoading.value) return;
      const queued = /** @type {SkillPreviewQueuedRequest} */ (composerSkillPreviewQueued);
      hasComposerSkillPreviewQueued = false;
      if (queued.requestSeq !== composerSkillPreviewSeq) return;
      runComposerSkillPreview(queued.requestSeq, queued.threadId, queued.text).catch(() => { });
    }

    /**
     * @param {number} requestSeq
     * @param {string} threadId
     * @param {string} text
     * @returns {Promise<void>}
     */
    async function runComposerSkillPreview(requestSeq, threadId, text) {
      const startedAt = Date.now();
      composerSkillPreviewLoading.value = true;
      try {
        const raw = await callAPI('skills/match/preview', {
          threadId,
          text,
        });
        if (requestSeq !== composerSkillPreviewSeq) return;
        const matches = normalizeSkillPreviewMatches(raw?.matches);
        composerSkillMatches.value = matches;
        const signature = buildSkillPreviewSignature(matches);
        if (signature !== composerSkillPreviewLastSignature) {
          composerSkillPreviewLastSignature = signature;
          logDebug('ui', 'chat.skillPreview.done', {
            thread_id: threadId,
            text_len: text.length,
            matches: matches.length,
            duration_ms: Date.now() - startedAt,
          });
        }
      } catch (error) {
        if (requestSeq !== composerSkillPreviewSeq) return;
        composerSkillMatches.value = [];
        composerSkillPreviewLastSignature = '';
        maybeWarnSkillPreviewFailure({
          thread_id: threadId,
          text_len: text.length,
          error,
          duration_ms: Date.now() - startedAt,
        });
      } finally {
        if (requestSeq === composerSkillPreviewSeq) {
          composerSkillPreviewLoading.value = false;
        }
        runQueuedComposerSkillPreviewIfNeeded();
      }
    }

    /**
     * @param {string} threadId
     * @param {string} text
     */
    function requestComposerSkillPreview(threadId, text) {
      const requestSeq = ++composerSkillPreviewSeq;
      if (composerSkillPreviewLoading.value) {
        composerSkillPreviewQueued = { requestSeq, threadId, text };
        hasComposerSkillPreviewQueued = true;
        return;
      }
      runComposerSkillPreview(requestSeq, threadId, text).catch(() => { });
    }

    function scheduleComposerSkillPreview() {
      clearComposerSkillPreviewTimer();
      const threadId = (selectedThreadId.value || '').toString().trim();
      const text = (composer.state.text || '').toString().trim();
      if (!threadId || !text) {
        composerSkillPreviewSeq += 1;
        hasComposerSkillPreviewQueued = false;
        composerSkillPreviewLastSignature = '';
        composerSkillMatches.value = [];
        composerSkillPreviewLoading.value = false;
        return;
      }
      composerSkillPreviewTimer = window.setTimeout(() => {
        composerSkillPreviewTimer = 0;
        requestComposerSkillPreview(threadId, text);
      }, 240);
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
          interruptible: isThreadInterruptible(thread.id),
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
        focusedDiffPath.value = '';
        focusedDiffLine.value = 0;
        fallbackDiffText.value = '';
        fallbackMediaPreview.value = null;
        fallbackMarkdownPreview.value = null;
        if (!id) return;
        shouldAutoScroll.value = true;
        try {
          await requestHistoryLoad(props.threadStore, id);
        } catch {
          // ignore: real-time stream may still backfill timeline
        }
        const pendingFocus = pendingFileRefFocus.value;
        if (pendingFocus && pendingFocus.threadId === id) {
          const pendingPath = (pendingFocus.path || '').toString().trim();
          const pendingLineRaw = Number(pendingFocus.line);
          if (pendingPath) {
            focusedDiffPath.value = pendingPath;
            focusedDiffLine.value = Number.isFinite(pendingLineRaw) && pendingLineRaw > 0
              ? Math.floor(pendingLineRaw)
              : 1;
            logInfo('ui', 'chat.diff.focus.pending_applied', {
              thread_id: id,
              requested_path: (pendingFocus.requestedPath || '').toString(),
              resolved_path: pendingPath,
              line: focusedDiffLine.value,
            });
          }
          pendingFileRefFocus.value = null;
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
      [() => selectedThreadId.value, () => composer.state.text],
      () => {
        scheduleComposerSkillPreview();
      },
      { immediate: true },
    );

    watch(
      () => composerSkillMatches.value,
      (nextMatches) => {
        if (!Array.isArray(nextMatches) || nextMatches.length === 0) {
          composerSelectedSkillNames.value = [];
          return;
        }
        const allowed = new Set(nextMatches.map((match) => skillNameKey(match?.name)));
        composerSelectedSkillNames.value = composerSelectedSkillNames.value
          .filter((name) => allowed.has(skillNameKey(name)));
      },
      { deep: false },
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
      const selectedSkills = [...composerSelectedSkillNames.value];
      const manualSkillSelection = selectedSkills.length > 0;
      composer.clearComposer();
      composerSelectedSkillNames.value = [];
      shouldAutoScroll.value = true;
      await props.threadStore.sendMessage(threadId, text, attachments, {
        selectedSkills,
        manualSkillSelection,
      });
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
      if (compacting.value) return;
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

    function selectThread(threadId) {
      selectedThreadId.value = threadId;
    }

    function stopSelected() {
      const threadId = (selectedThreadId.value || '').toString().trim();
      if (!threadId) return;
      if (!isThreadInterruptible(threadId)) {
        logInfo('ui', 'chat.interrupt.skipped.notInterruptible', {
          thread_id: threadId,
          source: 'toolbar',
        });
        return;
      }
      interruptCurrent({ threadId });
    }

    function renameSelected() {
      beginInlineRename(selectedThreadId.value);
    }

    function setMainSelected() {
      props.threadStore.setMainAgent(selectedThreadId.value);
    }

    function loadCardHistory(cardId) {
      props.threadStore.loadMessages(cardId, 300);
    }

    function renameCard(cardId) {
      if (isCmd.value && typeof props.threadStore.promptRenameThread === 'function') {
        props.threadStore.promptRenameThread(cardId);
        return;
      }
      beginInlineRename(cardId);
    }

    function stopCard(cardId) {
      const threadId = (cardId || '').toString().trim();
      if (!threadId) return;
      if (!isThreadInterruptible(threadId)) {
        logInfo('ui', 'chat.interrupt.skipped.notInterruptible', {
          thread_id: threadId,
          source: 'card',
        });
        return;
      }
      interruptCurrent({ threadId });
    }

    function toggleThreadPin(threadId) {
      if (typeof props.threadStore.toggleThreadPin !== 'function') return;
      props.threadStore.toggleThreadPin(threadId);
    }

    async function toggleThreadArchive(threadId) {
      if (typeof props.threadStore.toggleThreadArchive !== 'function') return;
      try {
        await props.threadStore.toggleThreadArchive(threadId);
      } catch (error) {
        logWarn('ui', 'thread.archive.toggle.failed', {
          thread_id: (threadId || '').toString(),
          error,
        });
      }
    }

    function toggleArchivedThreadList() {
      showArchivedThreadList.value = !showArchivedThreadList.value;
    }

    function setRenameInputRef(threadId, el) {
      const id = (threadId || '').toString().trim();
      if (!id) return;
      if (!el) {
        renameInputRefByThread.delete(id);
        return;
      }
      renameInputRefByThread.set(id, el);
    }

    function beginInlineRename(threadId) {
      const id = (threadId || '').toString().trim();
      if (!id) return;
      const target = chatThreadCards.value.find((item) => item.id === id);
      const current = (target?.name || id).toString().trim() || id;
      editingThreadId.value = id;
      editingAlias.value = current;
      renamingThreadId.value = '';
      selectThread(id);
      nextTick(() => {
        const input = renameInputRefByThread.get(id);
        if (!input) return;
        input.focus();
        input.select();
      });
    }

    function cancelInlineRename(threadId = '') {
      const id = (threadId || editingThreadId.value || '').toString().trim();
      if (!id || editingThreadId.value !== id) return;
      editingThreadId.value = '';
      editingAlias.value = '';
      renamingThreadId.value = '';
    }

    async function submitInlineRename(threadId) {
      const id = (threadId || editingThreadId.value || '').toString().trim();
      if (!id || editingThreadId.value !== id || renamingThreadId.value === id) return;

      const target = chatThreadCards.value.find((item) => item.id === id);
      const current = (target?.name || id).toString().trim() || id;
      const nextName = (editingAlias.value || '').toString().trim();
      if (!nextName || nextName === current) {
        cancelInlineRename(id);
        return;
      }

      renamingThreadId.value = id;
      try {
        if (typeof props.threadStore.renameThread === 'function') {
          await props.threadStore.renameThread(id, nextName);
        } else if (typeof props.threadStore.promptRenameThread === 'function') {
          props.threadStore.promptRenameThread(id);
        }
        cancelInlineRename(id);
      } catch (error) {
        logWarn('ui', 'thread.rename.inline.failed', {
          thread_id: id,
          error,
        });
        renamingThreadId.value = '';
        nextTick(() => {
          const input = renameInputRefByThread.get(id);
          if (!input) return;
          input.focus();
          input.select();
        });
      }
    }

    function handleInlineRenameBlur(threadId) {
      const id = (threadId || '').toString().trim();
      if (!id || editingThreadId.value !== id) return;
      submitInlineRename(id);
    }

    function getDisplayName(thread) {
      return props.threadStore.displayName(thread);
    }

    function setCmdLayout(value) {
      layoutMode.value = value;
    }

    function setCmdCardCols(value) {
      cmdCardCols.value = value;
    }

    async function onTimelineFileRefClick(payload) {
      const threadId = (selectedThreadId.value || '').toString().trim();
      if (!threadId) {
        logWarn('ui', 'chat.fileRef.handle.no_thread', {
          payload,
        });
        return;
      }
      const rawPath = (payload?.path || '').toString().trim();
      const lineRaw = Number(payload?.line);
      const line = Number.isFinite(lineRaw) && lineRaw > 0 ? Math.floor(lineRaw) : 1;
      const columnRaw = Number(payload?.column);
      const column = Number.isFinite(columnRaw) && columnRaw > 0 ? Math.floor(columnRaw) : 0;
      const diffText = (activeThreadDiffText.value || '').toString();
      const diffFiles = parseUnifiedDiff(diffText);
      logInfo('ui', 'chat.fileRef.handle.received', {
        thread_id: threadId,
        path: rawPath,
        line,
        column,
        diff_len: diffText.length,
        diff_files: diffFiles.length,
        payload,
      });
      if (!rawPath) {
        logWarn('ui', 'chat.fileRef.handle.no_path', {
          thread_id: threadId,
          line,
          payload,
        });
        return;
      }

      const preferMarkdownPreview = isMarkdownPath(rawPath);
      const selection = preferMarkdownPreview
        ? null
        : buildFocusedDiffSelection(diffText, rawPath);
      if (!selection) {
        const crossThreadSelection = preferMarkdownPreview
          ? null
          : findCrossThreadDiffSelection(
            props.threadStore?.state?.diffTextByThread,
            rawPath,
            threadId,
          );
        if (crossThreadSelection?.path) {
          const targetThreadId = (crossThreadSelection.threadId || '').toString().trim();
          if (targetThreadId && targetThreadId !== threadId) {
            fallbackDiffText.value = '';
            fallbackMediaPreview.value = null;
            fallbackMarkdownPreview.value = null;
            pendingFileRefFocus.value = {
              threadId: targetThreadId,
              path: crossThreadSelection.path,
              line,
              requestedPath: rawPath,
            };
            selectedThreadId.value = targetThreadId;
            logInfo('ui', 'chat.diff.focus.cross_thread_switch', {
              from_thread_id: threadId,
              to_thread_id: targetThreadId,
              requested_path: rawPath,
              resolved_path: crossThreadSelection.path,
              line,
            });
            return;
          }
          fallbackDiffText.value = '';
          fallbackMediaPreview.value = null;
          fallbackMarkdownPreview.value = null;
          focusedDiffPath.value = crossThreadSelection.path;
          focusedDiffLine.value = line;
          logInfo('ui', 'chat.diff.focus.recovered', {
            thread_id: threadId,
            requested_path: rawPath,
            resolved_path: crossThreadSelection.path,
            line,
          });
          return;
        }
        const activeProject = ((props.projectStore?.state?.active || '.').toString().trim()) || '.';
        const projectList = Array.isArray(props.projectStore?.state?.projects)
          ? props.projectStore.state.projects
            .map((item) => (item || '').toString().trim())
            .filter(Boolean)
          : [];
        const codeOpenCandidates = [rawPath];
        if (!/[\\/]/.test(rawPath) && /\.log$/i.test(rawPath)) {
          codeOpenCandidates.push(`logs/${rawPath}`);
        }
        /** @type {CodeOpenResult | null} */
        let codeOpenResult = null;
        let codeOpenInputPath = '';
        /** @type {any} */
        let codeOpenError = null;
        for (const candidatePath of codeOpenCandidates) {
          try {
            const result = /** @type {CodeOpenResult | null} */ (await callAPI('ui/code/open', {
              filePath: candidatePath,
              line,
              column,
              project: activeProject,
              projects: projectList,
            }));
            if (result?.ok) {
              codeOpenResult = result;
              codeOpenInputPath = candidatePath;
              break;
            }
          } catch (error) {
            codeOpenError = error;
          }
        }
        if (codeOpenResult?.ok) {
          const imagePreview = buildImagePreviewFromCodeOpen(codeOpenResult);
          if (imagePreview) {
            fallbackDiffText.value = '';
            fallbackMediaPreview.value = imagePreview;
            fallbackMarkdownPreview.value = null;
            focusedDiffPath.value = imagePreview.path || rawPath;
            focusedDiffLine.value = 0;
            logInfo('ui', 'chat.diff.focus.image_preview_applied', {
              thread_id: threadId,
              requested_path: rawPath,
              open_input_path: codeOpenInputPath,
              resolved_path: imagePreview.path || rawPath,
              media_type: imagePreview.mediaType,
              size_bytes: imagePreview.sizeBytes,
            });
            return;
          }

          const markdownPreview = buildMarkdownPreviewFromCodeOpen(codeOpenResult);
          if (markdownPreview) {
            fallbackDiffText.value = '';
            fallbackMediaPreview.value = null;
            fallbackMarkdownPreview.value = markdownPreview;
            focusedDiffPath.value = markdownPreview.path || rawPath;
            focusedDiffLine.value = line;
            logInfo('ui', 'chat.diff.focus.markdown_preview_applied', {
              thread_id: threadId,
              requested_path: rawPath,
              open_input_path: codeOpenInputPath,
              resolved_path: markdownPreview.path || rawPath,
              start_line: markdownPreview.startLine,
              end_line: markdownPreview.endLine,
              total_lines: markdownPreview.totalLines,
            });
            return;
          }

          const syntheticDiff = buildSyntheticDiffFromCodeOpen(codeOpenResult);
          const resolvedPath = (codeOpenResult?.relative || codeOpenResult?.filePath || codeOpenInputPath || rawPath).toString().trim();
          if (syntheticDiff && resolvedPath) {
            fallbackDiffText.value = syntheticDiff;
            fallbackMediaPreview.value = null;
            fallbackMarkdownPreview.value = null;
            focusedDiffPath.value = resolvedPath;
            focusedDiffLine.value = line;
            logInfo('ui', 'chat.diff.focus.code_open_applied', {
              thread_id: threadId,
              requested_path: rawPath,
              open_input_path: codeOpenInputPath,
              resolved_path: resolvedPath,
              line,
              column,
              snippet_start: Number(codeOpenResult?.startLine) || 0,
              snippet_end: Number(codeOpenResult?.endLine) || 0,
              snippet_len: Array.isArray(codeOpenResult?.snippet)
                ? codeOpenResult.snippet.length
                : (codeOpenResult?.snippet || '').toString().length,
            });
            return;
          }
          logWarn('ui', 'chat.diff.focus.code_open.empty', {
            thread_id: threadId,
            requested_path: rawPath,
            open_input_path: codeOpenInputPath,
            line,
            column,
            code_open_ok: true,
            snippet_len: Array.isArray(codeOpenResult?.snippet)
              ? codeOpenResult.snippet.length
              : (codeOpenResult?.snippet || '').toString().length,
          });
        } else if (codeOpenError) {
          logWarn('ui', 'chat.diff.focus.code_open.failed', {
            thread_id: threadId,
            requested_path: rawPath,
            line,
            column,
            tried_paths: codeOpenCandidates,
            error: codeOpenError,
          });
        }
        logWarn('ui', 'chat.diff.focus.miss', {
          thread_id: threadId,
          requested_path: rawPath,
          line,
          diff_len: diffText.length,
          diff_files: diffFiles.length,
        });
        // 回退：即使没有精确命中，也尝试以原始路径触发右侧 diff 聚焦。
        fallbackDiffText.value = '';
        fallbackMediaPreview.value = null;
        fallbackMarkdownPreview.value = null;
        focusedDiffPath.value = rawPath;
        focusedDiffLine.value = line;
        logInfo('ui', 'chat.diff.focus.fallback_applied', {
          thread_id: threadId,
          path: rawPath,
          line,
        });
        return;
      }

      fallbackDiffText.value = '';
      fallbackMediaPreview.value = null;
      fallbackMarkdownPreview.value = null;
      focusedDiffPath.value = selection.filename;
      focusedDiffLine.value = line;
      logInfo('ui', 'chat.diff.focus.applied', {
        thread_id: threadId,
        requested_path: rawPath,
        resolved_path: selection.filename,
        line,
        diff_len: diffText.length,
        diff_files: diffFiles.length,
      });
    }

    async function copySelectedThreadId() {
      const threadId = (selectedThreadId.value || '').toString();
      if (!threadId) return;
      const runtime = /** @type {ThreadIdentityInfo} */ ((activeRuntime.value && typeof activeRuntime.value === 'object')
        ? activeRuntime.value
        : { codexThreadId: '', port: null });
      /** @type {ThreadIdentityInfo} */
      let resolved = { codexThreadId: '', port: null };
      const existingCodexThreadID = (runtime.codexThreadId || '').toString().trim();
      if (!existingCodexThreadID) {
        try {
          resolved = /** @type {ThreadIdentityInfo} */ (await resolveThreadIdentity(threadId));
        } catch {
          resolved = { codexThreadId: '', port: null };
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
        return `${formatTokenPercent(usedPercent)} used · ${formatTokenCompact(used)} / ${formatTokenCompact(limit)}`;
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
      event.preventDefault();
      event.stopPropagation();
      document.body.classList.add('is-col-resizing');
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
        document.body.classList.remove('is-col-resizing');
        window.removeEventListener('mousemove', onMove);
        window.removeEventListener('mouseup', onUp);
        window.removeEventListener('blur', onUp);
      };

      window.addEventListener('mousemove', onMove);
      window.addEventListener('mouseup', onUp);
      window.addEventListener('blur', onUp);
    }

    function onActivityResizeStart(event) {
      if (isCmd.value) return;
      if (event.button !== 0) return;
      event.preventDefault();
      event.stopPropagation();
      document.body.classList.add('is-row-resizing');
      activityPanelDragging.value = true;
      clearActivityPanelResizeListeners();

      const startY = event.clientY;
      const startHeight = clampActivityPanelHeight(activityPanelHeight.value);
      const viewportMaxHeight = Math.max(
        ACTIVITY_PANEL_MIN_HEIGHT,
        Math.floor(window.innerHeight * 0.72),
      );
      const maxHeight = Math.max(ACTIVITY_PANEL_MAX_HEIGHT, viewportMaxHeight);

      const onMove = (e) => {
        const nextHeight = startHeight + (startY - e.clientY);
        activityPanelHeight.value = clampActivityPanelHeight(nextHeight, maxHeight);
      };

      const onUp = () => {
        activityPanelDragging.value = false;
        document.body.classList.remove('is-row-resizing');
        clearActivityPanelResizeListeners();
      };

      clearActivityPanelResizeListeners = () => {
        window.removeEventListener('mousemove', onMove);
        window.removeEventListener('mouseup', onUp);
        window.removeEventListener('blur', onUp);
      };

      window.addEventListener('mousemove', onMove);
      window.addEventListener('mouseup', onUp);
      window.addEventListener('blur', onUp);
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

    function onNativeFilesDropped(evt) {
      const payload = evt && typeof evt === 'object' ? evt : {};
      const files = Array.isArray(payload.files) ? payload.files : [];
      if (files.length === 0) return;

      const details = payload.details && typeof payload.details === 'object'
        ? payload.details
        : {};
      const targetID = (details.id || '').toString().trim();
      if (targetID && targetID !== 'chat-input-bar') return;

      const added = composer.attachByPaths(files, 'wails-drop');
      logInfo('ui', 'chat.nativeFilesDropped.handled', {
        files: files.length,
        added,
        target_id: targetID,
      });
    }

    onMounted(() => {
      window.addEventListener('keydown', onGlobalEscape, true);
      document.addEventListener('keydown', onGlobalEscape, true);
      offFilesDropped = onFilesDropped(onNativeFilesDropped);
    });

    onBeforeUnmount(() => {
      window.removeEventListener('keydown', onGlobalEscape, true);
      document.removeEventListener('keydown', onGlobalEscape, true);
      offFilesDropped();
      offFilesDropped = () => { };
      dragging.value = false;
      activityPanelDragging.value = false;
      document.body.classList.remove('is-col-resizing');
      document.body.classList.remove('is-row-resizing');
      clearActivityPanelResizeListeners();
      clearActivityPanelResizeListeners = () => { };
      clearComposerSkillPreviewTimer();
      composerSkillPreviewSeq += 1;
      hasComposerSkillPreviewQueued = false;
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
      chatThreadCards,
      showArchivedThreadList,
      chatActiveThreadCards,
      chatArchivedThreadCards,
      visibleChatThreadCards,
      activeChatThreadCount,
      archivedChatThreadCount,
      activeTimeline,
      activeDiffText,
      activeMediaPreview,
      activeMarkdownPreview,
      activeDiffFocusFile,
      activeDiffFocusLine,
      activeStatus,
      activeStatusHeader,
      activeStatusDetails,
      activeStatusMeta,
      activeTokenInline,
      activeTokenTooltip,
      compacting,
      canInterrupt,
      displayStatusText,
      noActiveThread,
      copyButtonLabel,
      layoutMode,
      cmdCardCols,
      splitRatio,
      showOverview,
      showWorkspace,
      chatComposerShellStyle,
      activityPanelRowStyle,
      activePinnedPlan,
      stats,
      recentThreads,
      cmdCards,
      composerSkillMatches,
      composerSelectedSkillNames,
      composerSkillPreviewLoading,
      isComposerSkillSelected,
      toggleComposerSelectedSkill,
      clearComposerSelectedSkills,
      selectAllComposerSuggestedSkills,
      composerSkillMatchClass,
      composerSkillMatchReason,
      dragging,
      activityPanelDragging,
      composerBarRef,
      workspaceRef,
      activeActivityStats,
      activeAlerts,
      activeProcessActivity,
      selectThread,
      launchOne,
      send,
      interruptCurrent,
      compactCurrent,
      forceCompleteCurrent,
      setCmdLayout,
      setCmdCardCols,
      copySelectedThreadId,
      timelinePreview,
      diffPreview,
      onResizeStart,
      onActivityResizeStart,
      stopSelected,
      renameSelected,
      setMainSelected,
      loadCardHistory,
      renameCard,
      stopCard,
      toggleThreadPin,
      toggleThreadArchive,
      toggleArchivedThreadList,
      editingThreadId,
      editingAlias,
      renamingThreadId,
      setRenameInputRef,
      beginInlineRename,
      submitInlineRename,
      cancelInlineRename,
      handleInlineRenameBlur,
      getDisplayName,
      dismissPinnedPlan,
      onTimelineFileRefClick,
    };
  },
  template: `
    <section class="page active unified-chat-page" :class="isCmd ? 'mode-cmd' : 'mode-chat'">
      <div class="chat-toolbar unified-toolbar" style="position:relative">
        <div
          v-if="activeStatus === 'thinking' || activeStatus === 'responding' || activeStatus === 'running'"
          class="chat-running-card"
          role="status"
          aria-live="polite"
        >
          <svg class="chat-running-spinner" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle class="chat-running-spinner-track" cx="12" cy="12" r="8.5"></circle>
            <circle class="chat-running-spinner-arc" cx="12" cy="12" r="8.5"></circle>
          </svg>
          <div class="chat-running-copy">
            <strong>{{ displayStatusText || '执行中' }}</strong>
            <span v-if="activeStatusMeta">{{ activeStatusMeta }}</span>
          </div>
        </div>
        <ProjectSelect
          :model-value="projectStore.state.active"
          :options="projectStore.projectOptions.value"
          @update:model-value="projectStore.setActive($event)"
          @add-project="projectStore.quickAdd()"
        />

        <div class="layout-switch" v-if="isCmd">
          <button class="btn btn-ghost btn-xs" :class="{active: layoutMode==='overview'}" @click="setCmdLayout('overview')">A 紧凑</button>
          <button class="btn btn-ghost btn-xs" :class="{active: layoutMode==='chat'}" @click="setCmdLayout('chat')">B 对话</button>
          <button class="btn btn-ghost btn-xs" :class="{active: layoutMode==='mix'}" @click="setCmdLayout('mix')">C 混合</button>
        </div>

        <div class="layout-switch" v-if="isCmd">
          <button class="btn btn-ghost btn-xs" :class="{active: cmdCardCols===2}" @click="setCmdCardCols(2)">2列</button>
          <button class="btn btn-ghost btn-xs" :class="{active: cmdCardCols===3}" @click="setCmdCardCols(3)">3列</button>
        </div>

        <button
          class="btn btn-secondary btn-toolbar-sm launch-agent-icon-btn"
          aria-label="启动 Agent"
          title="启动 Agent"
          @click="launchOne"
        >
          <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
            <path d="M2 10l2.3-.5L10 3.8a1.3 1.3 0 10-1.8-1.8L2.5 7.7 2 10z"></path>
            <path d="M7.6 2.6l1.8 1.8"></path>
          </svg>
        </button>
        <button
          v-if="!isCmd && selectedThreadId"
          class="btn btn-ghost btn-xs chat-toolbar-icon-btn"
          :aria-label="copyButtonLabel"
          :title="copyButtonLabel"
          @click="copySelectedThreadId"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <rect x="9" y="7" width="10" height="13" rx="2.2"></rect>
            <path d="M15 7V5.8A1.8 1.8 0 0 0 13.2 4H6.8A1.8 1.8 0 0 0 5 5.8V16.2A1.8 1.8 0 0 0 6.8 18H9"></path>
            <path d="M12 11.5h4"></path>
            <path d="M12 15h4"></path>
          </svg>
        </button>
        <button
          v-if="!isCmd && selectedThreadId"
          class="btn btn-ghost btn-xs chat-toolbar-icon-btn"
          :aria-label="selectedThreadId === mainAgentId ? '主Agent' : '设为主Agent'"
          :title="selectedThreadId === mainAgentId ? '主Agent' : '设为主Agent'"
          @click="setMainSelected"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="8"></circle>
            <path d="M12 8.2l1.3 2.6 2.9.4-2.1 2 .5 2.8-2.6-1.4-2.6 1.4.5-2.8-2.1-2 2.9-.4L12 8.2z"></path>
            <path v-if="selectedThreadId !== mainAgentId" d="M18.2 5.4v3.2"></path>
            <path v-if="selectedThreadId !== mainAgentId" d="M16.6 7h3.2"></path>
          </svg>
        </button>
        <button
          v-if="!isCmd && selectedThreadId"
          class="btn btn-ghost btn-xs chat-toolbar-icon-btn"
          :disabled="!canInterrupt"
          :aria-label="canInterrupt ? '停止' : '当前没有可中断任务'"
          :title="canInterrupt ? '中断当前执行' : '当前没有可中断任务'"
          @click="stopSelected"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="8"></circle>
            <rect x="9" y="9" width="6" height="6" rx="1.2"></rect>
          </svg>
        </button>
        <button
          v-if="!isCmd && selectedThreadId && activeStatus === 'running'"
          class="btn btn-ghost btn-xs btn-warning chat-toolbar-icon-btn"
          @click="forceCompleteCurrent"
          aria-label="重链"
          title="强制完成当前 turn，重置状态机"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M9.4 14.6 7 17a3.2 3.2 0 1 1-4.5-4.5l2.4-2.4"></path>
            <path d="M14.6 9.4 17 7a3.2 3.2 0 1 1 4.5 4.5l-2.4 2.4"></path>
            <path d="M9 15h6"></path>
            <path d="M18.5 15.5a6.5 6.5 0 0 1-9.2 2.1"></path>
            <path d="M18.5 18.5v-3"></path>
            <path d="M18.5 18.5h-3"></path>
          </svg>
        </button>
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
        <aside v-if="!isCmd" class="thread-rail" :aria-label="showArchivedThreadList ? '归档会话列表' : '会话列表'">
          <header class="thread-rail-header">
            <div class="thread-rail-header-main">
              <span
                class="thread-rail-kind-icon"
                role="img"
                :aria-label="showArchivedThreadList ? '归档列表' : '会话列表'"
                :title="showArchivedThreadList ? '归档列表' : '会话列表'"
              >
                <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M10 3V5"></path>
                  <path d="M6.2 5H13.8C15 5 16 6 16 7.2V12.8C16 14 15 15 13.8 15H6.2C5 15 4 14 4 12.8V7.2C4 6 5 5 6.2 5Z"></path>
                  <path d="M2.8 8V12"></path>
                  <path d="M17.2 8V12"></path>
                  <circle cx="8" cy="10" r="0.9" fill="currentColor" stroke="none"></circle>
                  <circle cx="12" cy="10" r="0.9" fill="currentColor" stroke="none"></circle>
                </svg>
              </span>
              <span
                class="thread-rail-count-chip"
                role="img"
                :aria-label="showArchivedThreadList ? (archivedChatThreadCount + ' 个 Agent') : (activeChatThreadCount + ' 个 Agent')"
                :title="showArchivedThreadList ? (archivedChatThreadCount + ' 个 Agent') : (activeChatThreadCount + ' 个 Agent')"
              >
                <svg class="thread-rail-count-icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M10 3V5"></path>
                  <path d="M6.2 5H13.8C15 5 16 6 16 7.2V12.8C16 14 15 15 13.8 15H6.2C5 15 4 14 4 12.8V7.2C4 6 5 5 6.2 5Z"></path>
                  <path d="M2.8 8V12"></path>
                  <path d="M17.2 8V12"></path>
                  <circle cx="8" cy="10" r="0.9" fill="currentColor" stroke="none"></circle>
                  <circle cx="12" cy="10" r="0.9" fill="currentColor" stroke="none"></circle>
                </svg>
                <strong>{{ showArchivedThreadList ? archivedChatThreadCount : activeChatThreadCount }}</strong>
              </span>
            </div>
            <button
              type="button"
              class="btn btn-ghost btn-xs thread-rail-switch-btn"
              :class="{ active: showArchivedThreadList }"
              :aria-label="showArchivedThreadList ? '返回会话列表' : '打开归档列表'"
              :title="showArchivedThreadList ? '返回会话列表' : '打开归档列表'"
              @click="toggleArchivedThreadList"
            >
              <svg v-if="showArchivedThreadList" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path d="M6 4L10 8L6 12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" transform="rotate(180 8 8)"></path>
              </svg>
              <svg v-else viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                <path d="M2.2 3.3h11.6a.9.9 0 0 1 .9.9v1.7a.9.9 0 0 1-.9.9H2.2a.9.9 0 0 1-.9-.9V4.2a.9.9 0 0 1 .9-.9Z"></path>
                <path d="M3.4 6.8h9.2V12a1 1 0 0 1-1 1h-7.2a1 1 0 0 1-1-1V6.8Z"></path>
                <path d="M6.1 9.3h3.8" stroke-linecap="round"></path>
              </svg>
            </button>
          </header>
          <div v-if="visibleChatThreadCards.length === 0" class="thread-rail-empty">
            {{ showArchivedThreadList ? '暂无归档会话' : '暂无会话，点击顶部「启动 Agent」开始对话' }}
          </div>
          <div v-else class="thread-rail-list hide-scrollbar">
            <button
              v-for="thread in visibleChatThreadCards"
              :key="thread.id"
              class="thread-rail-item"
              :class="{ active: thread.selected, archived: thread.isArchived }"
              @click="selectThread(thread.id)"
              :title="thread.name"
            >
              <div class="thread-rail-item-head">
                <button
                  type="button"
                  class="thread-rail-pin-btn"
                  :class="{ active: thread.isPinned }"
                  :aria-label="thread.isPinned ? '取消置顶会话' : '置顶会话'"
                  :title="thread.isPinned ? '取消置顶' : '置顶'"
                  @click.stop="toggleThreadPin(thread.id)"
                >
                  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                    <path d="M9.5 2.5L13.5 6.5L10 10L8 14L2 8L6 6L9.5 2.5Z" stroke-linejoin="round"></path>
                    <path d="M6 10L2.5 13.5" stroke-linecap="round"></path>
                  </svg>
                </button>
                <input
                  v-if="editingThreadId === thread.id"
                  :ref="(el) => setRenameInputRef(thread.id, el)"
                  v-model="editingAlias"
                  class="thread-rail-alias-input"
                  type="text"
                  maxlength="64"
                  :disabled="renamingThreadId === thread.id"
                  @click.stop
                  @keydown.enter.prevent="submitInlineRename(thread.id)"
                  @keydown.esc.prevent="cancelInlineRename(thread.id)"
                  @blur="handleInlineRenameBlur(thread.id)"
                >
                <strong
                  v-else
                  class="thread-rail-name"
                  @click.stop="beginInlineRename(thread.id)"
                >{{ thread.name }}</strong>
                <button
                  type="button"
                  class="thread-rail-archive-btn"
                  :class="{ active: thread.isArchived }"
                  :aria-label="thread.isArchived ? '恢复会话' : '归档会话'"
                  :title="thread.isArchived ? '恢复' : '归档'"
                  @click.stop="toggleThreadArchive(thread.id)"
                >
                  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                    <path d="M2.2 3.3h11.6a.9.9 0 0 1 .9.9v1.7a.9.9 0 0 1-.9.9H2.2a.9.9 0 0 1-.9-.9V4.2a.9.9 0 0 1 .9-.9Z"></path>
                    <path d="M3.4 6.8h9.2V12a1 1 0 0 1-1 1h-7.2a1 1 0 0 1-1-1V6.8Z"></path>
                    <path d="M6.1 9.3h3.8" stroke-linecap="round"></path>
                  </svg>
                </button>
                <span v-if="thread.isMain" class="thread-main-badge">主</span>
              </div>
              <div v-if="thread.showId" class="thread-rail-item-id">{{ thread.id }}</div>
              <div class="thread-rail-item-meta">
                <span class="status-dot" :class="thread.status"></span>
                <span>{{ thread.statusHeader }}</span>
              </div>
            </button>
          </div>
        </aside>
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
                  <button
                    class="btn btn-ghost btn-xs"
                    :disabled="!card.interruptible"
                    :title="card.interruptible ? '中断该 Agent 当前执行' : '当前没有可中断任务'"
                    @click.stop="stopCard(card.id)"
                  >停止</button>
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
                <aside
                  v-if="activePinnedPlan"
                  class="chat-plan-pin"
                  :class="{ done: activePinnedPlan.done }"
                  :title="activePinnedPlan.statusText"
                >
                  <header class="chat-plan-pin-head">
                    <span class="chat-plan-pin-role">计划</span>
                    <span class="chat-plan-pin-status">{{ activePinnedPlan.statusText }}</span>
                    <button
                      type="button"
                      class="chat-plan-pin-close"
                      aria-label="关闭计划标签"
                      @click="dismissPinnedPlan"
                    >×</button>
                  </header>
                  <pre class="chat-plan-pin-body">{{ activePinnedPlan.text }}</pre>
                </aside>
                <div v-if="noActiveThread" class="chat-messages-vue">
                  <div class="diff-empty">选择或启动一个 Agent 开始对话</div>
                </div>
                <ChatTimeline
                  v-else
                  :items="activeTimeline"
                  :active-status="activeStatus"
                  :active-status-text="displayStatusText"
                  :active-status-meta="activeStatusMeta"
                  :pinned-plan-visible="Boolean(activePinnedPlan)"
                  @file-ref-click="onTimelineFileRefClick"
                />
              </div>

              <div class="panel-resizer" :class="{dragging}" @mousedown="onResizeStart"></div>

              <div class="workspace-right-col" :style="{ flex: '0 0 ' + (100 - splitRatio) + '%' }">
                <DiffPanel
                  :diff-text="activeDiffText"
                  :media-preview="activeMediaPreview"
                  :markdown-preview="activeMarkdownPreview"
                  :focus-file="activeDiffFocusFile"
                  :focus-line="activeDiffFocusLine"
                />
              </div>
            </div>

            <div class="workspace-bottom-row" :class="{ 'is-cmd': isCmd }" :style="activityPanelRowStyle">
              <div class="chat-composer-shell" :class="{ 'for-chat': !isCmd }" :style="chatComposerShellStyle">
                <ComposerBar
                  ref="composerBarRef"
                  :composer="composer"
                  :thread-id="selectedThreadId"
                  :interruptible="canInterrupt"
                  :compacting="compacting"
                  :token-inline="activeTokenInline"
                  :token-tooltip="activeTokenTooltip"
                  :disabled="noActiveThread"
                  :skill-matches="composerSkillMatches"
                  :skill-matches-loading="composerSkillPreviewLoading"
                  :selected-skill-names="composerSelectedSkillNames"
                  @toggle-skill="toggleComposerSelectedSkill"
                  @select-all-skills="selectAllComposerSuggestedSkills"
                  @clear-skills="clearComposerSelectedSkills"
                  @send="send"
                  @interrupt="interruptCurrent"
                  @compact="compactCurrent"
                />
              </div>
              <div v-if="!isCmd" class="workspace-bottom-side">
                <div class="workspace-bottom-side-layer" :class="{ dragging: activityPanelDragging }">
                  <div class="activity-panel-resizer" :class="{ dragging: activityPanelDragging }" @mousedown="onActivityResizeStart"></div>
                  <ActivityPanel
                    :stats="activeActivityStats"
                    :alerts="activeAlerts"
                    :process-events="activeProcessActivity"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  `,
};
