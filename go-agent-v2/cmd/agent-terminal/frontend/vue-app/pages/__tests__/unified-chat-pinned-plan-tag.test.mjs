import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage exposes dismissible top-right pinned plan tag', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('const activePinnedPlan = computed(() => {'), true);
  assert.equal(src.includes("if (item?.kind !== 'plan') continue;"), true);
  assert.equal(src.includes('function dismissPinnedPlan() {'), true);
  assert.equal(src.includes('class="chat-plan-pin"'), true);
  assert.equal(src.includes('aria-label="关闭计划标签"'), true);
  assert.equal(src.includes(':pinned-plan-visible="Boolean(activePinnedPlan)"'), true);
});
