import { reactive, computed } from '../../lib/vue.esm-browser.prod.js';
import { selectProjectDir } from '../services/api.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';

const STORAGE_KEY = 'agent-orchestrator.projects';
const ACTIVE_KEY = 'agent-orchestrator.projects.active';

function normalizePath(path) {
  let value = (path || '').trim();
  if (!value) return '';
  if (value !== '/' && !/^[a-zA-Z]:[\\/]?$/.test(value)) {
    value = value.replace(/[\\/]+$/, '');
  }
  return value;
}

function loadList() {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
    return Array.isArray(parsed) ? parsed : [];
  } catch (error) {
    logWarn('project', 'storage.load.failed', { error });
    return [];
  }
}

const state = reactive({
  projects: loadList(),
  active: normalizePath(localStorage.getItem(ACTIVE_KEY) || '.') || '.',
  showModal: false,
  modalPath: '',
  browsing: false,
});

function persist() {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state.projects));
    localStorage.setItem(ACTIVE_KEY, state.active || '.');
  } catch (error) {
    logWarn('project', 'storage.persist.failed', { error });
  }
}

function ensureActive() {
  if (state.active === '.' || state.projects.includes(state.active)) return;
  state.active = '.';
  persist();
}

function setActive(path) {
  state.active = normalizePath(path) || '.';
  persist();
  logInfo('project', 'active.changed', { active: state.active });
}

function addProject(path) {
  const normalized = normalizePath(path);
  if (!normalized || normalized === '.') return false;
  if (!state.projects.includes(normalized)) {
    state.projects.push(normalized);
  }
  state.active = normalized;
  persist();
  logInfo('project', 'added', { path: normalized, total: state.projects.length });
  return true;
}

function removeProject(path) {
  const target = normalizePath(path);
  state.projects = state.projects.filter((item) => item !== target);
  if (state.active === target) state.active = '.';
  persist();
  logInfo('project', 'removed', { path: target, total: state.projects.length });
}

function openModal(defaultPath = '') {
  const seed = defaultPath || (state.active === '.' ? '' : state.active);
  state.modalPath = normalizePath(seed);
  state.showModal = true;
  logDebug('project', 'modal.opened', { seed: state.modalPath });
}

function closeModal() {
  state.showModal = false;
  state.browsing = false;
  logDebug('project', 'modal.closed', {});
}

async function browseDirectory() {
  // UI intent only: actual directory picker is provided by Wails bridge (Go).
  state.browsing = true;
  const start = Date.now();
  logInfo('project', 'browse.start', {});
  try {
    const value = await selectProjectDir();
    if (value) {
      state.modalPath = normalizePath(value);
    }
    logInfo('project', 'browse.done', {
      selected: Boolean(value),
      path: value || '',
      duration_ms: Date.now() - start,
    });
  } catch (error) {
    logWarn('project', 'browse.failed', {
      error,
      duration_ms: Date.now() - start,
    });
  } finally {
    state.browsing = false;
  }
}

function confirmModal() {
  const ok = addProject(state.modalPath);
  if (ok) {
    closeModal();
  }
  logInfo('project', 'modal.confirm', {
    ok,
    path: normalizePath(state.modalPath),
  });
  return ok;
}

function quickAdd() {
  openModal();
}

ensureActive();

export function useProjectStore() {
  return {
    state,
    projectOptions: computed(() => [{ value: '.', label: '当前目录 (.)', full: '.' }, ...state.projects.map((path) => {
      const segments = path.split('/').filter(Boolean);
      const short = segments.slice(-2).join('/') || path;
      return { value: path, label: short, full: path };
    })]),
    normalizePath,
    setActive,
    addProject,
    removeProject,
    openModal,
    closeModal,
    confirmModal,
    browseDirectory,
    quickAdd,
  };
}
