import { createApp } from '../lib/vue.esm-browser.prod.js';
import { AppRoot } from './app.js';
import { logError, logInfo } from './services/log.js';

function hasEcharts() {
  if (typeof globalThis !== 'undefined' && globalThis.echarts) return true;
  if (typeof window !== 'undefined' && window.echarts) return true;
  return false;
}

function loadScriptOnce(src, elementId = '') {
  if (typeof document === 'undefined') return Promise.resolve();
  return new Promise((resolve, reject) => {
    const existing = elementId ? document.getElementById(elementId) : null;
    if (existing) {
      if (existing.dataset.loaded === 'true') {
        resolve();
        return;
      }
      existing.addEventListener('load', () => resolve(), { once: true });
      existing.addEventListener('error', () => reject(new Error(`failed to load script: ${src}`)), { once: true });
      return;
    }
    const script = document.createElement('script');
    script.src = src;
    script.async = true;
    if (elementId) script.id = elementId;
    script.addEventListener('load', () => {
      script.dataset.loaded = 'true';
      resolve();
    }, { once: true });
    script.addEventListener('error', () => reject(new Error(`failed to load script: ${src}`)), { once: true });
    document.head.appendChild(script);
  });
}

async function ensureEchartsLoaded() {
  if (hasEcharts()) return;
  await loadScriptOnce('lib/echarts.min.js', 'echarts-runtime-script');
}

async function bootstrap() {
  try {
    logInfo('app', 'mount.start', {});
    try {
      await ensureEchartsLoaded();
    } catch (error) {
      logError('app', 'echarts.load.failed', { error });
    }
    createApp(AppRoot).mount('#app');
    logInfo('app', 'mount.done', {});
  } catch (error) {
    logError('app', 'mount.failed', { error });
    throw error;
  }
}

void bootstrap();
