const INLINE_CODE_RE = /`([^`\n]+)`/g;
const MARKDOWN_LINK_RE = /\[([^\]\n]+)\]\(([^)\s]+)\)/g;
const RAW_URL_RE = /https?:\/\/[^\s<]+/gi;
const BOLD_RE = /\*\*([^*]+)\*\*/g;
const ITALIC_RE = /(^|[\s(（\["'])\*([^*\n]+)\*(?=[\s).，。！？、\]"']|$)/g;
const HR_RE = /^\s*([-*_])(?:\s*\1){2,}\s*$/;

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

function renderInlineLine(raw) {
  const source = (raw || '').toString();
  if (!source) return '';

  const codeTokens = [];
  const linkTokens = [];
  let text = source.replace(INLINE_CODE_RE, (_, code) => stashToken(
    codeTokens,
    'CODE',
    `<code class="chat-md-inline-code">${escapeHtml(code)}</code>`,
  ));

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
  return `<blockquote class="chat-md-quote">${valid.map((line) => renderInlineLine(line)).join('<br>')}</blockquote>`;
}

function renderCodeBlock(codeLines, language = '') {
  const lang = (language || '').toString().trim().toLowerCase();
  const langLabel = lang ? `<span class="chat-md-code-lang">${escapeHtml(lang)}</span>` : '';
  const content = escapeHtml(codeLines.join('\n'));
  return `<pre class="chat-md-code">${langLabel}<code>${content}</code></pre>`;
}

function parseMarkdownBlocks(rawText) {
  const text = (rawText || '').toString().replace(/\r\n?/g, '\n');
  if (!text.trim()) return '';

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
