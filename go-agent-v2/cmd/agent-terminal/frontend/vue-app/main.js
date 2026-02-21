import { createApp } from '../lib/vue.esm-browser.prod.js';
import { AppRoot } from './app.js';
import { logError, logInfo } from './services/log.js';

try {
  logInfo('app', 'mount.start', {});
  createApp(AppRoot).mount('#app');
  logInfo('app', 'mount.done', {});
} catch (error) {
  logError('app', 'mount.failed', { error });
  throw error;
}
