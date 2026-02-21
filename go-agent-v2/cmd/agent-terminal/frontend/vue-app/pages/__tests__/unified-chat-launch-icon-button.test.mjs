import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage uses icon-only launch agent button with accessible label', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('class="btn btn-secondary btn-toolbar-sm launch-agent-icon-btn"'), true);
  assert.equal(src.includes('aria-label="启动 Agent"'), true);
  assert.equal(src.includes('title="启动 Agent"'), true);
  assert.equal(src.includes('<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">'), true);
  assert.equal(src.includes('<path d="M2 10l2.3-.5L10 3.8a1.3 1.3 0 10-1.8-1.8L2.5 7.7 2 10z"></path>'), true);
  assert.equal(src.includes('<path d="M7.6 2.6l1.8 1.8"></path>'), true);
});
