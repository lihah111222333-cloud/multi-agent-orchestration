import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage forwards global Esc to ComposerBar interrupt when focus is non-input', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('const composerBarRef = ref(null);'), true);
  assert.equal(src.includes('function onGlobalEscape(event)'), true);
  assert.equal(src.includes("window.addEventListener('keydown', onGlobalEscape);"), true);
  assert.equal(src.includes("window.removeEventListener('keydown', onGlobalEscape);"), true);
  assert.equal(src.includes('if (isEditableElement(event?.target) || isEditableElement(activeEl)) return;'), true);
  assert.equal(src.includes('composerBarRef.value?.onEscape?.(event);'), true);
  assert.equal(src.includes('ref="composerBarRef"'), true);
});
