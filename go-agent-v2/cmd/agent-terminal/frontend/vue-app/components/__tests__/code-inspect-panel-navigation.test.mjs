import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const CODE_INSPECT_PANEL_JS_PATH = new URL('../CodeInspectPanel.js', import.meta.url);

test('CodeInspectPanel supports IDE-like jump and problem navigation', async () => {
  const src = await fs.readFile(CODE_INSPECT_PANEL_JS_PATH, 'utf8');

  assert.equal(src.includes('const jumpLineInput = ref(\'\');'), true);
  assert.equal(src.includes('function jumpToLine(rawLine, rawColumn = selectedColumn.value, options = {})'), true);
  assert.equal(src.includes('function jumpToNextDiagnostic()'), true);
  assert.equal(src.includes('function jumpToPrevDiagnostic()'), true);
  assert.equal(src.includes('if ((event.metaKey || event.ctrlKey) && lower === \'g\')'), true);
  assert.equal(src.includes('if (key === \'F8\')'), true);
  assert.equal(src.includes('@keydown="onPanelKeydown"'), true);
  assert.equal(src.includes('class="code-inspect-jump-input"'), true);
  assert.equal(src.includes('@click="jumpToDiagnostic(item)"'), true);
  assert.equal(src.includes('@click.stop="onLineClick(Number(line.line))"'), true);
});
