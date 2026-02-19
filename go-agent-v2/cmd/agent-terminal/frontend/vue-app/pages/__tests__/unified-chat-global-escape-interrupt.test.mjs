import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage routes global Esc to page-level stop action when focus is non-input', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('const composerBarRef = ref(null);'), true);
  assert.equal(src.includes('function isEscapeKeyEvent(event)'), true);
  assert.equal(src.includes("if (key === 'Escape' || key === 'Esc') return true;"), true);
  assert.equal(src.includes("if (code === 'Escape') return true;"), true);
  assert.equal(src.includes('return keyCode === 27;'), true);
  assert.equal(src.includes('function onGlobalEscape(event)'), true);
  assert.equal(src.includes("window.addEventListener('keydown', onGlobalEscape, true);"), true);
  assert.equal(src.includes("document.addEventListener('keydown', onGlobalEscape, true);"), true);
  assert.equal(src.includes("window.removeEventListener('keydown', onGlobalEscape, true);"), true);
  assert.equal(src.includes("document.removeEventListener('keydown', onGlobalEscape, true);"), true);
  assert.equal(src.includes('const inComposerTextarea = isComposerTextarea(event?.target) || isComposerTextarea(activeEl);'), true);
  assert.equal(src.includes('if (!inComposerTextarea && (isEditableElement(event?.target) || isEditableElement(activeEl))) return;'), true);
  assert.equal(src.includes('if (event && event.__aoGlobalEscapeHandled) return;'), true);
  assert.equal(src.includes('stopSelected();'), true);
  assert.equal(src.includes('ref="composerBarRef"'), true);
});
