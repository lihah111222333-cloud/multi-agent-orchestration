import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const APP_JS_PATH = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/vue-app/app.js';

test('app dashboard fetch uses ui/dashboard/get aggregate endpoint', async () => {
  const src = await fs.readFile(APP_JS_PATH, 'utf8');
  assert.equal(src.includes("callAPI('ui/dashboard/get'"), true);
  assert.equal(src.includes("callAPI('dashboard/agentStatus'"), false);
  assert.equal(src.includes("callAPI('dashboard/dags'"), false);
  assert.equal(src.includes("callAPI('dashboard/taskAcks'"), false);
  assert.equal(src.includes("callAPI('dashboard/taskTraces'"), false);
  assert.equal(src.includes("callAPI('dashboard/commandCards'"), false);
  assert.equal(src.includes("callAPI('dashboard/prompts'"), false);
  assert.equal(src.includes("callAPI('dashboard/sharedFiles'"), false);
});

