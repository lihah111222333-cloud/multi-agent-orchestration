import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const COMPOSER_BAR_JS_PATH = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/ComposerBar.js';

test('ComposerBar exposes token chip props for bottom-right usage display', async () => {
  const src = await fs.readFile(COMPOSER_BAR_JS_PATH, 'utf8');

  assert.equal(src.includes("tokenInline: { type: String, default: '' }"), true);
  assert.equal(src.includes("tokenTooltip: { type: String, default: '' }"), true);
  assert.equal(src.includes("compacting: { type: Boolean, default: false }"), true);
  assert.equal(src.includes("emits: ['send', 'interrupt', 'compact']"), true);
  assert.equal(src.includes('composer-compact-btn'), true);
  assert.equal(src.includes('composer-token-chip'), true);
  assert.equal(src.includes("tokenInline || compacting"), true);
  assert.equal(src.includes("compacting ? 'CTX 更新中…' : ('CTX ' + tokenInline)"), true);
});
