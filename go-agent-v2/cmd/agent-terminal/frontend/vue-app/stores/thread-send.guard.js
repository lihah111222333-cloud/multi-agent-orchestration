function normalizeThreadId(value) {
  return (value || '').toString().trim();
}

function normalizeThreadState(value) {
  const state = (value || '').toString().trim();
  return state || 'idle';
}

export function buildLoadedThreadMap(threads) {
  const list = Array.isArray(threads) ? threads : [];
  const map = {};
  for (const item of list) {
    const id = normalizeThreadId(item?.id);
    if (!id) continue;
    map[id] = true;
  }
  return map;
}

export function buildLoadedStateMap(threads) {
  const list = Array.isArray(threads) ? threads : [];
  const map = {};
  for (const item of list) {
    const id = normalizeThreadId(item?.id);
    if (!id) continue;
    map[id] = normalizeThreadState(item?.state);
  }
  return map;
}

export function isThreadLoadedForSend(loadedThreadMap, threadId) {
  const id = normalizeThreadId(threadId);
  if (!id) return false;
  if (!loadedThreadMap || typeof loadedThreadMap !== 'object') return false;
  return loadedThreadMap[id] === true;
}

export function upsertLoadedThread(loadedThreadMap, loadedStateMap, threadId, threadState = 'idle') {
  const id = normalizeThreadId(threadId);
  if (!id) return false;
  if (loadedThreadMap && typeof loadedThreadMap === 'object') {
    loadedThreadMap[id] = true;
  }
  if (loadedStateMap && typeof loadedStateMap === 'object') {
    loadedStateMap[id] = normalizeThreadState(threadState);
  }
  return true;
}

export function choosePreferredActiveThreadId({
  currentActiveId,
  threads,
  loadedThreadMap,
}) {
  const current = normalizeThreadId(currentActiveId);
  const list = Array.isArray(threads) ? threads : [];
  const loaded = loadedThreadMap && typeof loadedThreadMap === 'object'
    ? loadedThreadMap
    : {};

  if (current && loaded[current] === true) {
    return current;
  }

  for (const item of list) {
    const id = normalizeThreadId(item?.id);
    if (!id) continue;
    if (loaded[id] === true) {
      return id;
    }
  }

  if (current && list.some((item) => normalizeThreadId(item?.id) === current)) {
    return current;
  }

  return normalizeThreadId(list[0]?.id);
}
