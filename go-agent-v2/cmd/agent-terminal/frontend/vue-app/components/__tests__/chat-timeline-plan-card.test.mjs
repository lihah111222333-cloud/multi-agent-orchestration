import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const CHAT_TIMELINE_JS_PATH = new URL('../ChatTimeline.js', import.meta.url);

test('ChatTimeline renders plan items with dedicated plan card', async () => {
  const src = await fs.readFile(CHAT_TIMELINE_JS_PATH, 'utf8');

  assert.equal(src.includes("item.kind === 'plan'"), true);
  assert.equal(src.includes('class="ran-plan-card-json"'), true);
  assert.equal(src.includes('planCardSpec(item)'), true);
  assert.equal(src.includes("type: 'Card'"), true);
  assert.equal(src.includes("type: 'Markdown'"), true);
  assert.equal(src.includes(':disabled="!((item.text || \'\').toString().trim())"'), true);
});
