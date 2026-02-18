import test from 'node:test';
import assert from 'node:assert/strict';
import {
  buildLoadedThreadMap,
  buildLoadedStateMap,
  isThreadLoadedForSend,
  upsertLoadedThread,
  choosePreferredActiveThreadId,
} from '../thread-send.guard.js';

test('buildLoadedThreadMap extracts loaded thread ids', () => {
  const map = buildLoadedThreadMap([
    { id: 'a', state: 'running' },
    { id: 'b', state: 'idle' },
    { id: 'a', state: 'idle' },
    { id: '' },
    null,
  ]);
  assert.deepEqual(map, { a: true, b: true });
});

test('buildLoadedStateMap keeps state by loaded thread id', () => {
  const map = buildLoadedStateMap([
    { id: 'a', state: 'running' },
    { id: 'b', state: 'idle' },
  ]);
  assert.deepEqual(map, { a: 'running', b: 'idle' });
});

test('isThreadLoadedForSend validates selected thread id', () => {
  const loaded = { a: true };
  assert.equal(isThreadLoadedForSend(loaded, ''), false);
  assert.equal(isThreadLoadedForSend(loaded, 'a'), true);
  assert.equal(isThreadLoadedForSend(loaded, 'b'), false);
});

test('upsertLoadedThread marks started thread as loaded immediately', () => {
  const loadedIds = {};
  const loadedStates = {};
  upsertLoadedThread(loadedIds, loadedStates, 'thread-1', 'starting');
  assert.deepEqual(loadedIds, { 'thread-1': true });
  assert.deepEqual(loadedStates, { 'thread-1': 'starting' });
});

test('choosePreferredActiveThreadId switches stale active id to loaded thread', () => {
  const preferred = choosePreferredActiveThreadId({
    currentActiveId: 'history-1',
    threads: [{ id: 'history-1' }, { id: 'live-1' }, { id: 'live-2' }],
    loadedThreadMap: { 'live-2': true, 'live-1': true },
  });
  assert.equal(preferred, 'live-1');
});

test('choosePreferredActiveThreadId keeps active id when already loaded', () => {
  const preferred = choosePreferredActiveThreadId({
    currentActiveId: 'live-2',
    threads: [{ id: 'history-1' }, { id: 'live-1' }, { id: 'live-2' }],
    loadedThreadMap: { 'live-2': true },
  });
  assert.equal(preferred, 'live-2');
});
