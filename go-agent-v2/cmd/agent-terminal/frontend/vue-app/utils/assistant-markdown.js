const INLINE_CODE_RE = /`([^`\n]+)`/g;
const MARKDOWN_LINK_RE = /\[([^\]\n]+)\]\(([^)\s]+)\)/g;
const RAW_URL_RE = /https?:\/\/[^\s<]+/gi;
const BOLD_RE = /\*\*([^*]+)\*\*/g;
const ITALIC_RE = /(^|[\s(（\["'])\*([^*\n]+)\*(?=[\s).，。！？、\]"']|$)/g;
const HR_RE = /^\s*([-*_])(?:\s*\1){2,}\s*$/;
const FILE_REF_COLON_RE = /^(?<path>.*?):(?<line>\d+)(?::(?<column>\d+))?(?:[-–](?<endLine>\d+)(?::(?<endColumn>\d+))?)?$/;
const FILE_REF_HASH_RE = /^(?<path>.*?)#L(?<line>\d+)(?:C(?<column>\d+))?(?:-L(?<endLine>\d+)(?:C(?<endColumn>\d+))?)?$/;
const FILE_REF_LINE_LABEL_RE = /^(?<path>.+?)\s*\((?:line|lines)\s*(?<line>\d+)(?:\s*[,，]\s*(?:col|column)\s*(?<column>\d+))?\)$/i;
const INLINE_FILE_REF_LINE_LABEL_RE = /(^|[\s(（\["'，。！？、\-])(-?[A-Za-z0-9_./\\][^\s<>()]*)\s*\((?:line|lines)\s*(\d+)(?:\s*[,，]\s*(?:col|column)\s*(\d+))?\)(?=$|[\s).，。！？、:：;；\]"'\-])/gi;
const INLINE_FILE_REF_RE = /(^|[\s(（\["'，。！？、\-])(-?[A-Za-z0-9_./\\][^\s<>()]*)(?=$|[\s).，。！？、:：;；\]"'\-])/g;
const TABLE_DELIMITER_CELL_RE = /^:?-{2,}:?$/;
const TABLE_DELIMITER_ROW_RE = /^\s*\|?(?:\s*:?-{2,}:?\s*\|)+\s*(?:\s*:?-{2,}:?\s*)?\|?\s*$/;
const CALLOUT_RE = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*$/i;
const FILE_REF_TRAILING_PUNCTUATION_RE = /[),.!?;:'"。，、！？；：）】》]+$/;
const FILE_REF_LINE_LABEL_TRAILING_PUNCTUATION_RE = /[,.!?;:'"。，、！？；：】》]+$/;
const LONG_EXTENSION_ALLOWLIST = new Set([
  'bashrc',
  'dockerignore',
  'editorconfig',
  'eslintrc',
  'gitignore',
  'gitattributes',
  'npmignore',
  'prettierignore',
  'prettierrc',
  'terraform',
  'workspace',
]);
const KNOWN_FILE_EXT_ALLOWLIST = new Set([
  'c',
  'cc',
  'cpp',
  'cs',
  'css',
  'csv',
  'go',
  'h',
  'hpp',
  'html',
  'ini',
  'java',
  'js',
  'json',
  'jsx',
  'kt',
  'less',
  'log',
  'lua',
  'm',
  'md',
  'mjs',
  'mm',
  'php',
  'pl',
  'properties',
  'proto',
  'ps1',
  'py',
  'rb',
  'rs',
  'sass',
  'scala',
  'scss',
  'sh',
  'sql',
  'swift',
  'toml',
  'ts',
  'tsx',
  'txt',
  'vue',
  'xml',
  'yaml',
  'yml',
  'zsh',
]);
const BARE_FILENAME_ALLOWLIST = new Set([
  'dockerfile',
  'makefile',
  'readme',
  'license',
]);

function escapeHtml(value) {
  return (value || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function decodeEntity(value) {
  return (value || '')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'");
}

function normalizeHref(raw) {
  const decoded = decodeEntity((raw || '').trim());
  if (!decoded) return '';
  if (/^mailto:[^\s]+$/i.test(decoded)) return decoded;
  if (!/^https?:\/\/[^\s]+$/i.test(decoded)) return '';
  try {
    const parsed = new URL(decoded);
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
      return parsed.href;
    }
  } catch {
    return '';
  }
  return '';
}

function stashToken(tokens, label, html) {
  const token = `\u0000${label}${tokens.length}\u0000`;
  tokens.push(html);
  return token;
}

function restoreToken(text, label, tokens) {
  return text.replace(new RegExp(`\\u0000${label}(\\d+)\\u0000`, 'g'), (_, index) => {
    const i = Number(index);
    if (!Number.isFinite(i) || i < 0 || i >= tokens.length) return '';
    return tokens[i];
  });
}

function trimUrlDisplay(rawUrl) {
  const url = (rawUrl || '').trim();
  if (url.length <= 62) return url;
  return `${url.slice(0, 34)}…${url.slice(-20)}`;
}

function stripTrailingPunctuation(url) {
  let text = (url || '').toString();
  let tail = '';
  while (/[),.!?;:'"。，、！？；：）】》]$/.test(text)) {
    tail = text.slice(-1) + tail;
    text = text.slice(0, -1);
  }
  return { url: text, tail };
}

function isLikelyFilePath(pathRaw, hasLocation) {
  const hasPathSeparator = /[\\/]/.test(pathRaw);
  const hasRelativePrefix = /^\.{1,2}[\\/]/.test(pathRaw);
  if (hasPathSeparator || hasRelativePrefix) return true;

  const filename = pathRaw.split(/[\\/]/).filter(Boolean).pop() || pathRaw;
  const filenameLower = filename.toLowerCase();
  if (BARE_FILENAME_ALLOWLIST.has(filenameLower)) return true;

  if (!filename.includes('.')) return false;
  const extension = filename.split('.').pop() || '';
  if (!/^[a-zA-Z][a-zA-Z0-9_-]{0,20}$/.test(extension)) return false;

  const extLower = extension.toLowerCase();
  const hasLower = /[a-z]/.test(extension);
  const hasUpperAfterFirst = /[A-Z]/.test(extension.slice(1));
  if (hasLower && hasUpperAfterFirst) return false;

  const knownExtension = KNOWN_FILE_EXT_ALLOWLIST.has(extLower) || LONG_EXTENSION_ALLOWLIST.has(extLower);
  if (!knownExtension && !hasLocation) return false;
  if (!knownExtension && !hasPathSeparator && !hasRelativePrefix) return false;

  if (extLower.length > 10 && !LONG_EXTENSION_ALLOWLIST.has(extLower)) return false;
  return true;
}

export function parseInlineFileReference(rawText) {
  const raw = (rawText || '').toString().trim();
  if (!raw) return null;
  let text = raw;
  if (text.includes('：')) {
    text = text.split('：')[0].trim();
  }
  if (/^-[A-Za-z0-9_./\\]/.test(text)) {
    text = text.slice(1).trim();
  }
  const lineLabelText = text.replace(FILE_REF_LINE_LABEL_TRAILING_PUNCTUATION_RE, '').trim();
  if (lineLabelText) {
    const lineLabelMatch = lineLabelText.match(FILE_REF_LINE_LABEL_RE);
    if (lineLabelMatch && lineLabelMatch.groups) {
      const pathRaw = (lineLabelMatch.groups.path || '').toString().trim().replace(FILE_REF_TRAILING_PUNCTUATION_RE, '');
      const line = Number.parseInt(lineLabelMatch.groups.line || '0', 10) || 0;
      const column = Number.parseInt(lineLabelMatch.groups.column || '0', 10) || 0;
      if (pathRaw && line > 0 && isLikelyFilePath(pathRaw, true)) {
        return {
          path: pathRaw,
          line,
          column,
          endLine: 0,
          endColumn: 0,
        };
      }
    }
  }
  text = text.replace(FILE_REF_TRAILING_PUNCTUATION_RE, '').trim();
  if (!text) return null;
  let pathRaw = text;
  let line = 0;
  let column = 0;
  let endLine = 0;
  let endColumn = 0;

  const colonMatch = text.match(FILE_REF_COLON_RE);
  const hashMatch = text.match(FILE_REF_HASH_RE);
  const match = colonMatch || hashMatch;
  if (match && match.groups) {
    pathRaw = (match.groups.path || '').toString().trim();
    line = Number.parseInt(match.groups.line || '0', 10) || 0;
    column = Number.parseInt(match.groups.column || '0', 10) || 0;
    endLine = Number.parseInt(match.groups.endLine || '0', 10) || 0;
    endColumn = Number.parseInt(match.groups.endColumn || '0', 10) || 0;
  }
  if (/^-[A-Za-z0-9_./\\]/.test(pathRaw)) {
    pathRaw = pathRaw.slice(1).trim();
  }
  if (!pathRaw) return null;
  if (/^https?:\/\//i.test(pathRaw)) return null;
  if (/^www\./i.test(pathRaw)) return null;
  if (/^(mailto|tel):/i.test(pathRaw)) return null;

  const hasLocation = line > 0 || column > 0 || endLine > 0 || endColumn > 0;
  if (!isLikelyFilePath(pathRaw, hasLocation)) return null;

  return {
    path: pathRaw,
    line,
    column,
    endLine,
    endColumn,
  };
}

function formatFileRefLocation(ref) {
  const line = Number(ref?.line) || 0;
  const column = Number(ref?.column) || 0;
  const endLine = Number(ref?.endLine) || 0;
  const endColumn = Number(ref?.endColumn) || 0;
  const parts = [];
  if (line > 0) {
    if (endLine > 0 && endLine !== line) {
      parts.push(`lines ${line}-${endLine}`);
    } else {
      parts.push(`line ${line}`);
    }
  }
  if (column > 0 || endColumn > 0) {
    if (column > 0 && endColumn > 0 && endColumn !== column) {
      parts.push(`columns ${column}-${endColumn}`);
    } else if (column > 0) {
      parts.push(`column ${column}`);
    } else if (endColumn > 0) {
      parts.push(`column ${endColumn}`);
    }
  }
  return parts.join(', ');
}

function formatFileRefLabel(ref) {
  const fullPath = (ref?.path || '').toString().trim();
  const filename = fullPath.split(/[\\/]/).filter(Boolean).pop() || fullPath;
  const location = formatFileRefLocation(ref);
  return location ? `${filename} (${location})` : filename;
}

function renderFileRefCode(rawRef, parsedFileRef) {
  const location = formatFileRefLocation(parsedFileRef);
  const titleText = location
    ? `${parsedFileRef.path} (${location})`
    : `${parsedFileRef.path}`;
  const label = formatFileRefLabel(parsedFileRef) || rawRef;
  const line = Number(parsedFileRef?.line) > 0 ? Number(parsedFileRef.line) : 1;
  const column = Number(parsedFileRef?.column) > 0 ? Number(parsedFileRef.column) : 0;
  return `<code class="chat-md-inline-code chat-md-file-ref is-file-ref" data-file-path="${escapeHtml(parsedFileRef.path)}" data-file-line="${line}" data-file-column="${column}" title="定位 ${escapeHtml(titleText)}">${escapeHtml(label)}</code>`;
}

function renderInlineLine(raw) {
  const source = (raw || '').toString();
  if (!source) return '';

  const codeTokens = [];
  const linkTokens = [];
  const fileRefTokens = [];
  let text = source.replace(INLINE_CODE_RE, (_, code) => {
    const parsedFileRef = parseInlineFileReference(code);
    if (!parsedFileRef) {
      return stashToken(
        codeTokens,
        'CODE',
        `<code class="chat-md-inline-code">${escapeHtml(code)}</code>`,
      );
    }
    return stashToken(
      codeTokens,
      'CODE',
      renderFileRefCode(code, parsedFileRef),
    );
  });

  text = text.replace(MARKDOWN_LINK_RE, (_, label, href) => {
    const safeHref = normalizeHref(href);
    if (!safeHref) {
      return `${label} (${href})`;
    }
    return stashToken(
      linkTokens,
      'LINK',
      `<a class="chat-md-link" href="${escapeHtml(safeHref)}" target="_blank" rel="noopener noreferrer">${escapeHtml(label)}</a>`,
    );
  });

  text = text.replace(INLINE_FILE_REF_LINE_LABEL_RE, (full, prefix, path, lineText, columnText) => {
    const line = Number.parseInt((lineText || '').toString(), 10) || 0;
    const column = Number.parseInt((columnText || '').toString(), 10) || 0;
    const rawRef = column > 0
      ? `${path} (line ${line}, column ${column})`
      : `${path} (line ${line})`;
    const parsedFileRef = parseInlineFileReference(rawRef);
    if (!parsedFileRef) return full;
    return `${prefix}${stashToken(fileRefTokens, 'FILEREF', renderFileRefCode(rawRef, parsedFileRef))}`;
  });

  text = text.replace(INLINE_FILE_REF_RE, (full, prefix, candidate) => {
    const parsedFileRef = parseInlineFileReference(candidate);
    if (!parsedFileRef) return full;
    return `${prefix}${stashToken(fileRefTokens, 'FILEREF', renderFileRefCode(candidate, parsedFileRef))}`;
  });

  text = escapeHtml(text);

  text = text.replace(RAW_URL_RE, (rawUrl) => {
    const { url, tail } = stripTrailingPunctuation(rawUrl);
    const safeHref = normalizeHref(url);
    if (!safeHref) return rawUrl;
    const label = trimUrlDisplay(url);
    const link = `<a class="chat-md-link" href="${escapeHtml(safeHref)}" target="_blank" rel="noopener noreferrer">${escapeHtml(label)}</a>`;
    return `${link}${tail}`;
  });

  text = text.replace(BOLD_RE, '<strong>$1</strong>');
  text = text.replace(ITALIC_RE, '$1<em>$2</em>');

  text = restoreToken(text, 'LINK', linkTokens);
  text = restoreToken(text, 'FILEREF', fileRefTokens);
  text = restoreToken(text, 'CODE', codeTokens);
  return text;
}

function renderParagraph(lines) {
  const valid = lines.filter((line) => (line || '').trim().length > 0);
  if (valid.length === 0) return '';
  return `<p>${valid.map((line) => renderInlineLine(line)).join('<br>')}</p>`;
}

function renderList(type, items) {
  if (!items.length) return '';
  const body = items.map((item) => `<li>${renderInlineLine(item)}</li>`).join('');
  return `<${type}>${body}</${type}>`;
}

function renderBlockQuote(lines) {
  const valid = lines.filter((line) => (line || '').trim().length > 0);
  if (valid.length === 0) return '';
  const first = (valid[0] || '').trim();
  const callout = first.match(CALLOUT_RE);
  if (callout) {
    const type = callout[1].toLowerCase();
    const titleMap = {
      note: 'NOTE',
      tip: 'TIP',
      important: 'IMPORTANT',
      warning: 'WARNING',
      caution: 'CAUTION',
    };
    const bodyLines = valid.slice(1);
    const body = bodyLines.length > 0
      ? `<div class="chat-md-callout-body">${bodyLines.map((line) => renderInlineLine(line)).join('<br>')}</div>`
      : '';
    return `<blockquote class="chat-md-quote chat-md-callout chat-md-callout-${type}"><div class="chat-md-callout-title">${titleMap[type] || type.toUpperCase()}</div>${body}</blockquote>`;
  }
  return `<blockquote class="chat-md-quote">${valid.map((line) => renderInlineLine(line)).join('<br>')}</blockquote>`;
}

function renderCodeBlock(codeLines, language = '') {
  const lang = (language || '').toString().trim().toLowerCase();
  const langLabel = lang ? `<span class="chat-md-code-lang">${escapeHtml(lang)}</span>` : '';
  const content = escapeHtml(codeLines.join('\n'));
  return `<pre class="chat-md-code">${langLabel}<code>${content}</code></pre>`;
}

function parseTableRow(line) {
  let text = (line || '').trim();
  if (text.startsWith('|')) text = text.slice(1);
  if (text.endsWith('|')) text = text.slice(0, -1);
  return text.split('|').map((cell) => cell.trim());
}

function parseTableAlignments(delimiterLine) {
  return parseTableRow(delimiterLine).map((cell) => {
    if (!TABLE_DELIMITER_CELL_RE.test(cell)) return '';
    const starts = cell.startsWith(':');
    const ends = cell.endsWith(':');
    if (starts && ends) return 'center';
    if (ends) return 'right';
    return 'left';
  });
}

function isTableDelimiterLine(line) {
  return TABLE_DELIMITER_ROW_RE.test((line || '').trim());
}

function renderTable(headers, aligns, rows) {
  if (!Array.isArray(headers) || headers.length === 0) return '';
  const th = headers.map((cell, index) => {
    const align = aligns[index] || 'left';
    return `<th data-align="${align}">${renderInlineLine(cell)}</th>`;
  }).join('');
  const tbody = rows.map((row) => {
    const cells = headers.map((_, index) => {
      const align = aligns[index] || 'left';
      const value = row[index] || '';
      return `<td data-align="${align}">${renderInlineLine(value)}</td>`;
    }).join('');
    return `<tr>${cells}</tr>`;
  }).join('');
  return `<div class="chat-md-table-wrap"><table class="chat-md-table"><thead><tr>${th}</tr></thead><tbody>${tbody}</tbody></table></div>`;
}

function parseMarkdownBlocks(rawText) {
  let text = (rawText || '').toString().replace(/\r\n?/g, '\n');
  if (!text.trim()) return '';
  text = text.replace(/^---\n[\s\S]*?\n---\s*\n?/, '');

  const lines = text.split('\n');
  const html = [];
  let paragraphLines = [];
  let quoteLines = [];
  let listType = '';
  let listItems = [];

  function flushParagraph() {
    const out = renderParagraph(paragraphLines);
    if (out) html.push(out);
    paragraphLines = [];
  }

  function flushQuote() {
    const out = renderBlockQuote(quoteLines);
    if (out) html.push(out);
    quoteLines = [];
  }

  function flushList() {
    const out = renderList(listType, listItems);
    if (out) html.push(out);
    listType = '';
    listItems = [];
  }

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    const trimmed = line.trim();

    if (trimmed.startsWith('```')) {
      flushParagraph();
      flushQuote();
      flushList();
      const language = trimmed.slice(3).trim();
      const codeLines = [];
      index += 1;
      while (index < lines.length && !lines[index].trim().startsWith('```')) {
        codeLines.push(lines[index]);
        index += 1;
      }
      html.push(renderCodeBlock(codeLines, language));
      continue;
    }

    if (!trimmed) {
      flushParagraph();
      flushQuote();
      flushList();
      continue;
    }

    if (HR_RE.test(trimmed)) {
      flushParagraph();
      flushQuote();
      flushList();
      html.push('<hr class="chat-md-divider">');
      continue;
    }

    const heading = line.match(/^(#{1,4})\s+(.+)$/);
    if (heading) {
      flushParagraph();
      flushQuote();
      flushList();
      const level = heading[1].length;
      html.push(`<h${level}>${renderInlineLine(heading[2].trim())}</h${level}>`);
      continue;
    }

    const maybeTableHeader = line.includes('|');
    const delimiterLine = lines[index + 1] || '';
    if (maybeTableHeader && isTableDelimiterLine(delimiterLine)) {
      flushParagraph();
      flushQuote();
      flushList();
      const headers = parseTableRow(line);
      const aligns = parseTableAlignments(delimiterLine);
      const rows = [];
      index += 2;
      while (index < lines.length) {
        const rowLine = lines[index];
        const rowTrimmed = (rowLine || '').trim();
        if (!rowTrimmed || !rowLine.includes('|')) break;
        rows.push(parseTableRow(rowLine));
        index += 1;
      }
      html.push(renderTable(headers, aligns, rows));
      index -= 1;
      continue;
    }

    const quote = line.match(/^>\s?(.*)$/);
    if (quote) {
      flushParagraph();
      flushList();
      quoteLines.push(quote[1]);
      continue;
    }
    flushQuote();

    const unordered = line.match(/^\s*[-*]\s+(.+)$/);
    if (unordered) {
      flushParagraph();
      if (listType && listType !== 'ul') flushList();
      listType = 'ul';
      listItems.push(unordered[1]);
      continue;
    }

    const ordered = line.match(/^\s*\d+\.\s+(.+)$/);
    if (ordered) {
      flushParagraph();
      if (listType && listType !== 'ol') flushList();
      listType = 'ol';
      listItems.push(ordered[1]);
      continue;
    }

    flushList();
    paragraphLines.push(line);
  }

  flushParagraph();
  flushQuote();
  flushList();
  return html.join('');
}

export function renderAssistantMarkdown(rawText) {
  return parseMarkdownBlocks(rawText);
}
