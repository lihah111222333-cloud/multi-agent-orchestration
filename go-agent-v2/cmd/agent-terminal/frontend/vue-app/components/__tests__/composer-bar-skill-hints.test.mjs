import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const COMPOSER_BAR_JS_PATH = new URL('../ComposerBar.js', import.meta.url);

test('ComposerBar shows matched skill hint chips with reason labels', async () => {
  const src = await fs.readFile(COMPOSER_BAR_JS_PATH, 'utf8');

  assert.equal(src.includes("skillMatches: { type: Array, default: () => [] }"), true);
  assert.equal(src.includes("skillMatchesLoading: { type: Boolean, default: false }"), true);
  assert.equal(src.includes("selectedSkillNames: { type: Array, default: () => [] }"), true);
  assert.equal(src.includes("emits: ['send', 'interrupt', 'compact', 'toggle-skill', 'select-all-skills', 'clear-skills']"), true);
  assert.equal(src.includes("const typeLabel = type === 'force' ? '强制词' : (type === 'explicit' ? '显式提及' : '触发词');"), true);
  assert.equal(src.includes('class="composer-skill-selector"'), true);
  assert.equal(src.includes("@click=\"onToggleSkill(match.name)\""), true);
  assert.equal(src.includes("{{ skillMatchesLoading ? '技能匹配中…' : ('技能选择 ' + selectedSkillNames.length + '/' + skillMatches.length) }}"), true);
  assert.equal(src.includes('skillMatchReason(match)'), true);
});
