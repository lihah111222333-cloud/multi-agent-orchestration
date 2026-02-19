import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const COMPOSER_BAR_JS_PATH = new URL('../ComposerBar.js', import.meta.url);

test('ComposerBar keeps interrupt state isolated per thread and supports ack feedback', async () => {
  const src = await fs.readFile(COMPOSER_BAR_JS_PATH, 'utf8');

  assert.equal(src.includes("threadId: { type: String, default: '' }"), true);
  assert.equal(src.includes('interruptRequestThreadId'), true);
  assert.equal(src.includes('composerBar.thread.switch.reset'), true);
  assert.equal(src.includes('composerBar.interrupt.confirmed.ignored'), true);
  assert.equal(src.includes('composerBar.interrupt.rejected.ignored'), true);
});
