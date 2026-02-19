import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const CHAT_TIMELINE_JS_PATH = new URL('../ChatTimeline.js', import.meta.url);

test('ChatTimeline presence only reuses shared top-right status text', async () => {
  const src = await fs.readFile(CHAT_TIMELINE_JS_PATH, 'utf8');

  assert.equal(src.includes("activeStatus: { type: String, default: 'idle' }"), true);
  assert.equal(src.includes("activeStatusText: { type: String, default: '' }"), true);
  assert.equal(src.includes("if (!text || text === '未选择会话') return false;"), true);
  assert.equal(src.includes("(props.activeStatus || '').toString() !== 'idle'"), false);
  assert.equal(src.includes('return true;'), true);
  // After refactor: presenceLabel is a computed, rendered without ()
  assert.equal(src.includes('<span>{{ presenceLabel }}</span>'), true);
  assert.equal(src.includes('latestPendingLabel('), false);
});
