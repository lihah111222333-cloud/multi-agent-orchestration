import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const WIDGETS_JS_PATH = new URL('../JsonRenderWidgets.js', import.meta.url);

test('JsonRenderWidgets registers Markdown widget', async () => {
  const src = await fs.readFile(WIDGETS_JS_PATH, 'utf8');

  assert.equal(src.includes('const JrMarkdown = defineComponent'), true);
  assert.equal(src.includes("class: 'jr-root jr-markdown chat-item-markdown codex-markdown-root'"), true);
  assert.equal(src.includes('Markdown: { component: JrMarkdown }'), true);
});
