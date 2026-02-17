export function parseUnifiedDiff(diffText) {
  if (!diffText || typeof diffText !== 'string') return [];

  const lines = diffText.split('\n');
  const files = [];
  let current = null;

  for (const line of lines) {
    if (line.startsWith('diff --git') || line.startsWith('--- a/') || line.startsWith('+++ b/')) {
      if (line.startsWith('+++ b/')) {
        const filename = line.slice(6) || line.slice(4);
        current = { filename, lines: [] };
        files.push(current);
      }
      continue;
    }

    if (line.startsWith('--- ') || line.startsWith('index ') || line.startsWith('new file') || line.startsWith('deleted file')) {
      continue;
    }

    if (!current) {
      if (line.startsWith('@@') || line.startsWith('+') || line.startsWith('-')) {
        current = { filename: 'file', lines: [] };
        files.push(current);
      } else {
        continue;
      }
    }

    if (line.startsWith('@@')) {
      current.lines.push({ type: 'hunk', text: line });
    } else if (line.startsWith('+')) {
      current.lines.push({ type: 'add', text: line.slice(1) });
    } else if (line.startsWith('-')) {
      current.lines.push({ type: 'del', text: line.slice(1) });
    } else {
      current.lines.push({ type: 'ctx', text: line.startsWith(' ') ? line.slice(1) : line });
    }
  }

  return files;
}

export function diffStats(file) {
  const add = file.lines.filter((item) => item.type === 'add').length;
  const del = file.lines.filter((item) => item.type === 'del').length;
  return { add, del };
}
