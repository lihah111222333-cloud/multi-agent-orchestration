import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const ACTIVITY_PANEL_JS_PATH = new URL('../ActivityPanel.js', import.meta.url);

test('ActivityPanel command records support click-to-expand details', async () => {
  const src = await fs.readFile(ACTIVITY_PANEL_JS_PATH, 'utf8');

  assert.equal(src.includes('commandRecords: { type: Array, default: () => [] }'), true);
  assert.equal(src.includes('const expandedCommandID = ref(\'\');'), true);
  assert.equal(src.includes('function toggleCommandRecord(commandID)'), true);
  assert.equal(src.includes('function isCommandExpanded(record)'), true);
  assert.equal(src.includes('@click="toggleCommandRecord(record.id)"'), true);
  assert.equal(src.includes('v-if="isCommandExpanded(record)" class="command-detail"'), true);
  assert.equal(src.includes('class="command-detail-code"'), true);
  assert.equal(src.includes('class="command-detail-output"'), true);
});
