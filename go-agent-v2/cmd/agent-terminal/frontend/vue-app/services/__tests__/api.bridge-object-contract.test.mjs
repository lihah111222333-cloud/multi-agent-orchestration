import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

test('callAPI bridge passes object payload without JSON stringify', async () => {
  const src = await readFile(new URL('../api.js', import.meta.url), 'utf8');

  assert.equal(src.includes('JSON.stringify(payload)'), false);
  assert.equal(src.includes('callByID(METHOD_IDS.CALL_API, method, payload)'), true);
  assert.equal(src.includes('const result = parseMaybeJSON(raw);'), false);
  assert.equal(src.includes('callAPI params must be an object'), true);
});

test('debug bridge shim drops legacy string-params compatibility', async () => {
  const src = await readFile(new URL('../../../../shim/bridge-shim.html', import.meta.url), 'utf8');

  assert.equal(src.includes('return result ? JSON.stringify(result) : \'{}\''), false);
  assert.equal(src.includes('return JSON.stringify(result);'), false);
  assert.equal(src.includes('typeof params === \'string\''), false);
  assert.equal(src.includes('JSON.parse(params)'), false);
  assert.equal(src.includes('CallAPI params must be an object'), true);
});
