import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage stop buttons respect interruptible state and route through interrupt flow', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('function isThreadInterruptible(threadId)'), true);
  assert.equal(src.includes('const canInterrupt = computed(() => isThreadInterruptible(selectedThreadId.value));'), true);
  assert.equal(src.includes('if (!isThreadInterruptible(threadId)) {'), true);
  assert.equal(src.includes("logInfo('ui', 'chat.interrupt.skipped.notInterruptible'"), true);
  assert.equal(src.includes('interruptCurrent({ threadId });'), true);
  assert.equal(src.includes(':disabled="!canInterrupt"'), true);
  assert.equal(src.includes(":title=\"canInterrupt ? '中断当前执行' : '当前没有可中断任务'\""), true);
  assert.equal(src.includes(':disabled="!card.interruptible"'), true);
  assert.equal(src.includes(":title=\"card.interruptible ? '中断该 Agent 当前执行' : '当前没有可中断任务'\""), true);
});
