import test from 'node:test';
import assert from 'node:assert/strict';
import { normalizeRuntimeEventEnvelope } from '../api.js';

test('normalizeRuntimeEventEnvelope keeps plain event payload unchanged', () => {
  const input = {
    agent_id: 'thread-1',
    type: 'item/agentMessage/delta',
    data: '{"delta":"hello"}',
  };

  const result = normalizeRuntimeEventEnvelope(input);

  assert.equal(result, input);
});

test('normalizeRuntimeEventEnvelope unwraps WailsEvent object payload', () => {
  const input = {
    name: 'agent-event',
    data: {
      agent_id: 'thread-2',
      type: 'turn/completed',
      data: '{}',
    },
  };

  const result = normalizeRuntimeEventEnvelope(input);

  assert.deepEqual(result, {
    agent_id: 'thread-2',
    type: 'turn/completed',
    data: '{}',
  });
});

test('normalizeRuntimeEventEnvelope unwraps WailsEvent JSON string payload', () => {
  const input = {
    name: 'bridge-event',
    data: '{"type":"workspace/run/created","payload":{"run_key":"rk-1"}}',
  };

  const result = normalizeRuntimeEventEnvelope(input);

  assert.deepEqual(result, {
    type: 'workspace/run/created',
    payload: {
      run_key: 'rk-1',
    },
  });
});
