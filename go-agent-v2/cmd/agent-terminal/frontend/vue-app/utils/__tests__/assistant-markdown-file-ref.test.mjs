import test from 'node:test';
import assert from 'node:assert/strict';

import { parseInlineFileReference, renderAssistantMarkdown } from '../assistant-markdown.js';

test('tool chain text with slashes is not parsed as a file reference', () => {
  const chain = 'lsp_open_file/lsp_hover/lsp_diagnostics';
  assert.equal(parseInlineFileReference(chain), null);

  const html = renderAssistantMarkdown(`这个不是文档，是工具：${chain}`);
  assert.equal(html.includes(chain), true);
  assert.equal(html.includes('data-file-path="lsp_open_file/lsp_hover/lsp_diagnostics"'), false);
});

test('real file reference still renders with file metadata', () => {
  const html = renderAssistantMarkdown('文件示例 internal/apiserver/methods_turn.go:455');
  assert.equal(html.includes('data-file-path="internal/apiserver/methods_turn.go"'), true);
  assert.equal(html.includes('data-file-line="455"'), true);
});

test('absolute image path renders as clickable file reference', () => {
  const imagePath = '/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/cmd/agent-terminal/frontend/review/toolbar-icons-review.png';
  const html = renderAssistantMarkdown(`预览图路径：\`${imagePath}\``);
  assert.equal(html.includes(`data-file-path="${imagePath}"`), true);
  assert.equal(html.includes('data-file-line="1"'), true);
  assert.equal(html.includes('is-file-ref'), true);
});

test('ordered list keeps one ol across blank lines', () => {
  const html = renderAssistantMarkdown('1. first\n\n1. second\n\n1. third');
  assert.equal(html, '<ol><li>first</li><li>second</li><li>third</li></ol>');
});

test('ordered list keeps sequential numbering when items have detail lines', () => {
  const md = [
    '1. first',
    'detail A',
    '',
    '1. second',
    'detail B',
    '',
    '1. third',
    'detail C',
    '',
    '1. fourth',
  ].join('\n');
  const html = renderAssistantMarkdown(md);
  assert.equal(
    html,
    '<ol><li>first<br>detail A</li><li>second<br>detail B</li><li>third<br>detail C</li><li>fourth</li></ol>',
  );
});

test('ordered list keeps explicit start number', () => {
  const html = renderAssistantMarkdown('3. third\n4. fourth');
  assert.equal(html, '<ol start="3"><li>third</li><li>fourth</li></ol>');
});

test('ordered list preserves numbering when split by unordered details', () => {
  const md = [
    '1. protocol.go + 新增',
    '- 第一条',
    '- 第二条',
    '',
    '2. client.go',
    '- 第三条',
    '',
    '3. manager.go',
    '- 第四条',
  ].join('\n');
  const html = renderAssistantMarkdown(md);
  assert.equal((html.match(/<ol/g) || []).length, 3);
  assert.equal(html.includes('<ol start="2">'), true);
  assert.equal(html.includes('<ol start="3">'), true);
  assert.equal(html.includes('<ul><li>第一条</li><li>第二条</li></ul>'), true);
  assert.equal(html.includes('<ul><li>第三条</li></ul>'), true);
  assert.equal(html.includes('<ul><li>第四条</li></ul>'), true);
});

test('ordered list keeps explicit restart number for 1/ul/1 pattern', () => {
  const md = [
    '1. 第一阶段',
    '- 说明 A',
    '',
    '1. 第二阶段',
    '- 说明 B',
    '',
    '1. 第三阶段',
    '- 说明 C',
  ].join('\n');
  const html = renderAssistantMarkdown(md);
  assert.equal((html.match(/<ol/g) || []).length, 3);
  assert.equal(html.includes('<ol start="2">'), false);
  assert.equal(html.includes('<ol start="3">'), false);
});

test('unordered list keeps one ul across blank lines', () => {
  const html = renderAssistantMarkdown('- first\n\n- second\n\n- third');
  assert.equal(html, '<ul><li>first</li><li>second</li><li>third</li></ul>');
});
