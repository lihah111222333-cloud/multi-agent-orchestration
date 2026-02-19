import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const UNIFIED_CHAT_PAGE_JS_PATH = new URL('../UnifiedChatPage.js', import.meta.url);

test('UnifiedChatPage previews skill matches from composer input', async () => {
  const src = await fs.readFile(UNIFIED_CHAT_PAGE_JS_PATH, 'utf8');

  assert.equal(src.includes("import { callAPI, copyTextToClipboard, resolveThreadIdentity } from '../services/api.js';"), true);
  assert.equal(src.includes('composerSkillMatches ='), true);
  assert.equal(src.includes("const composerSkillPreviewLoading = ref(false);"), true);
  assert.equal(src.includes("await callAPI('skills/match/preview', {"), true);
  assert.equal(src.includes('composerSkillPreviewQueued'), true);
  assert.equal(src.includes('requestComposerSkillPreview(threadId, text);'), true);
  assert.equal(src.includes("[() => selectedThreadId.value, () => composer.state.text]"), true);
  assert.equal(src.includes("logDebug('ui', 'chat.skillPreview.done'"), true);
  assert.equal(src.includes('const selectedSkills = [...composerSelectedSkillNames.value];'), true);
  assert.equal(src.includes('const manualSkillSelection = composerSkillMatches.value.length > 0 || selectedSkills.length > 0;'), true);
  assert.equal(src.includes('selectedSkills,'), true);
  assert.equal(src.includes('manualSkillSelection,'), true);
  assert.equal(src.includes(':skill-matches="composerSkillMatches"'), true);
  assert.equal(src.includes(':skill-matches-loading="composerSkillPreviewLoading"'), true);
  assert.equal(src.includes(':selected-skill-names="composerSelectedSkillNames"'), true);
  assert.equal(src.includes('@toggle-skill="toggleComposerSelectedSkill"'), true);
});
