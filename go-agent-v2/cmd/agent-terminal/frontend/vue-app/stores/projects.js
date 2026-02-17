import { reactive, computed } from '../../lib/vue.esm-browser.prod.js';
import { selectProjectDir } from '../services/api.js';

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
  } catch {
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
  localStorage.setItem(STORAGE_KEY, JSON.stringify(state.projects));
  localStorage.setItem(ACTIVE_KEY, state.active || '.');
}

function ensureActive() {
  if (state.active === '.' || state.projects.includes(state.active)) return;
  state.active = '.';
  persist();
}

function setActive(path) {
  state.active = normalizePath(path) || '.';
  persist();
}

function addProject(path) {
  const normalized = normalizePath(path);
  if (!normalized || normalized === '.') return false;
  if (!state.projects.includes(normalized)) {
    state.projects.push(normalized);
  }
  state.active = normalized;
  persist();
  return true;
}

function removeProject(path) {
  const target = normalizePath(path);
  state.projects = state.projects.filter((item) => item !== target);
  if (state.active === target) state.active = '.';
  persist();
}

function openModal(defaultPath = '') {
  const seed = defaultPath || (state.active === '.' ? '' : state.active);
  state.modalPath = normalizePath(seed);
  state.showModal = true;
}

function closeModal() {
  state.showModal = false;
  state.browsing = false;
}

async function browseDirectory() {
  // UI intent only: actual directory picker is provided by Wails bridge (Go).
  state.browsing = true;
  try {
    const value = await selectProjectDir();
    if (value) {
      state.modalPath = normalizePath(value);
    }
  } catch (error) {
    console.warn('browseDirectory failed:', error);
  } finally {
    state.browsing = false;
  }
}

function confirmModal() {
  const ok = addProject(state.modalPath);
  if (ok) closeModal();
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
