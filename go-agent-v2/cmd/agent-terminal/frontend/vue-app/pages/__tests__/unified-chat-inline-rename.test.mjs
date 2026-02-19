import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage supports inline rename with enter/blur autosave', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes("const editingThreadId = ref('');"), true);
  assert.equal(src.includes("const editingAlias = ref('');"), true);
  assert.equal(src.includes('function beginInlineRename(threadId) {'), true);
  assert.equal(src.includes('function submitInlineRename(threadId) {'), true);
  assert.equal(src.includes('function handleInlineRenameBlur(threadId) {'), true);
  assert.equal(src.includes('v-if="editingThreadId === thread.id"'), true);
  assert.equal(src.includes('v-model="editingAlias"'), true);
  assert.equal(src.includes('@keydown.enter.prevent="submitInlineRename(thread.id)"'), true);
  assert.equal(src.includes('@blur="handleInlineRenameBlur(thread.id)"'), true);
});
