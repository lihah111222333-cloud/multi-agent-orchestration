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
