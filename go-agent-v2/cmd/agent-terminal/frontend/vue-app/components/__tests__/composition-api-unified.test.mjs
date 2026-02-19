import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const BASE = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/vue-app/components';

test('ComposerBar uses pure Composition API (no Options API mixing)', async () => {
    const src = await fs.readFile(`${BASE}/ComposerBar.js`, 'utf8');

    // Must NOT have Options API sections
    assert.equal(src.includes('data()'), false, 'should not use Options API data()');
    assert.equal(src.includes('methods:'), false, 'should not use Options API methods');
    assert.equal(src.includes('updated()'), false, 'should not use Options API updated hook');

    // Must use Composition API equivalents
    assert.equal(src.includes('setup('), true, 'should use setup()');
    assert.equal(src.includes('ref('), true, 'should use ref()');
    assert.equal(src.includes('watch('), true, 'should use watch()');
    assert.equal(src.includes('onBeforeUnmount('), true, 'should use onBeforeUnmount()');
});

test('ProjectSelect uses Composition API setup()', async () => {
    const src = await fs.readFile(`${BASE}/ProjectSelect.js`, 'utf8');

    assert.equal(src.includes('methods:'), false, 'should not use Options API methods');
    assert.equal(src.includes('setup('), true, 'should use setup()');
});

test('SidebarNav uses Composition API setup()', async () => {
    const src = await fs.readFile(`${BASE}/SidebarNav.js`, 'utf8');

    assert.equal(src.includes('methods:'), false, 'should not use Options API methods');
    assert.equal(src.includes('setup('), true, 'should use setup()');
});
