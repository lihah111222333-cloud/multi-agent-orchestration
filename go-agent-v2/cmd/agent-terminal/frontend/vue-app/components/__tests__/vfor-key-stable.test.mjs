import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const VUE_APP = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/vue-app';

test('app.js tasks v-for uses stable key instead of index', async () => {
    const src = await fs.readFile(`${VUE_APP}/app.js`, 'utf8');
    // The tasks v-for should not use bare :key="idx"
    const tasksSection = src.slice(src.indexOf('v-for="(item, idx) in tasksItems"'));
    const keyMatch = tasksSection.match(/:key="idx"/);
    assert.equal(keyMatch, null, 'tasks list should not use :key="idx"');
});

test('DataPage v-for uses stable key instead of index', async () => {
    const src = await fs.readFile(`${VUE_APP}/pages/DataPage.js`, 'utf8');
    const itemsFor = src.match(/v-for=".*items"[^>]*:key="idx"/);
    assert.equal(itemsFor, null, 'DataPage items should not use :key="idx"');
});

test('ChatTimeline attachment v-for uses stable key instead of index', async () => {
    const src = await fs.readFile(`${VUE_APP}/components/ChatTimeline.js`, 'utf8');
    const attachFor = src.match(/v-for=".*attachments[^"]*"[^>]*:key="idx"/);
    assert.equal(attachFor, null, 'attachment list should not use :key="idx"');
});

test('DiffPanel line v-for uses stable key instead of index', async () => {
    const src = await fs.readFile(`${VUE_APP}/components/DiffPanel.js`, 'utf8');
    const lineFor = src.match(/v-for=".*file\.lines[^"]*"[^>]*:key="idx"/);
    assert.equal(lineFor, null, 'diff lines should not use :key="idx"');
});
