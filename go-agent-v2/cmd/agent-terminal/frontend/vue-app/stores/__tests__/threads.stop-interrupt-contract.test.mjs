import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const THREADS_JS_PATH = new URL('../threads.js', import.meta.url);

test('thread store stop flow uses interrupt contract only', async () => {
  const src = await fs.readFile(THREADS_JS_PATH, 'utf8');

  assert.equal(src.includes("callAPI('turn/interrupt', { threadId })"), true);
  assert.equal(src.includes("callAPI('thread/abort'"), false);
  assert.equal(src.includes('interruptResult?.confirmed'), true);
  assert.equal(src.includes('interrupt_confirmed'), true);
  assert.equal(src.includes('interrupt_terminal_completed'), true);
  assert.equal(src.includes('interrupt_terminal_failed'), true);
  assert.equal(src.includes('no_active_turn'), true);
});
