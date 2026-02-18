import test from 'node:test';
import assert from 'node:assert/strict';

async function loadProjectStore(mockCallAPI) {
  globalThis.__AO_PROJECTS_CALL_API__ = mockCallAPI;
  const mod = await import(`../projects.js?test=${Date.now()}-${Math.random()}`);
  return mod.useProjectStore();
}

test('reloadProjects reads backend-owned ui/projects/get', async () => {
  const calls = [];
  const store = await loadProjectStore(async (method, params) => {
    calls.push({ method, params });
    if (method === 'ui/projects/get') {
      return {
        projects: ['/repo/a/', '/repo/a', '/repo/b'],
        active: '/repo/a/',
      };
    }
    return { projects: [], active: '.' };
  });

  await store.reloadProjects();

  assert.equal(calls.some((item) => item.method === 'ui/projects/get'), true);
  assert.deepEqual(store.state.projects, ['/repo/a', '/repo/b']);
  assert.equal(store.state.active, '/repo/a');
  delete globalThis.__AO_PROJECTS_CALL_API__;
});

test('addProject delegates to ui/projects/add and applies response', async () => {
  const calls = [];
  const store = await loadProjectStore(async (method, params) => {
    calls.push({ method, params });
    if (method === 'ui/projects/get') {
      return { projects: ['/repo/a'], active: '/repo/a' };
    }
    if (method === 'ui/projects/add') {
      return { projects: ['/repo/a', '/repo/b'], active: '/repo/b' };
    }
    return { projects: ['/repo/a'], active: '/repo/a' };
  });

  const ok = await store.addProject('/repo/b');

  assert.equal(ok, true);
  assert.equal(calls.some((item) => item.method === 'ui/projects/add'), true);
  assert.deepEqual(store.state.projects, ['/repo/a', '/repo/b']);
  assert.equal(store.state.active, '/repo/b');
  delete globalThis.__AO_PROJECTS_CALL_API__;
});

