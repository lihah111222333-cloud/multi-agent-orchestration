import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('unified chat interrupt flow treats settled mode as confirm', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes('result?.settled || confirmed'), true);
  assert.equal(src.includes('if (settled)'), true);
  assert.equal(src.includes("reason: mode || 'not_confirmed'"), true);
});
