import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage pauses status timer when modal is open', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(
    src.includes('const isStatusTimerModalPaused = computed(() => Boolean(props.projectStore?.state?.showModal));'),
    true,
  );
  assert.equal(src.includes('isStatusTimerModalPaused.value'), true);
  assert.equal(src.includes('const shouldTick = Boolean(interruptible) && !modalPaused;'), true);
});
