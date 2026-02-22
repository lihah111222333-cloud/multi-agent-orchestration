import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const CHAT_TIMELINE_JS_PATH = new URL('../ChatTimeline.js', import.meta.url);

test('ChatTimeline supports top-right pinned plan spacing class', async () => {
  const src = await fs.readFile(CHAT_TIMELINE_JS_PATH, 'utf8');

  assert.equal(src.includes("pinnedPlanVisible: { type: Boolean, default: false }"), true);
  assert.equal(src.includes("pinnedPlanItemId: { type: [String, Number], default: null }"), true);
  assert.equal(src.includes('function resolvePlanTimelineKey(item) {'), true);
  assert.equal(src.includes('if (item?.kind !== \'plan\') return true;'), true);
  assert.equal(src.includes('const itemKey = resolvePlanTimelineKey(item);'), true);
  assert.match(src, /class="chat-messages-vue hide-scrollbar"/);
  assert.match(src, /:class="\{\s*'has-plan-pin': pinnedPlanVisible\s*\}"/);
});
