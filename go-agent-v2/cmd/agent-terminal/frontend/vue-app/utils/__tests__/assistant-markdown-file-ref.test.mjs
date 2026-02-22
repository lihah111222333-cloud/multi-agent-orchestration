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

test('unordered list keeps one ul across blank lines', () => {
  const html = renderAssistantMarkdown('- first\n\n- second\n\n- third');
  assert.equal(html, '<ul><li>first</li><li>second</li><li>third</li></ul>');
});
