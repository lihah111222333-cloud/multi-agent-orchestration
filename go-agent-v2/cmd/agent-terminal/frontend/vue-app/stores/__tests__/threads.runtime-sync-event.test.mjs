import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const THREADS_JS_PATH = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/threads.js';

test('thread store sync is driven by ui/state/changed bridge event', async () => {
  const src = await fs.readFile(THREADS_JS_PATH, 'utf8');
  assert.equal(src.includes("eventType !== 'ui/state/changed'"), true);
  assert.equal(src.includes('thread-runtime-sync-policy'), false);
  assert.equal(src.includes('shouldScheduleRuntimeSync'), false);
});

