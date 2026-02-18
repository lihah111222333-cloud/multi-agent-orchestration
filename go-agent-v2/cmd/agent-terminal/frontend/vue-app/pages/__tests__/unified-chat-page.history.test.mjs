import test from 'node:test';
import assert from 'node:assert/strict';
import { requestHistoryLoad } from '../UnifiedChatPage.js';

test('requestHistoryLoad loads selected thread history by default', async () => {
  const calls = [];
  const threadStore = {
    async loadMessages(...args) {
      calls.push(args);
    },
  };

  const loaded = await requestHistoryLoad(threadStore, 'thread-1');

  assert.equal(loaded, true);
  assert.deepEqual(calls, [['thread-1']]);
});

test('requestHistoryLoad supports force history reload', async () => {
  const calls = [];
  const threadStore = {
    async loadMessages(...args) {
      calls.push(args);
    },
  };

  const loaded = await requestHistoryLoad(threadStore, 'thread-2', { force: true, limit: 120 });

  assert.equal(loaded, true);
  assert.deepEqual(calls, [['thread-2', 120, { force: true }]]);
});

test('requestHistoryLoad ignores empty thread id', async () => {
  const threadStore = {
    async loadMessages() {
      throw new Error('should not be called');
    },
  };

  const loaded = await requestHistoryLoad(threadStore, '');

  assert.equal(loaded, false);
});
