import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage thread rail exposes pin toggle button and store wiring', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('const pinnedAt = (typeof props.threadStore.getThreadPinnedAt === \'function\')'), true);
  assert.equal(src.includes('isPinned: Number.isFinite(pinnedAt) && pinnedAt > 0,'), true);
  assert.equal(src.includes('function toggleThreadPin(threadId) {'), true);
  assert.equal(src.includes('props.threadStore.toggleThreadPin(threadId);'), true);
  assert.equal(src.includes('class="thread-rail-pin-btn"'), true);
  assert.equal(src.includes('@click.stop="toggleThreadPin(thread.id)"'), true);
});
