// Bridge Policy (must keep):
// - This file is the only frontend bridge for desktop capabilities.
// - Vue/JS is UI-only; system capabilities must go through Wails v3 runtime bridge.
// - Do not introduce browser-native fallbacks for file/system access.
import { logDebug, logInfo, logWarn, logError } from './log.js';

const METHOD_IDS = Object.freeze({
  CALL_API: 1055257995,
  GET_BUILD_INFO: 3168473285,
  GET_GROUP: 4127719990,
  SAVE_CLIPBOARD_IMAGE: 3932748547,
  SELECT_FILES: 937743440,
  SELECT_PROJECT_DIR: 373469749,
});

const EVENT_SAMPLE_EVERY = 120;

let bridgeRequestSeq = 0;
let rpcRequestSeq = 0;
let agentEventCount = 0;
let bridgeEventCount = 0;

function perfNow() {
  if (typeof performance !== 'undefined' && typeof performance.now === 'function') {
    return performance.now();
  }
  return Date.now();
}

function parseMaybeJSON(raw) {
  if (raw == null || raw === '') return {};
  if (typeof raw === 'object') return raw;
  try {
    return JSON.parse(raw);
  } catch (error) {
    logWarn('api', 'json.parse.failed', {
      error,
      raw_len: typeof raw === 'string' ? raw.length : 0,
      raw_preview: typeof raw === 'string' ? raw.slice(0, 200) : '',
    });
    return {};
  }
}

export function normalizeRuntimeEventEnvelope(evt) {
  if (!evt || typeof evt !== 'object') return {};

  const hasWailsEnvelope = Object.prototype.hasOwnProperty.call(evt, 'name')
    && Object.prototype.hasOwnProperty.call(evt, 'data');
  if (!hasWailsEnvelope) {
    return evt;
  }

  const inner = evt.data;
  if (inner == null || inner === '') return {};
  if (typeof inner === 'object') return inner;
  if (typeof inner === 'string') return parseMaybeJSON(inner);
  return { data: inner };
}

let runtimePromise = null;

async function waitRuntime() {
  if (!runtimePromise) {
    logInfo('bridge', 'runtime.load.start', {});
    runtimePromise = import('/wails/runtime.js')
      .then((module) => {
        logInfo('bridge', 'runtime.load.done', {
          ready: Boolean(module?.Call?.ByID),
          has_events: Boolean(module?.Events?.On),
        });
        return module || null;
      })
      .catch((error) => {
        logError('bridge', 'runtime.load.failed', { error });
        return null;
      });
  }
  return runtimePromise;
}

async function callByID(methodID, ...args) {
  // Hard bridge boundary:
  // If Wails runtime is unavailable, we fail fast instead of silently
  // falling back to browser-style system APIs.
  const reqId = ++bridgeRequestSeq;
  const start = perfNow();
  logDebug('bridge', 'call.start', {
    req_id: reqId,
    method_id: methodID,
    arg_count: args.length,
  });

  const runtime = await waitRuntime();
  if (!runtime?.Call?.ByID) {
    logWarn('bridge', 'call.runtime.unavailable', {
      req_id: reqId,
      method_id: methodID,
    });
    throw new Error('Wails runtime bridge not ready');
  }
  try {
    const result = await runtime.Call.ByID(methodID, ...args);
    logDebug('bridge', 'call.done', {
      req_id: reqId,
      method_id: methodID,
      duration_ms: Math.round(perfNow() - start),
    });
    return result;
  } catch (error) {
    logWarn('bridge', 'call.failed', {
      req_id: reqId,
      method_id: methodID,
      duration_ms: Math.round(perfNow() - start),
      error,
    });
    throw error;
  }
}

export async function callAPI(method, params = {}) {
  const reqId = ++rpcRequestSeq;
  const start = perfNow();
  const payload = params || {};
  logDebug('api', 'rpc.start', {
    req_id: reqId,
    method,
    param_keys: Object.keys(payload),
  });
  try {
    const raw = await callByID(METHOD_IDS.CALL_API, method, JSON.stringify(payload));
    const result = parseMaybeJSON(raw);
    logDebug('api', 'rpc.done', {
      req_id: reqId,
      method,
      duration_ms: Math.round(perfNow() - start),
    });
    return result;
  } catch (error) {
    logWarn('api', 'rpc.failed', {
      req_id: reqId,
      method,
      duration_ms: Math.round(perfNow() - start),
      error,
    });
    throw error;
  }
}

