import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const THREADS_JS_PATH = new URL('../threads.js', import.meta.url);

test('thread store sync is driven by bridge settle events', async () => {
  const src = await fs.readFile(THREADS_JS_PATH, 'utf8');
  assert.equal(src.includes("eventType === 'ui/state/changed' || eventType === 'thread/messages/page'"), true);
  assert.equal(src.includes('thread-runtime-sync-policy'), false);
  assert.equal(src.includes('shouldScheduleRuntimeSync'), false);
});
