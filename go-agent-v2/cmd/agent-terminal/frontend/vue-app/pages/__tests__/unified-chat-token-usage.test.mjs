import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage exposes context-window token usage tooltip formatting', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('Context window:'), true);
  assert.equal(src.includes('tokens used'), true);
  assert.equal(src.includes("formatTokenPercent(usedPercent)} · ${formatTokenCompact(used)} / ${formatTokenCompact(limit)}"), true);
  assert.equal(src.includes(':token-inline="activeTokenInline"'), true);
  assert.equal(src.includes(':token-tooltip="activeTokenTooltip"'), true);
  assert.equal(src.includes('getThreadTokenUsage'), true);
});
