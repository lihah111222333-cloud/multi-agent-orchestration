import test from 'node:test';
import assert from 'node:assert/strict';
import {
  THREAD_STORE_STATE_WHITELIST,
  THREAD_STORE_UI_LOCAL_STATE_WHITELIST,
  THREAD_STORE_RUNTIME_STATE_KEYS,
  getUnexpectedThreadStoreStateKeys,
  assertThreadStoreStateWhitelist,
} from '../thread-state-whitelist.js';

test('whitelist includes ui local keys', () => {
  const allowed = new Set(THREAD_STORE_STATE_WHITELIST);
  for (const key of THREAD_STORE_UI_LOCAL_STATE_WHITELIST) {
    assert.equal(allowed.has(key), true);
  }
});

test('ui local state whitelist stays minimal and ui-only', () => {
  assert.deepEqual(THREAD_STORE_UI_LOCAL_STATE_WHITELIST, [
    'activeThreadId',
    'activeCmdThreadId',
    'mainAgentId',
    'viewPrefs',
    'loadingThreads',
    'sending',
  ]);
});

test('runtime state keys are not part of root whitelist', () => {
  const allowed = new Set(THREAD_STORE_STATE_WHITELIST);
  for (const key of THREAD_STORE_RUNTIME_STATE_KEYS) {
    assert.equal(allowed.has(key), false);
  }
});

test('detects runtime root keys as unexpected', () => {
  const unexpected = getUnexpectedThreadStoreStateKeys({
    activeThreadId: '',
    statuses: {},
  });
  assert.deepEqual(unexpected, ['statuses']);
});

test('detects unexpected root state keys', () => {
  const unexpected = getUnexpectedThreadStoreStateKeys({
    activeThreadId: '',
    sending: false,
    rogueState: true,
  });
  assert.deepEqual(unexpected, ['rogueState']);
});

test('assert throws when unknown state key exists', () => {
  assert.throws(
    () => assertThreadStoreStateWhitelist({ activeThreadId: '', unknownState: 1 }, 'unit-test'),
    /unexpected thread store state keys/,
  );
});

test('assert passes with allowed keys only', () => {
  assert.doesNotThrow(() => {
    assertThreadStoreStateWhitelist({
      activeThreadId: '',
      activeCmdThreadId: '',
      mainAgentId: '',
      viewPrefs: {},
      loadingThreads: false,
      sending: false,
    }, 'unit-test');
  });
});
