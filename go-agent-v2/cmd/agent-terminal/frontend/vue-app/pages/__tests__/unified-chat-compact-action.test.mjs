import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/UnifiedChatPage.js';
const THREADS_JS_PATH = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/threads.js';

test('UnifiedChatPage wires compact action to thread store', async () => {
  const pageSrc = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');
  const storeSrc = await fs.readFile(THREADS_JS_PATH, 'utf8');

  assert.equal(pageSrc.includes('async function compactCurrent()'), true);
  assert.equal(pageSrc.includes('props.threadStore.compactThread(threadId)'), true);
  assert.equal(pageSrc.includes('@compact="compactCurrent"'), true);
  assert.equal(pageSrc.includes(':compacting="compacting"'), true);

  assert.equal(storeSrc.includes('async function compactThread(threadId)'), true);
  assert.equal(storeSrc.includes("callAPI('thread/compact/start'"), true);
  assert.equal(storeSrc.includes('waitCompactTokenUsageRefresh'), true);
  assert.equal(storeSrc.includes('getThreadCompacting'), true);
});
