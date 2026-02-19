import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const VUE_APP = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/vue-app';

// --- Inline style elimination ---

test('ProjectModal has no inline style attributes', async () => {
    const src = await fs.readFile(`${VUE_APP}/components/ProjectModal.js`, 'utf8');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    assert.equal(template.includes('style="'), false, 'ProjectModal template should not have inline styles');
});

test('UnifiedChatPage has no inline style attributes', async () => {
    const src = await fs.readFile(`${VUE_APP}/pages/UnifiedChatPage.js`, 'utf8');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    // Allow dynamic :style bindings but not static style="..."
    const staticStyles = template.match(/\sstyle="/g) || [];
    assert.equal(staticStyles.length, 0, 'UnifiedChatPage template should not have static inline styles');
});

test('SettingsPage has no inline style attributes', async () => {
    const src = await fs.readFile(`${VUE_APP}/pages/SettingsPage.js`, 'utf8');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    assert.equal(template.includes('style="'), false, 'SettingsPage template should not have inline styles');
});

// --- SettingsPage uses emit instead of Function prop ---

test('SettingsPage uses emit for refresh instead of Function prop', async () => {
    const src = await fs.readFile(`${VUE_APP}/pages/SettingsPage.js`, 'utf8');
    assert.equal(src.includes('type: Function'), false, 'should not have Function type prop');
    assert.equal(src.includes("emits:") || src.includes("emits :"), true, 'should declare emits');
});

// --- UnifiedChatPage template-store decoupling ---

test('UnifiedChatPage template does not directly call threadStore methods', async () => {
    const src = await fs.readFile(`${VUE_APP}/pages/UnifiedChatPage.js`, 'utf8');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    // Template should not reference threadStore directly
    assert.equal(template.includes('threadStore.refreshThreads'), false,
        'template should not call threadStore.refreshThreads directly');
    assert.equal(template.includes('threadStore.stopThread'), false,
        'template should not call threadStore.stopThread directly');
    assert.equal(template.includes('threadStore.promptRenameThread'), false,
        'template should not call threadStore.promptRenameThread directly');
    assert.equal(template.includes('threadStore.loadMessages'), false,
        'template should not call threadStore.loadMessages directly');
});

// --- ChatTimeline uses computed for presence ---

test('ChatTimeline uses computed for showAgentPresence', async () => {
    const src = await fs.readFile(`${VUE_APP}/components/ChatTimeline.js`, 'utf8');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    // Template should not invoke showAgentPresence as a function call
    assert.equal(template.includes('showAgentPresence()'), false,
        'template should use computed showAgentPresence, not a function call');
});

// --- ProjectModal template uses setup wrappers ---

test('ProjectModal template does not bypass setup methods', async () => {
    const src = await fs.readFile(`${VUE_APP}/components/ProjectModal.js`, 'utf8');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    assert.equal(template.includes('store.confirmModal()'), false,
        'template should use onConfirm, not store.confirmModal()');
    assert.equal(template.includes('store.closeModal()'), false,
        'template should use closeByMask, not store.closeModal()');
});

// --- app.js extracts TasksPage and CommandsPage ---

test('app.js uses TasksPage component instead of inline tasks section', async () => {
    const src = await fs.readFile(`${VUE_APP}/app.js`, 'utf8');
    assert.equal(src.includes('TasksPage'), true, 'should reference TasksPage component');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    assert.equal(template.includes('class="sub-tabs"'), false,
        'inline tasks HTML should be extracted to TasksPage');
});

test('app.js uses CommandsPage component instead of inline commands section', async () => {
    const src = await fs.readFile(`${VUE_APP}/app.js`, 'utf8');
    assert.equal(src.includes('CommandsPage'), true, 'should reference CommandsPage component');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    assert.equal(template.includes('class="split-panel"'), false,
        'inline commands HTML should be extracted to CommandsPage');
});

test('TasksPage.js exists', async () => {
    const src = await fs.readFile(`${VUE_APP}/pages/TasksPage.js`, 'utf8');
    assert.equal(src.includes('setup('), true, 'TasksPage should use Composition API');
    assert.equal(src.includes("name: 'TasksPage'"), true, 'should have correct component name');
});

test('CommandsPage.js exists', async () => {
    const src = await fs.readFile(`${VUE_APP}/pages/CommandsPage.js`, 'utf8');
    assert.equal(src.includes('setup('), true, 'CommandsPage should use Composition API');
    assert.equal(src.includes("name: 'CommandsPage'"), true, 'should have correct component name');
});

// --- SettingsPage app.js usage with emit ---

test('app.js uses @refresh event on SettingsPage instead of Function prop', async () => {
    const src = await fs.readFile(`${VUE_APP}/app.js`, 'utf8');
    const templateStart = src.indexOf('template: `');
    const template = src.slice(templateStart);
    assert.equal(template.includes(':refresh-build-info='), false,
        'should not pass refreshBuildInfo as prop');
    assert.equal(template.includes('@refresh='), true,
        'should use @refresh event binding');
});
