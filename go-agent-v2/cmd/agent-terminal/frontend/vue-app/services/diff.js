export function parseUnifiedDiff(diffText) {
  if (!diffText || typeof diffText !== 'string') return [];

  const lines = diffText.split('\n');
  const files = [];
  let current = null;
  let oldLine = 1;
  let newLine = 1;

  function ensureCurrent(filename = 'file') {
    if (current) return current;
    current = { filename, lines: [] };
    files.push(current);
    oldLine = 1;
    newLine = 1;
    return current;
  }

  function startFile(filename) {
    current = { filename: filename || `file-${files.length + 1}`, lines: [] };
    files.push(current);
    oldLine = 1;
    newLine = 1;
    return current;
  }

  for (const line of lines) {
    if (line.startsWith('diff --git')) {
      const match = line.match(/^diff --git a\/(.+?) b\/(.+)$/);
      const filename = match?.[2] || match?.[1] || `file-${files.length + 1}`;
      startFile(filename);
      continue;
    }

    if (line.startsWith('+++ b/')) {
      const filename = line.slice(6) || line.slice(4) || current?.filename;
      if (current) {
        current.filename = filename || current.filename;
      } else {
        startFile(filename);
      }
      continue;
    }

    if (line.startsWith('+++ /dev/null')) {
      continue;
    }

    if (line.startsWith('--- a/') || line.startsWith('--- ') || line.startsWith('index ') || line.startsWith('new file') || line.startsWith('deleted file')) {
      continue;
    }

    if (line.startsWith('@@')) {
      ensureCurrent();
      const match = line.match(/^@@\s+\-(\d+)(?:,\d+)?\s+\+(\d+)(?:,\d+)?\s+@@/);
      oldLine = Number(match?.[1] || 1);
      newLine = Number(match?.[2] || 1);
      current.lines.push({
        type: 'hunk',
        text: line,
        oldNo: '',
        newNo: '',
      });
      continue;
    }

    if (line.startsWith('+')) {
      if (line.startsWith('+++')) continue;
      ensureCurrent();
      current.lines.push({
        type: 'add',
        text: line.slice(1),
        oldNo: '',
        newNo: newLine,
      });
      newLine += 1;
      continue;
    }

    if (line.startsWith('-')) {
      if (line.startsWith('---')) continue;
      ensureCurrent();
      current.lines.push({
        type: 'del',
        text: line.slice(1),
        oldNo: oldLine,
        newNo: '',
      });
      oldLine += 1;
      continue;
    }

    if (!current) {
      continue;
    }

    if (line.startsWith('\\')) {
      current.lines.push({
        type: 'meta',
        text: line,
        oldNo: '',
        newNo: '',
      });
      continue;
    }

    current.lines.push({
      type: 'ctx',
      text: line.startsWith(' ') ? line.slice(1) : line,
      oldNo: oldLine,
      newNo: newLine,
    });
    oldLine += 1;
    newLine += 1;
  }

  return files;
}

export function diffStats(file) {
  const add = file.lines.filter((item) => item.type === 'add').length;
  const del = file.lines.filter((item) => item.type === 'del').length;
  return { add, del };
}
