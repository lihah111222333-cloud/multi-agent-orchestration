import test from 'node:test';
import assert from 'node:assert/strict';
import {
  BACKEND_THREAD_STATUSES,
  normalizeStatus,
  statusLabel,
} from '../status.js';

test('backend thread statuses stay canonical', () => {
  assert.deepEqual(BACKEND_THREAD_STATUSES, [
    'idle',
    'starting',
    'thinking',
    'responding',
    'running',
    'editing',
    'waiting',
    'syncing',
    'error',
  ]);
});

test('normalizeStatus passes through backend statuses', () => {
  for (const status of BACKEND_THREAD_STATUSES) {
    assert.equal(normalizeStatus(status), status);
  }
});

test('normalizeStatus does not remap backend-owned aliases in frontend', () => {
  assert.equal(normalizeStatus('booting'), 'idle');
  assert.equal(normalizeStatus('executing'), 'idle');
  assert.equal(normalizeStatus('pending'), 'idle');
  assert.equal(normalizeStatus('failed'), 'idle');
  assert.equal(normalizeStatus('error_like_value'), 'idle');
});

test('statusLabel renders readable zh label', () => {
  assert.equal(statusLabel('running'), '执行中');
  assert.equal(statusLabel('thinking'), '思考中');
  assert.equal(statusLabel('not-a-status'), '空闲');
});
