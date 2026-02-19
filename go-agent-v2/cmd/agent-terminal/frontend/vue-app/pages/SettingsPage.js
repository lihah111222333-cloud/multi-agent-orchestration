import { computed, onBeforeUnmount, onMounted, ref } from '../../lib/vue.esm-browser.prod.js';
import { logInfo, readLogBuffer, readLogLevel } from '../services/log.js';

export const SettingsPage = {
  name: 'SettingsPage',
  props: {
    buildInfo: { type: Object, required: true },
  },
  emits: ['refresh'],
  setup(props, { emit }) {
    const LOG_LIST_LIMIT = 14;
    const versionText = computed(() => `Agent Orchestrator ${props.buildInfo.version || 'dev'}`);
    const runtimeText = computed(() => props.buildInfo.runtime
      ? `Wails WebKit · Go Backend · ${props.buildInfo.runtime}`
      : 'Wails WebKit · Go Backend');
    const buildTimeText = computed(() => props.buildInfo.buildTime || '-');
    const commitText = computed(() => props.buildInfo.commit || '-');
    const logLevel = ref('info');
    const logEntries = ref([]);
    let logRefreshTimer = 0;

    function formatLogTime(value) {
      if (!value) return '--:--:--';
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return '--:--:--';
      return date.toLocaleTimeString('zh-CN', { hour12: false });
    }

    function refreshLogPanel() {
      logLevel.value = readLogLevel();
      const buffer = readLogBuffer();
      logEntries.value = buffer.slice(-LOG_LIST_LIMIT).reverse();
    }

    const refresh = () => {
      logInfo('page', 'settings.refreshBuildInfo.click', {});
      emit('refresh');
    };

    onMounted(() => {
      logInfo('page', 'settings.mounted', {});
      refreshLogPanel();
      logRefreshTimer = window.setInterval(refreshLogPanel, 1000);
    });
    onBeforeUnmount(() => {
      if (logRefreshTimer) {
        window.clearInterval(logRefreshTimer);
      }
      logInfo('page', 'settings.unmounted', {});
    });

    return {
      versionText,
      runtimeText,
      buildTimeText,
      commitText,
      logLevel,
      logEntries,
      refresh,
      refreshLogPanel,
      formatLogTime,
    };
  },
  template: `
    <section id="page-settings" class="page active">
      <div class="panel-header">
        <div class="ph-bar"></div>
        <div class="ph-text">
          <h2>设置</h2>
        </div>
      </div>

      <div class="panel-body">
        <div class="section-header">ABOUT</div>
        <div class="data-card-vue">
          <div class="data-row-vue"><strong>版本</strong><span>{{ versionText }}</span></div>
          <div class="data-row-vue"><strong>运行时</strong><span>{{ runtimeText }}</span></div>
          <div class="data-row-vue"><strong>构建时间</strong><span>{{ buildTimeText }}</span></div>
          <div class="data-row-vue"><strong>Commit</strong><span>{{ commitText }}</span></div>
        </div>
        <div class="settings-action-row">
          <button class="btn btn-secondary" @click="refresh">刷新构建信息</button>
        </div>

        <div class="section-header">UI LOG</div>
        <div class="data-card-vue settings-log-card">
          <div class="data-row-vue">
            <strong>日志级别</strong>
            <span>{{ logLevel }}</span>
          </div>
          <div class="settings-action-row">
            <button class="btn btn-secondary btn-toolbar-sm" @click="refreshLogPanel">刷新日志</button>
          </div>
          <div v-if="logEntries.length === 0" class="settings-log-empty">暂无日志</div>
          <div v-else class="settings-log-list">
            <div
              v-for="entry in logEntries"
              :key="entry.seq"
              class="settings-log-item"
            >
              <span class="settings-log-time">{{ formatLogTime(entry.ts) }}</span>
              <span class="settings-log-level" :class="'is-' + entry.level">{{ entry.level }}</span>
              <span class="settings-log-event">{{ entry.scope }}.{{ entry.event }}</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  `,
};
