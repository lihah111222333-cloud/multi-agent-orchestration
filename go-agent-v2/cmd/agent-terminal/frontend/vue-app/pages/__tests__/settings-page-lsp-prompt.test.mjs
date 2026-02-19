import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const SETTINGS_PAGE_PATH = new URL('../SettingsPage.js', import.meta.url);

test('settings page exposes LSP prompt hint read/write actions', async () => {
  const src = await fs.readFile(SETTINGS_PAGE_PATH, 'utf8');
  assert.equal(src.includes("callAPI('config/lspPromptHint/read'"), true);
  assert.equal(src.includes("callAPI('config/lspPromptHint/write'"), true);
  assert.equal(src.includes('saveLSPPromptHint'), true);
  assert.equal(src.includes('resetLSPPromptHint'), true);
});

test('settings page renders LSP prompt editor section', async () => {
  const src = await fs.readFile(SETTINGS_PAGE_PATH, 'utf8');
  assert.equal(src.includes('LSP 提示词注入'), true);
  assert.equal(src.includes('settings-prompt-textarea'), true);
  assert.equal(src.includes('保存提示词'), true);
  assert.equal(src.includes('恢复默认'), true);
});

