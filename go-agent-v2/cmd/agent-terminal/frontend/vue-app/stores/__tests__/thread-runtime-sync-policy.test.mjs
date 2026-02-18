import test from 'node:test';
import assert from 'node:assert/strict';
import {
  isDeltaLikeEvent,
  shouldScheduleRuntimeSync,
} from '../thread-runtime-sync-policy.js';

test('isDeltaLikeEvent identifies delta events', () => {
  assert.equal(isDeltaLikeEvent('item/agentMessage/delta'), true);
  assert.equal(isDeltaLikeEvent('agent_reasoning_delta'), true);
  assert.equal(isDeltaLikeEvent('turn/started'), false);
});

test('shouldScheduleRuntimeSync blocks delta within min interval', () => {
  const allowed = shouldScheduleRuntimeSync({
    eventType: 'item/agentMessage/delta',
    nowMs: 1_000,
    lastSyncAtMs: 900,
    minIntervalMs: 200,
    timerActive: false,
  });
  assert.equal(allowed, false);
});

test('shouldScheduleRuntimeSync allows non-delta events immediately', () => {
  const allowed = shouldScheduleRuntimeSync({
    eventType: 'turn/started',
    nowMs: 1_000,
    lastSyncAtMs: 999,
    minIntervalMs: 2_000,
    timerActive: false,
  });
  assert.equal(allowed, true);
});

test('shouldScheduleRuntimeSync blocks when timer already active', () => {
  const allowed = shouldScheduleRuntimeSync({
    eventType: 'turn/started',
    nowMs: 1_000,
    lastSyncAtMs: 0,
    minIntervalMs: 0,
    timerActive: true,
  });
  assert.equal(allowed, false);
});
