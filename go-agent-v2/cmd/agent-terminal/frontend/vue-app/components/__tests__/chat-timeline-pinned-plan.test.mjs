import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const CHAT_TIMELINE_JS_PATH = new URL('../ChatTimeline.js', import.meta.url);

test('ChatTimeline supports top-right pinned plan spacing class', async () => {
  const src = await fs.readFile(CHAT_TIMELINE_JS_PATH, 'utf8');

  assert.equal(src.includes("pinnedPlanVisible: { type: Boolean, default: false }"), true);
  assert.equal(src.includes("<div class=\"chat-messages-vue\" :class=\"{ 'has-plan-pin': pinnedPlanVisible }\">"), true);
});
