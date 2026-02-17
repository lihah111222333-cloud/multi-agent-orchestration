import test from 'node:test';
import assert from 'node:assert/strict';
import {
  defaultLayoutForMode,
  normalizeCmdLayout,
  normalizeChatLayout,
  resolveMainAgent,
  deriveChatAgents,
  deriveCmdAgents,
  pickMostRecentAgent,
} from '../thread-view.model.js';

test('default layouts by mode', () => {
  assert.equal(defaultLayoutForMode('chat'), 'focus');
  assert.equal(defaultLayoutForMode('cmd'), 'mix');
});

test('layout normalization', () => {
  assert.equal(normalizeChatLayout('focus'), 'focus');
  assert.equal(normalizeChatLayout('mix'), 'mix');
  assert.equal(normalizeChatLayout('other'), 'focus');

  assert.equal(normalizeCmdLayout('overview'), 'overview');
  assert.equal(normalizeCmdLayout('chat'), 'chat');
  assert.equal(normalizeCmdLayout('mix'), 'mix');
  assert.equal(normalizeCmdLayout('other'), 'mix');
});

test('cmd agents exclude main agent', () => {
  const threads = [{ id: 'a' }, { id: 'b' }];
  const main = resolveMainAgent({ mainAgentId: 'a', threads, meta: {} });
  assert.equal(main, 'a');
  assert.deepEqual(deriveCmdAgents({ threads, mainAgentId: main }).map((t) => t.id), ['b']);
});

test('resolve main from meta switch first', () => {
  const threads = [{ id: 'x', name: 'worker-x' }, { id: 'y', name: '主agent' }];
  const main = resolveMainAgent({ mainAgentId: '', threads, meta: { x: { isMain: true } } });
  assert.equal(main, 'x');
});

test('chat agents keep all', () => {
  const threads = [{ id: 'a' }, { id: 'b' }];
  assert.deepEqual(deriveChatAgents({ threads }).map((t) => t.id), ['a', 'b']);
});

test('pick recent agent by lastActiveAt', () => {
  const threads = [{ id: 'a' }, { id: 'b' }];
  const id = pickMostRecentAgent({
    threads,
    meta: {
      a: { lastActiveAt: '2026-02-17T10:00:00.000Z' },
      b: { lastActiveAt: '2026-02-17T10:00:01.000Z' },
    },
  });
  assert.equal(id, 'b');
});
