function normalizeString(value) {
  return (value || '').toString().trim();
}

export function defaultLayoutForMode(mode) {
  return mode === 'cmd' ? 'mix' : 'focus';
}

export function normalizeChatLayout(layout) {
  return layout === 'mix' ? 'mix' : 'focus';
}

export function normalizeCmdLayout(layout) {
  if (layout === 'overview' || layout === 'chat' || layout === 'mix') {
    return layout;
  }
  return 'mix';
}

function looksLikeMainName(name) {
  const v = normalizeString(name).toLowerCase();
  if (!v) return false;
  return v.includes('主agent') || v.includes('主 agent') || v.includes('main agent') || v === 'main';
}

export function resolveMainAgent({ mainAgentId, threads, meta }) {
  const threadList = Array.isArray(threads) ? threads : [];
  const ids = new Set(threadList.map((item) => item?.id).filter(Boolean));
  if (mainAgentId && ids.has(mainAgentId)) {
    return mainAgentId;
  }

  const metaMap = meta && typeof meta === 'object' ? meta : {};
  for (const thread of threadList) {
    const id = normalizeString(thread?.id);
    if (!id) continue;
    if (metaMap[id]?.isMain === true) {
      return id;
    }
  }

  for (const thread of threadList) {
    if (looksLikeMainName(thread?.name) || looksLikeMainName(metaMap[thread?.id]?.alias)) {
      return thread?.id || '';
    }
  }

  return '';
}

export function deriveChatAgents({ threads }) {
  return Array.isArray(threads) ? threads : [];
}

export function deriveCmdAgents({ threads, mainAgentId }) {
  const threadList = Array.isArray(threads) ? threads : [];
  if (!mainAgentId) return threadList;
  return threadList.filter((item) => item?.id !== mainAgentId);
}

export function pickMostRecentAgent({ threads, meta }) {
  const threadList = Array.isArray(threads) ? threads : [];
  if (threadList.length === 0) return '';
  const metaMap = meta && typeof meta === 'object' ? meta : {};

  return [...threadList]
    .sort((a, b) => {
      const aTs = Date.parse(metaMap[a?.id]?.lastActiveAt || '') || 0;
      const bTs = Date.parse(metaMap[b?.id]?.lastActiveAt || '') || 0;
      return bTs - aTs;
    })[0]?.id || '';
}