export async function selectProjectDir() {
  // Project directory chooser must be handled by Go/Wails native dialog.
  logInfo('ui', 'selectProjectDir.start', {});
  const value = await callByID(METHOD_IDS.SELECT_PROJECT_DIR);
  const path = typeof value === 'string' ? value : '';
  logInfo('ui', 'selectProjectDir.done', { selected: Boolean(path), path });
  return path;
}

export async function selectFiles() {
  // Attachment file chooser must be handled by Go/Wails native dialog.
  logInfo('ui', 'selectFiles.start', {});
  const values = await callByID(METHOD_IDS.SELECT_FILES);
  const files = Array.isArray(values) ? values : [];
  logInfo('ui', 'selectFiles.done', {
    count: files.length,
    first: files[0] || '',
  });
  return files;
}

export async function saveClipboardImage(base64Payload) {
  const start = perfNow();
  const path = (await callByID(METHOD_IDS.SAVE_CLIPBOARD_IMAGE, base64Payload)) || '';
  logDebug('ui', 'clipboardImage.saved', {
    ok: Boolean(path),
    duration_ms: Math.round(perfNow() - start),
  });
  return path;
}

export async function getBuildInfo() {
  const raw = await callByID(METHOD_IDS.GET_BUILD_INFO);
  const info = parseMaybeJSON(raw);
  logDebug('api', 'buildInfo.read', {
    version: info?.version || '',
    commit: info?.commit || '',
  });
  return info;
}

export function onAgentEvent(callback) {
  let off = () => {};
  const wrapped = (evt) => {
    const normalized = normalizeRuntimeEventEnvelope(evt);
    agentEventCount += 1;
    if (agentEventCount % EVENT_SAMPLE_EVERY === 0) {
      logDebug('event', 'agent.sample', {
        count: agentEventCount,
        type: (normalized?.type || '').toString(),
      });
    }
    try {
      callback(normalized);
    } catch (error) {
      logError('event', 'agent.callback.failed', { error });
    }
  };
  waitRuntime().then((runtime) => {
    if (!runtime?.Events?.On) {
      logWarn('event', 'agent.subscribe.unavailable', {});
      return;
    }
    const unbind = runtime.Events.On('agent-event', wrapped);
    logInfo('event', 'agent.subscribe.ready', {});
    if (typeof unbind === 'function') {
      off = unbind;
      return;
    }
    off = () => {
      try {
        runtime.Events.Off('agent-event');
        logInfo('event', 'agent.unsubscribe.done', {});
      } catch {
        // ignore
      }
    };
  });
  return () => off();
}

export function onBridgeEvent(callback) {
  let off = () => {};
  const wrapped = (evt) => {
    const normalized = normalizeRuntimeEventEnvelope(evt);
    bridgeEventCount += 1;
    if (bridgeEventCount % EVENT_SAMPLE_EVERY === 0) {
      logDebug('event', 'bridge.sample', {
        count: bridgeEventCount,
        type: (normalized?.type || normalized?.method || '').toString(),
      });
    }
    try {
      callback(normalized);
    } catch (error) {
      logError('event', 'bridge.callback.failed', { error });
    }
  };
  waitRuntime().then((runtime) => {
    if (!runtime?.Events?.On) {
      logWarn('event', 'bridge.subscribe.unavailable', {});
      return;
    }
    const unbind = runtime.Events.On('bridge-event', wrapped);
    logInfo('event', 'bridge.subscribe.ready', {});
    if (typeof unbind === 'function') {
      off = unbind;
      return;
    }
    off = () => {
      try {
        runtime.Events.Off('bridge-event');
        logInfo('event', 'bridge.unsubscribe.done', {});
      } catch {
        // ignore
      }
    };
  });
  return () => off();
}

export async function getGroup() {
  const value = await callByID(METHOD_IDS.GET_GROUP);
  const group = typeof value === 'string' ? value : '';
  logDebug('api', 'group.read', { group });
  return group;
}
