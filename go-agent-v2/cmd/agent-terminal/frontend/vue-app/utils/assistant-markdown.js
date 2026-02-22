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
  'avif',
  'bmp',
  'c',
  'cc',
  'cpp',
  'cs',
  'css',
  'csv',
  'gif',
  'go',
  'h',
  'hpp',
  'html',
  'ico',
  'ini',
  'java',
  'js',
  'jpeg',
  'jpg',
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
  'png',
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
  'svg',
  'sql',
  'swift',
  'toml',
  'ts',
  'tsx',
  'txt',
  'vue',
  'webp',
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
const CODE_KEYWORD_LINE_RE = /^\s*(package|import|func|type|var|const|class|def|from|return|if|else|for|while|switch|case|try|catch|finally|interface|enum|struct|public|private|protected|async|await|select|insert|update|delete|create|alter|drop|begin|with)\b/i;
const CODE_SYMBOL_LINE_RE = /[{}[\];]|=>|::|:=|==|!=|<=|>=|\|\||&&/;
const CODE_INDENT_LINE_RE = /^(?:\t+| {2,})\S/;
const CODE_COMMENT_LINE_RE = /^\s*(\/\/|#|\/\*|\*|--)\s*\S/;
const CODE_HTML_TAG_LINE_RE = /^\s*<\/?[a-z][\w-]*(?:\s+[^>]*)?>\s*$/i;
const CODE_START_HINT_RE = /^\s*(package|import|func|type|var|const|class|def|from|interface|enum|struct|<\?xml|<\/?[a-z]|#include|select|insert|update|delete|create|alter|drop|begin|with|\{|\[)/i;
const NON_CODE_META_LINE_RE = /^\s*(?:\[skill:[^\]]+\]|可选段落[:：]|使用方式[:：]|已注入LSP工具|普通对话直接用 markdown|##+\s+)/i;
const CODE_TOKEN_LABEL = 'CODETOK';
const GO_KEYWORD_RE = /\b(?:break|case|chan|const|continue|default|defer|else|fallthrough|for|func|go|goto|if|import|interface|map|package|range|return|select|struct|switch|type|var)\b/g;
const JS_KEYWORD_RE = /\b(?:async|await|break|case|catch|class|const|continue|debugger|default|delete|do|else|export|extends|finally|for|from|function|if|import|in|instanceof|let|new|of|return|static|super|switch|this|throw|try|typeof|var|void|while|with|yield)\b/g;
const TS_EXTRA_KEYWORD_RE = /\b(?:abstract|any|as|asserts|bigint|boolean|declare|enum|implements|infer|interface|keyof|module|namespace|never|readonly|satisfies|type|unknown)\b/g;
const PY_KEYWORD_RE = /\b(?:and|as|assert|async|await|break|class|continue|def|del|elif|else|except|finally|for|from|global|if|import|in|is|lambda|nonlocal|not|or|pass|raise|return|try|while|with|yield)\b/g;
const SQL_KEYWORD_RE = /\b(?:select|insert|update|delete|from|where|group|by|order|having|limit|offset|join|left|right|inner|outer|on|as|and|or|not|into|values|set|create|alter|drop|table|view|index|distinct|union|all|case|when|then|else|end|with)\b/gi;
const BASH_KEYWORD_RE = /\b(?:if|then|else|fi|for|in|do|done|case|esac|while|until|function|local|export|return|break|continue)\b/g;
const NUMBER_TOKEN_RE = /\b(?:0x[0-9a-fA-F]+|\d+(?:\.\d+)?(?:e[+-]?\d+)?)\b/g;
const CONST_TOKEN_RE = /\b(?:true|false|null|nil|undefined|None|True|False)\b/g;
const FUNC_TOKEN_RE = /\b[A-Za-z_]\w*(?=\s*\()/g;
const OPERATOR_TOKEN_RE = /:=|==|!=|<=|>=|&&|\|\||=>|<-|\+\+|--|\+|-|\*|\/|%|=|<|>/g;
const BLOCK_COMMENT_TOKEN_RE = /\/\*[\s\S]*?\*\//g;
const LINE_COMMENT_TOKEN_RE = /(^|\s)(\/\/.*$)/gm;
const HASH_COMMENT_TOKEN_RE = /(^|\s)(#.*$)/gm;
const SQL_COMMENT_TOKEN_RE = /(^|\s)(--.*$)/gm;
const STRING_DQ_TOKEN_RE = /&quot;(?:\\.|(?!&quot;)[\s\S])*?&quot;/g;
const STRING_SQ_TOKEN_RE = /&#39;(?:\\.|(?!&#39;)[\s\S])*?&#39;/g;
const STRING_BT_TOKEN_RE = /`(?:\\.|[^`\\])*`/g;

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
  const hasAbsolutePrefix = /^(?:[\\/]|~[\\/]|[A-Za-z]:[\\/])/.test(pathRaw);

  const filename = pathRaw.split(/[\\/]/).filter(Boolean).pop() || pathRaw;
  const filenameLower = filename.toLowerCase();
  if (BARE_FILENAME_ALLOWLIST.has(filenameLower)) return true;

  if (!filename.includes('.')) {
    // 防止把工具链路如 lsp_open_file/lsp_hover/lsp_diagnostics 误判为文件路径。
    if (hasLocation && (hasRelativePrefix || hasAbsolutePrefix)) return true;
    return false;
  }
  const extension = filename.split('.').pop() || '';
  if (!/^[a-zA-Z][a-zA-Z0-9_-]{0,20}$/.test(extension)) return false;

  const extLower = extension.toLowerCase();
  const hasLower = /[a-z]/.test(extension);
  const hasUpperAfterFirst = /[A-Z]/.test(extension.slice(1));
  if (hasLower && hasUpperAfterFirst) return false;

  const knownExtension = KNOWN_FILE_EXT_ALLOWLIST.has(extLower) || LONG_EXTENSION_ALLOWLIST.has(extLower);
  if (!knownExtension && !hasLocation) return false;
  if (!knownExtension && !hasPathSeparator && !hasRelativePrefix && !hasAbsolutePrefix) return false;

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

function renderList(type, items, orderedStart = 1) {
  if (!items.length) return '';
  const body = items.map((item) => {
    const lines = (Array.isArray(item) ? item : [item])
      .map((line) => (line || '').toString())
      .filter((line) => line.trim().length > 0);
    const content = lines.map((line) => renderInlineLine(line)).join('<br>');
    return `<li>${content}</li>`;
  }).join('');
  const start = Number.isFinite(orderedStart) ? Math.max(1, Math.trunc(orderedStart)) : 1;
  const startAttr = type === 'ol' && start > 1 ? ` start="${start}"` : '';
  return `<${type}${startAttr}>${body}</${type}>`;
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

function normalizeCodeLanguage(language) {
  const lang = (language || '').toString().trim().toLowerCase();
  if (!lang) return '';
  if (lang === 'ts') return 'typescript';
  if (lang === 'js') return 'javascript';
  if (lang === 'py') return 'python';
  if (lang === 'sh' || lang === 'zsh' || lang === 'shell') return 'bash';
  return lang;
}

function highlightEscapedCode(escapedCode, language) {
  const lang = normalizeCodeLanguage(language);
  if (!escapedCode) return '';
  if (!lang || lang === 'text' || lang === 'markdown') return escapedCode;

  let html = escapedCode;
  const tokens = [];
  const wrap = (className, value) => stashToken(tokens, CODE_TOKEN_LABEL, `<span class="${className}">${value}</span>`);

  const replaceByRegex = (regex, className) => {
    html = html.replace(regex, (match) => wrap(className, match));
  };

  const replaceWithPrefix = (regex, className) => {
    html = html.replace(regex, (_, prefix, body) => `${prefix}${wrap(className, body)}`);
  };

  const isSQL = lang === 'sql';
  const isPythonLike = lang === 'python';
  const isBashLike = lang === 'bash';
  const isCLike = lang === 'go' || lang === 'javascript' || lang === 'typescript' || lang === 'java' || lang === 'c' || lang === 'cpp' || lang === 'rust';

  if (isCLike) replaceByRegex(BLOCK_COMMENT_TOKEN_RE, 'chat-md-token-comment');
  if (isCLike) replaceWithPrefix(LINE_COMMENT_TOKEN_RE, 'chat-md-token-comment');
  if (isPythonLike || isBashLike) replaceWithPrefix(HASH_COMMENT_TOKEN_RE, 'chat-md-token-comment');
  if (isSQL) replaceWithPrefix(SQL_COMMENT_TOKEN_RE, 'chat-md-token-comment');

  replaceByRegex(STRING_BT_TOKEN_RE, 'chat-md-token-string');
  replaceByRegex(STRING_DQ_TOKEN_RE, 'chat-md-token-string');
  replaceByRegex(STRING_SQ_TOKEN_RE, 'chat-md-token-string');
  replaceByRegex(NUMBER_TOKEN_RE, 'chat-md-token-number');
  replaceByRegex(CONST_TOKEN_RE, 'chat-md-token-constant');

  if (lang === 'go') replaceByRegex(GO_KEYWORD_RE, 'chat-md-token-keyword');
  if (lang === 'javascript') replaceByRegex(JS_KEYWORD_RE, 'chat-md-token-keyword');
  if (lang === 'typescript') {
    replaceByRegex(JS_KEYWORD_RE, 'chat-md-token-keyword');
    replaceByRegex(TS_EXTRA_KEYWORD_RE, 'chat-md-token-keyword');
  }
  if (lang === 'python') replaceByRegex(PY_KEYWORD_RE, 'chat-md-token-keyword');
  if (lang === 'sql') replaceByRegex(SQL_KEYWORD_RE, 'chat-md-token-keyword');
  if (lang === 'bash') replaceByRegex(BASH_KEYWORD_RE, 'chat-md-token-keyword');

  replaceByRegex(FUNC_TOKEN_RE, 'chat-md-token-function');
  replaceByRegex(OPERATOR_TOKEN_RE, 'chat-md-token-operator');
  return restoreToken(html, CODE_TOKEN_LABEL, tokens);
}

function renderCodeBlock(codeLines, language = '') {
  const lang = normalizeCodeLanguage(language);
  const langLabel = lang ? `<span class="chat-md-code-lang">${escapeHtml(lang)}</span>` : '';
  const escaped = escapeHtml(codeLines.join('\n'));
  const content = highlightEscapedCode(escaped, lang);
  const languageClass = lang ? ` language-${escapeHtml(lang)}` : '';
  return `<pre class="chat-md-code">${langLabel}<code class="${languageClass.trim()}">${content}</code></pre>`;
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

function stripOuterBlankLines(text) {
  const lines = (text || '').split('\n');
  while (lines.length > 0 && !lines[0].trim()) lines.shift();
  while (lines.length > 0 && !lines[lines.length - 1].trim()) lines.pop();
  return lines.join('\n');
}

function analyzeCodeShape(lines) {
  let keywordLines = 0;
  let symbolLines = 0;
  let indentLines = 0;
  let commentLines = 0;
  let htmlTagLines = 0;
  let nonEmptyCount = 0;
  for (const line of lines) {
    const trimmed = (line || '').trim();
    if (!trimmed) continue;
    nonEmptyCount += 1;
    if (CODE_KEYWORD_LINE_RE.test(trimmed)) keywordLines += 1;
    if (CODE_SYMBOL_LINE_RE.test(trimmed)) symbolLines += 1;
    if (CODE_INDENT_LINE_RE.test(line || '')) indentLines += 1;
    if (CODE_COMMENT_LINE_RE.test(trimmed)) commentLines += 1;
    if (CODE_HTML_TAG_LINE_RE.test(trimmed)) htmlTagLines += 1;
  }
  return {
    keywordLines,
    symbolLines,
    indentLines,
    commentLines,
    htmlTagLines,
    nonEmptyCount,
  };
}

function isCodeSignalLine(line) {
  const raw = (line || '').toString();
  const trimmed = raw.trim();
  if (!trimmed) return false;
  if (NON_CODE_META_LINE_RE.test(trimmed)) return false;
  if (CODE_KEYWORD_LINE_RE.test(trimmed)) return true;
  if (CODE_COMMENT_LINE_RE.test(trimmed)) return true;
  if (CODE_HTML_TAG_LINE_RE.test(trimmed)) return true;
  if (CODE_SYMBOL_LINE_RE.test(trimmed)) return true;
  if (CODE_INDENT_LINE_RE.test(raw)) return true;
  if (/^[)\]}],?$/.test(trimmed)) return true;
  if (/^[A-Za-z_][\w.]*\s*\([^)]*\)\s*\{?$/.test(trimmed)) return true;
  return false;
}

function looksLikeRawCodeMessage(text) {
  const raw = (text || '').toString();
  if (!raw || raw.includes('```')) return false;
  const lines = raw.split('\n');
  if (lines.some((line) => NON_CODE_META_LINE_RE.test(((line || '') + '').trim()))) return false;
  const shape = analyzeCodeShape(lines);
  if (shape.nonEmptyCount < 3) return false;

  const firstLine = lines.find((line) => (line || '').trim().length > 0) || '';
  const hasStartHint = CODE_START_HINT_RE.test(firstLine.trim());
  const score = (shape.keywordLines * 2)
    + shape.symbolLines
    + shape.indentLines
    + shape.commentLines
    + (shape.htmlTagLines * 2);
  const threshold = Math.max(5, Math.ceil(shape.nonEmptyCount * 1.05));

  if (shape.htmlTagLines >= Math.max(3, Math.floor(shape.nonEmptyCount * 0.5))) return true;
  if (shape.keywordLines >= 2 && shape.symbolLines >= 2) return true;
  if (!hasStartHint && score < threshold + 2) return false;
  if (score >= threshold && (shape.keywordLines >= 1 || shape.symbolLines >= Math.ceil(shape.nonEmptyCount * 0.45))) {
    return true;
  }
  return false;
}

function collectRawCodeBlock(lines, startIndex) {
  const startLine = (lines[startIndex] || '').toString();
  if (!isCodeSignalLine(startLine)) return null;

  const blockLines = [];
  let codeSignalCount = 0;
  let nonEmptyCount = 0;
  let index = startIndex;

  while (index < lines.length) {
    const line = (lines[index] || '').toString();
    const trimmed = line.trim();
    if (!trimmed) {
      let lookAhead = index + 1;
      while (lookAhead < lines.length && !(lines[lookAhead] || '').toString().trim()) {
        lookAhead += 1;
      }
      if (lookAhead < lines.length && isCodeSignalLine(lines[lookAhead] || '')) {
        blockLines.push(line);
        index += 1;
        continue;
      }
      break;
    }

    if (NON_CODE_META_LINE_RE.test(trimmed)) {
      break;
    }

    const signal = isCodeSignalLine(line);
    const likelyExpr = /[=:+\-*/<>]/.test(line) || /[`"'\\]/.test(line);
    if (signal || (nonEmptyCount > 0 && likelyExpr)) {
      blockLines.push(line);
      nonEmptyCount += 1;
      if (signal) codeSignalCount += 1;
      index += 1;
      continue;
    }
    break;
  }

  if (blockLines.length === 0 || codeSignalCount < 2) return null;
  const text = blockLines.join('\n');
  if (!looksLikeRawCodeMessage(text)) return null;
  return {
    text,
    lines: blockLines,
    endIndex: index,
  };
}

function autoFencePlainLines(lines) {
  const output = [];
  let index = 0;
  let changed = false;

  while (index < lines.length) {
    const block = collectRawCodeBlock(lines, index);
    if (!block) {
      output.push(lines[index]);
      index += 1;
      continue;
    }
    const lang = inferCodeLanguage(block.text);
    output.push(`\`\`\`${lang}`);
    output.push(...block.lines);
    output.push('```');
    changed = true;
    index = block.endIndex;
  }

  return {
    lines: output,
    changed,
  };
}

function autoFenceCodeSections(text) {
  const raw = (text || '').toString();
  if (!raw) return raw;
  const lines = raw.split('\n');
  const output = [];
  let changed = false;
  let index = 0;

  while (index < lines.length) {
    const line = (lines[index] || '').toString();
    if (line.trim().startsWith('```')) {
      output.push(line);
      index += 1;
      while (index < lines.length) {
        const fenceLine = (lines[index] || '').toString();
        output.push(fenceLine);
        index += 1;
        if (fenceLine.trim().startsWith('```')) break;
      }
      continue;
    }

    const chunkStart = index;
    while (index < lines.length) {
      const chunkLine = (lines[index] || '').toString();
      if (chunkLine.trim().startsWith('```')) break;
      index += 1;
    }
    const chunk = lines.slice(chunkStart, index);
    const chunkResult = autoFencePlainLines(chunk);
    output.push(...chunkResult.lines);
    if (chunkResult.changed) changed = true;
  }

  return changed ? output.join('\n') : raw;
}

function inferCodeLanguage(text) {
  const sample = (text || '').toString();
  const trimmed = sample.trim();
  if (!trimmed) return 'text';
  if (/^\s*package\s+\w+/m.test(sample) || /^\s*func\s*(?:\([^)]*\)\s*)?\w+\s*\(/m.test(sample) || /^\s*import\s*\(/m.test(sample)) {
    return 'go';
  }
  if (/^\s*def\s+\w+\s*\(/m.test(sample) || /^\s*class\s+\w+/m.test(sample) || /^\s*from\s+\w+\s+import\s+/m.test(sample)) {
    return 'python';
  }
  if (/^\s*(?:select|insert|update|delete|create|alter|drop|with)\b/im.test(sample)) {
    return 'sql';
  }
  if (/^\s*<\?xml\b/i.test(sample)) return 'xml';
  if (/^\s*<(?:!doctype|html|head|body|main|section|article|div|span|script|style|template)\b/i.test(sample) || /<\/[a-z][\w-]*>/i.test(sample)) {
    return 'html';
  }
  if ((trimmed.startsWith('{') || trimmed.startsWith('[')) && /"[\w-]+"\s*:/.test(trimmed)) {
    return 'json';
  }
  if (/\binterface\s+\w+/.test(sample) || /\btype\s+\w+\s*=/.test(sample) || /:\s*(string|number|boolean|unknown|any)\b/.test(sample)) {
    return 'typescript';
  }
  if (/\b(const|let|var|function)\b/.test(sample) || /=>/.test(sample)) {
    return 'javascript';
  }
  if (/^\s*#!/.test(sample) || /^\s*(echo|cd|ls|pwd|grep|cat|chmod|chown|export)\b/m.test(sample)) {
    return 'bash';
  }
  return 'text';
}

function maybeAutoFenceCode(text) {
  const raw = (text || '').toString();
  if (!raw) return raw;
  const normalized = stripOuterBlankLines(raw);
  if (!normalized) return raw;
  // 当模型未输出 fenced code block 时，自动识别“纯代码回复”并补全围栏。
  if (!raw.includes('```') && looksLikeRawCodeMessage(normalized)) {
    const language = inferCodeLanguage(normalized);
    return `\`\`\`${language}\n${normalized}\n\`\`\``;
  }
  // 对“解释文字 + 代码段”混合回复，自动识别代码区并仅包裹代码区。
  return autoFenceCodeSections(raw);
}

function parseMarkdownBlocks(rawText) {
  let text = (rawText || '').toString().replace(/\r\n?/g, '\n');
  text = maybeAutoFenceCode(text);
  if (!text.trim()) return '';
  text = text.replace(/^---\n[\s\S]*?\n---\s*\n?/, '');

  const lines = text.split('\n');
  const html = [];
  let paragraphLines = [];
  let quoteLines = [];
  let listType = '';
  let listStartNumber = 1;
  let listItems = [];
  const unorderedListItemRe = /^\s*[-*]\s+(.+)$/;
  const orderedListItemRe = /^\s*(\d+)\.\s+(.+)$/;

  function listTypeOfLine(rawLine) {
    if (unorderedListItemRe.test(rawLine || '')) return 'ul';
    if (orderedListItemRe.test(rawLine || '')) return 'ol';
    return '';
  }

  function nextNonEmptyLineType(fromIndex) {
    for (let i = fromIndex; i < lines.length; i += 1) {
      const candidate = (lines[i] || '').toString();
      if (!candidate.trim()) continue;
      return listTypeOfLine(candidate);
    }
    return '';
  }

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
    const out = renderList(listType, listItems, listStartNumber);
    if (out) html.push(out);
    listType = '';
    listStartNumber = 1;
    listItems = [];
  }

  function appendToCurrentListItem(line) {
    if (!listItems.length) return;
    listItems[listItems.length - 1].push(line);
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
      if (listType) {
        const nextType = nextNonEmptyLineType(index + 1);
        if (nextType && nextType !== listType) flushList();
      }
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

    const unordered = line.match(unorderedListItemRe);
    if (unordered) {
      flushParagraph();
      if (listType && listType !== 'ul') flushList();
      if (listType !== 'ul') {
        listType = 'ul';
        listStartNumber = 1;
      }
      listItems.push([unordered[1]]);
      continue;
    }

    const ordered = line.match(orderedListItemRe);
    if (ordered) {
      flushParagraph();
      if (listType && listType !== 'ol') flushList();
      let markerNumber = Number.parseInt(ordered[1], 10);
      if (!Number.isFinite(markerNumber) || markerNumber <= 0) {
        markerNumber = 1;
      }
      if (listType !== 'ol') {
        listType = 'ol';
        listStartNumber = markerNumber;
      }
      listItems.push([ordered[2]]);
      continue;
    }

    if (listType) {
      const nextType = nextNonEmptyLineType(index + 1);
      if (nextType === listType) {
        appendToCurrentListItem(line);
        continue;
      }
      flushList();
    }
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
