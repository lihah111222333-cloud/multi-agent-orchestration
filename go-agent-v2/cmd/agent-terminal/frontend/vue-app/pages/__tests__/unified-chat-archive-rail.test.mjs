import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage separates archive list from active thread list with explicit toggle', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('const showArchivedThreadList = ref(false);'), true);
  assert.equal(src.includes('const chatActiveThreadCards = computed(() => chatThreadCards.value.filter((thread) => !thread.isArchived));'), true);
  assert.equal(src.includes('const chatArchivedThreadCards = computed(() => chatThreadCards.value.filter((thread) => thread.isArchived));'), true);
  assert.equal(src.includes('const visibleChatThreadCards = computed(() => ('), true);
  assert.equal(src.includes('function toggleArchivedThreadList() {'), true);
  assert.equal(src.includes('class="thread-rail-kind-icon"'), true);
  assert.equal(src.includes('class="thread-rail-count-chip"'), true);
  assert.equal(src.includes('<path d="M10 3V5"></path>'), true);
  assert.equal(src.includes(":aria-label=\"showArchivedThreadList ? '返回会话列表' : '打开归档列表'\""), true);
  assert.equal(src.includes('v-for="thread in visibleChatThreadCards"'), true);
});
