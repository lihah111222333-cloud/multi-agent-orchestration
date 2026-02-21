import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const PROJECT_SELECT_JS_PATH = new URL('../ProjectSelect.js', import.meta.url);

test('ProjectSelect uses folder icon for add-project action', async () => {
  const src = await fs.readFile(PROJECT_SELECT_JS_PATH, 'utf8');

  assert.equal(src.includes('class="btn btn-ghost btn-xs project-add-btn"'), true);
  assert.equal(src.includes('aria-label="添加项目"'), true);
  assert.equal(src.includes('<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6"'), true);
  assert.equal(src.includes('<path d="M2.8 6.4C2.8 5.5 3.5 4.8 4.4 4.8H8.1L9.8 6.6H15.6C16.5 6.6 17.2 7.3 17.2 8.2V13.6C17.2 14.5 16.5 15.2 15.6 15.2H4.4C3.5 15.2 2.8 14.5 2.8 13.6V6.4Z"></path>'), true);
});
