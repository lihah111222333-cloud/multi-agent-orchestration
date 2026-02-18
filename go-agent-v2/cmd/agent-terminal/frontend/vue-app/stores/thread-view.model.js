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

export function deriveChatAgents({ threads }) {
  return Array.isArray(threads) ? threads : [];
}

export function deriveCmdAgents({ threads, mainAgentId }) {
  const threadList = Array.isArray(threads) ? threads : [];
  if (!mainAgentId) return threadList;
  return threadList.filter((item) => item?.id !== mainAgentId);
}
