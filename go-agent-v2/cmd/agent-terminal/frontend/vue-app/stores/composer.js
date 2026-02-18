import { reactive, computed } from '../../lib/vue.esm-browser.prod.js';
import { saveClipboardImage, selectFiles } from '../services/api.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';

const state = reactive({
  text: '',
  attachments: [],
  attaching: false,
});

function clearComposer() {
  const attachmentCount = state.attachments.length;
  state.text = '';
  state.attachments = [];
  logDebug('composer', 'cleared', { attachment_count: attachmentCount });
}

function removeAttachment(index) {
  const target = state.attachments[index];
  state.attachments.splice(index, 1);
  logDebug('composer', 'attachment.removed', {
    index,
    name: target?.name || '',
    count: state.attachments.length,
  });
}

function pushAttachment(attachment) {
  if (!attachment?.path) return;
  if (state.attachments.some((item) => item.path === attachment.path)) return;
  state.attachments.push(attachment);
  logInfo('composer', 'attachment.added', {
    kind: attachment.kind,
    name: attachment.name,
    count: state.attachments.length,
  });
}

function normalizeFileAttachment(path) {
  const value = (path || '').trim();
  if (!value) return null;
  const parts = value.split('/');
  const name = parts[parts.length - 1] || value;
  const lower = name.toLowerCase();
  const image = /\.(png|jpg|jpeg|gif|webp|bmp|svg)$/.test(lower);
  return {
    kind: image ? 'image' : 'file',
    name,
    path: value,
    previewUrl: image ? `file://${value}` : '',
  };
}

async function attachByPicker() {
  // UI intent only: actual file chooser is provided by Wails bridge (Go).
  state.attaching = true;
  const start = Date.now();
  logInfo('composer', 'picker.start', {});
  try {
    const paths = await selectFiles();
    paths.forEach((path) => {
      const attachment = normalizeFileAttachment(path);
      if (attachment) pushAttachment(attachment);
    });
    logInfo('composer', 'picker.done', {
      selected: paths.length,
      duration_ms: Date.now() - start,
    });
  } catch (error) {
    logWarn('composer', 'picker.failed', {
      error,
      duration_ms: Date.now() - start,
    });
  } finally {
    state.attaching = false;
  }
}

async function handlePaste(event) {
  const items = event?.clipboardData?.items;
  if (!items || items.length === 0) return false;

  for (const item of items) {
    if (!item.type.startsWith('image/')) continue;
    event.preventDefault();

    const blob = item.getAsFile();
    if (!blob) continue;
    try {
      const dataUrl = await blobToDataURL(blob);
      const base64 = dataUrl.split(',')[1] || '';
      const tempPath = await saveClipboardImage(base64);

      pushAttachment({
        kind: 'image',
        name: `screenshot-${Date.now()}.png`,
        path: tempPath || '',
        previewUrl: dataUrl,
      });
      logInfo('composer', 'paste.image.added', { has_path: Boolean(tempPath) });
      return true;
    } catch (error) {
      logWarn('composer', 'paste.image.failed', { error });
      return false;
    }
  }

  logDebug('composer', 'paste.ignored', {});
  return false;
}

async function blobToDataURL(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result || '');
    reader.onerror = () => reject(reader.error || new Error('read blob failed'));
    reader.readAsDataURL(blob);
  });
}

function buildTurnInput() {
  const input = [];
  const text = (state.text || '').trim();
  if (text) {
    input.push({ type: 'text', text });
  }

  for (const item of state.attachments) {
    if (!item.path) continue;
    if (item.kind === 'image') {
      input.push({ type: 'localImage', path: item.path });
    } else {
      input.push({ type: 'fileContent', path: item.path });
    }
  }

  return input;
}

export function useComposerStore() {
  return {
    state,
    canSend: computed(() => {
      const text = (state.text || '').trim();
      return Boolean(text) || state.attachments.length > 0;
    }),
    clearComposer,
    removeAttachment,
    attachByPicker,
    handlePaste,
    buildTurnInput,
  };
}
