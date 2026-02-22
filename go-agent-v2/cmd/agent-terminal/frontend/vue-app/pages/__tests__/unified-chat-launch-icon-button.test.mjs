import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage uses icon-only launch agent button with accessible label', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('class="btn btn-secondary btn-toolbar-sm launch-agent-icon-btn"'), true);
  assert.equal(src.includes('aria-label="启动 Agent"'), true);
  assert.equal(src.includes('title="启动 Agent"'), true);
  assert.equal(src.includes('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">'), true);
  assert.equal(src.includes('<path d="M4 20h4l10-10a2.2 2.2 0 0 0-3.1-3.1L4.9 16.8 4 20z"></path>'), true);
  assert.equal(src.includes('<path d="M13.8 7.2l3 3"></path>'), true);
});
