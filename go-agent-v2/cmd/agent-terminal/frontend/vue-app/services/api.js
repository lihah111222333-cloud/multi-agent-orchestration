// Bridge Policy (must keep):
// - This file is the only frontend bridge for desktop capabilities.
// - Vue/JS is UI-only; system capabilities must go through Wails v3 runtime bridge.
// - Do not introduce browser-native fallbacks for file/system access.
const METHOD_IDS = Object.freeze({
  CALL_API: 1055257995,
  GET_BUILD_INFO: 3168473285,
  GET_GROUP: 4127719990,
  SAVE_CLIPBOARD_IMAGE: 3932748547,
  SELECT_FILES: 937743440,
  SELECT_PROJECT_DIR: 373469749,
});

function parseMaybeJSON(raw) {
  if (raw == null || raw === '') return {};
  if (typeof raw === 'object') return raw;
  try {
    return JSON.parse(raw);
  } catch {
    return {};
  }
}

let runtimePromise = null;

async function waitRuntime() {
  if (!runtimePromise) {
    runtimePromise = import('/wails/runtime.js')
      .then((module) => module || null)
      .catch((error) => {
        console.error('Failed to load /wails/runtime.js:', error);
        return null;
      });
  }
  return runtimePromise;
}

async function callByID(methodID, ...args) {
  // Hard bridge boundary:
  // If Wails runtime is unavailable, we fail fast instead of silently
  // falling back to browser-style system APIs.
  const runtime = await waitRuntime();
  if (!runtime?.Call?.ByID) {
    throw new Error('Wails runtime bridge not ready');
  }
  return runtime.Call.ByID(methodID, ...args);
}

export async function callAPI(method, params = {}) {
  const raw = await callByID(METHOD_IDS.CALL_API, method, JSON.stringify(params || {}));
  return parseMaybeJSON(raw);
}

export async function selectProjectDir() {
  // Project directory chooser must be handled by Go/Wails native dialog.
  const value = await callByID(METHOD_IDS.SELECT_PROJECT_DIR);
  return typeof value === 'string' ? value : '';
}

export async function selectFiles() {
  // Attachment file chooser must be handled by Go/Wails native dialog.
  const values = await callByID(METHOD_IDS.SELECT_FILES);
  return Array.isArray(values) ? values : [];
}

export async function saveClipboardImage(base64Payload) {
  return (await callByID(METHOD_IDS.SAVE_CLIPBOARD_IMAGE, base64Payload)) || '';
}

export async function getBuildInfo() {
  const raw = await callByID(METHOD_IDS.GET_BUILD_INFO);
  return parseMaybeJSON(raw);
}

export function onAgentEvent(callback) {
  let off = () => {};
  waitRuntime().then((runtime) => {
    if (!runtime?.Events?.On) return;
    const unbind = runtime.Events.On('agent-event', callback);
    if (typeof unbind === 'function') {
      off = unbind;
      return;
    }
    off = () => {
      try {
        runtime.Events.Off('agent-event');
      } catch {
        // ignore
      }
    };
  });
  return () => off();
}

export function onBridgeEvent(callback) {
  let off = () => {};
  waitRuntime().then((runtime) => {
    if (!runtime?.Events?.On) return;
    const unbind = runtime.Events.On('bridge-event', callback);
    if (typeof unbind === 'function') {
      off = unbind;
      return;
    }
    off = () => {
      try {
        runtime.Events.Off('bridge-event');
      } catch {
        // ignore
      }
    };
  });
  return () => off();
}

export async function getGroup() {
  const value = await callByID(METHOD_IDS.GET_GROUP);
  return typeof value === 'string' ? value : '';
}
