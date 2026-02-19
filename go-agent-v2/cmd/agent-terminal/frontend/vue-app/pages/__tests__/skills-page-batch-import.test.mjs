import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

test('SkillsPage uses multi-directory picker for batch import', async () => {
  const src = await readFile(new URL('../SkillsPage.js', import.meta.url), 'utf8');

  assert.equal(src.includes('selectProjectDirs'), true);
  assert.equal(src.includes("callAPI('skills/local/importDir', { paths: folderPaths })"), true);
  assert.equal(src.includes("批量导入技能目录"), true);
  assert.equal(src.includes('选择目录中存在重复技能名'), true);
  assert.equal(src.includes('skills-failure-list'), true);
  assert.equal(src.includes('请先选择会话，再刷新绑定'), true);
  assert.equal(src.includes('会话绑定已刷新（'), true);
  assert.equal(src.includes('loadThreadSkills({ silent: true })'), true);
});
