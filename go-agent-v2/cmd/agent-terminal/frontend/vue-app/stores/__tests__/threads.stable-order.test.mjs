import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const THREADS_JS_PATH = new URL('../threads.js', import.meta.url);

test('thread store keeps first-seen stable order for thread rail', async () => {
  const src = await fs.readFile(THREADS_JS_PATH, 'utf8');

  assert.equal(src.includes('const threadOrderIndexById = new Map();'), true);
  assert.equal(src.includes('function ensureThreadOrderIndex(threadId) {'), true);
  assert.equal(src.includes('function sortThreadsByStableFirstSeen(threads) {'), true);
  assert.equal(src.includes('const nextThreads = sortThreadsByStableFirstSeen(unorderedThreads);'), true);
  assert.equal(src.includes('stableOrder: ensureThreadOrderIndex(item?.id),'), true);
});
