import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);
const STATUS_SERVICE_JS_PATH = new URL('../../services/status.js', import.meta.url);

test('UnifiedChatPage reuses one status text source for top bar and timeline presence', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes("if (!selectedThreadId.value) return '未选择会话';"), true);
  assert.equal(src.includes('if (header) return header;'), true);
  assert.equal(src.includes("return '等待指示';"), true);
  assert.equal(src.includes("return activeStatusHeader.value || '等待指示';"), true);
  assert.equal(src.includes('return statusLabel(activeStatus.value);'), false);
  assert.equal(src.includes('{{ card.statusHeader }}'), true);
  assert.equal(src.includes("statusHeader: getThreadStatusHeader(thread.id) || '等待指示',"), true);
  assert.equal(src.includes('statusLabel(card.status)'), false);
  assert.equal(src.includes('<span>{{ displayStatusText }}</span>'), true);
  assert.equal(src.includes(':active-status-text="displayStatusText"'), true);
  assert.equal(src.includes(':active-status-meta="activeStatusMeta"'), true);
});

test('status service keeps only backend status normalization', async () => {
  const src = await fs.readFile(STATUS_SERVICE_JS_PATH, 'utf8');
  assert.equal(src.includes('export function normalizeStatus'), true);
  assert.equal(src.includes('STATUS_LABEL_ZH'), false);
  assert.equal(src.includes('export function statusLabel'), false);
});
