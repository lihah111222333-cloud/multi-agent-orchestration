import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const COMPOSER_BAR_JS_PATH = new URL('../ComposerBar.js', import.meta.url);

test('ComposerBar binds Esc to interrupt when thread is interruptible', async () => {
  const src = await fs.readFile(COMPOSER_BAR_JS_PATH, 'utf8');

  assert.equal(src.includes('function onEscape(event)'), true);
  assert.equal(src.includes('return Boolean(props.interruptible);'), true);
  assert.equal(src.includes('if (!Boolean(props.interruptible)) return;'), true);
  assert.equal(src.includes("logInfo('ui', 'composerBar.interrupt.escape'"), true);
  assert.equal(src.includes('@keydown.esc.exact="onEscape"'), true);
});
