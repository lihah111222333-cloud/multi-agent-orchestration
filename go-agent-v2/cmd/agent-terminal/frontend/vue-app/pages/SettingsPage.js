import { computed, onBeforeUnmount, onMounted, reactive, ref } from '../../lib/vue.esm-browser.prod.js';
import { callAPI } from '../services/api.js';
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
    const lspPromptHint = ref('');
    const lspPromptDefaultHint = ref('');
    const lspPromptLoading = ref(false);
    const lspPromptSaving = ref(false);
    const lspPromptNotice = reactive({ level: 'info', message: '' });

    let logRefreshTimer = 0;

    // Turn Tracker 设置
    const stallThreshold = ref(480);
    const stallHeartbeat = ref(300);
    const stallLoading = ref(false);
    const stallNotice = reactive({ level: 'info', message: '' });

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

    function setLSPPromptNotice(level, message) {
      lspPromptNotice.level = level || 'info';
      lspPromptNotice.message = (message || '').toString().trim();
    }

    function setStallNotice(level, message) {
      stallNotice.level = level || 'info';
      stallNotice.message = (message || '').toString().trim();
    }

    async function loadLSPPromptHint() {
      lspPromptLoading.value = true;
      try {
        const res = await callAPI('config/lspPromptHint/read', {});
        const hint = (res?.hint || '').toString();
        const defaultHint = (res?.defaultHint || '').toString();
        lspPromptHint.value = hint;
        lspPromptDefaultHint.value = defaultHint;
        setLSPPromptNotice('info', '');
      } catch (error) {
        setLSPPromptNotice('error', `加载失败：${error?.message || error}`);
      } finally {
        lspPromptLoading.value = false;
      }
    }

    async function saveLSPPromptHint() {
      if (lspPromptSaving.value) return;
      lspPromptSaving.value = true;
      try {
        const res = await callAPI('config/lspPromptHint/write', {
          hint: lspPromptHint.value,
        });
        lspPromptHint.value = (res?.hint || '').toString();
        if (res?.usingDefault) {
          setLSPPromptNotice('info', '已恢复默认提示词');
        } else {
          setLSPPromptNotice('info', '提示词已保存');
        }
      } catch (error) {
        setLSPPromptNotice('error', `保存失败：${error?.message || error}`);
      } finally {
        lspPromptSaving.value = false;
      }
    }

    async function resetLSPPromptHint() {
      if (lspPromptSaving.value) return;
      lspPromptHint.value = '';
      await saveLSPPromptHint();
    }

    // Turn Tracker: 加载
    async function loadStallSettings() {
      stallLoading.value = true;
      try {
        const [thresholdRes, heartbeatRes] = await Promise.all([
          callAPI('ui/preferences/get', { key: 'stallThresholdSec' }).catch(() => null),
          callAPI('ui/preferences/get', { key: 'stallHeartbeatSec' }).catch(() => null),
        ]);
        if (thresholdRes != null && typeof thresholdRes === 'number') stallThreshold.value = thresholdRes;
        if (heartbeatRes != null && typeof heartbeatRes === 'number') stallHeartbeat.value = heartbeatRes;
        setStallNotice('info', '');
      } catch (error) {
        setStallNotice('error', `加载失败：${error?.message || error}`);
      } finally {
        stallLoading.value = false;
      }
    }

    // Turn Tracker: 保存单个
    async function saveStallSetting(key, value, label) {
      const num = parseInt(value, 10);
      if (Number.isNaN(num) || num < 10) {
        setStallNotice('error', `${label}不能小于 10 秒`);
        return;
      }
      try {
        await callAPI('ui/preferences/set', { key, value: num });
        setStallNotice('info', `${label}已保存: ${num}s (${Math.round(num / 60)}分钟)`);
      } catch (error) {
        setStallNotice('error', `保存失败：${error?.message || error}`);
      }
    }

    async function saveStallThreshold() {
      await saveStallSetting('stallThresholdSec', stallThreshold.value, 'Stall 阈值');
    }
    async function saveStallHeartbeat() {
      await saveStallSetting('stallHeartbeatSec', stallHeartbeat.value, '心跳间隔');
    }

    const refresh = () => {
      logInfo('page', 'settings.refreshBuildInfo.click', {});
      emit('refresh');
    };

    onMounted(() => {
      logInfo('page', 'settings.mounted', {});
      refreshLogPanel();
      loadLSPPromptHint();
      loadStallSettings();
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
      lspPromptHint,
      lspPromptDefaultHint,
      lspPromptLoading,
      lspPromptSaving,
      lspPromptNotice,
      refresh,
      refreshLogPanel,
      loadLSPPromptHint,
      saveLSPPromptHint,
      resetLSPPromptHint,
      formatLogTime,
      stallThreshold,
      stallHeartbeat,
      stallLoading,
      stallNotice,
      loadStallSettings,
      saveStallThreshold,
      saveStallHeartbeat,
    };
  },
  template: `
    <section id="page-settings" class="page active" data-testid="settings-page">
      <div class="panel-header">
        <div class="ph-bar"></div>
        <div class="ph-text">
          <h2>设置</h2>
        </div>
      </div>

      <div class="panel-body" data-testid="settings-panel-body">
        <div class="section-header">ABOUT</div>
        <div class="data-card-vue" data-testid="settings-about-card">
          <div class="data-row-vue"><strong>版本</strong><span>{{ versionText }}</span></div>
          <div class="data-row-vue"><strong>运行时</strong><span>{{ runtimeText }}</span></div>
          <div class="data-row-vue"><strong>构建时间</strong><span>{{ buildTimeText }}</span></div>
          <div class="data-row-vue"><strong>Commit</strong><span>{{ commitText }}</span></div>
        </div>
        <div class="settings-action-row">
          <button class="btn btn-secondary" data-testid="settings-refresh-build-button" @click="refresh">刷新构建信息</button>
        </div>

        <div class="section-header">TURN TRACKER</div>
        <div class="data-card-vue settings-stall-card" data-testid="settings-stall-card">
          <div class="data-row-vue">
            <strong>Stall 检测阈值</strong>
            <span>无事件超过此时间自动中断 turn</span>
          </div>
          <div class="settings-stall-row">
            <input
              type="number"
              class="settings-stall-input"
              data-testid="settings-stall-threshold-input"
              v-model.number="stallThreshold"
              min="30"
              step="30"
              :disabled="stallLoading"
            />
            <span class="settings-stall-unit">秒 ({{ Math.round(stallThreshold / 60) }} 分钟)</span>
            <button class="btn btn-primary btn-toolbar-sm" data-testid="settings-stall-threshold-save-button" @click="saveStallThreshold" :disabled="stallLoading">保存</button>
          </div>
          <div class="data-row-vue" style="margin-top:12px">
            <strong>心跳保活间隔</strong>
            <span>等待工具 / 审批期间的续命频率</span>
          </div>
          <div class="settings-stall-row">
            <input
              type="number"
              class="settings-stall-input"
              data-testid="settings-stall-heartbeat-input"
              v-model.number="stallHeartbeat"
              min="10"
              step="30"
              :disabled="stallLoading"
            />
            <span class="settings-stall-unit">秒 ({{ Math.round(stallHeartbeat / 60) }} 分钟)</span>
            <button class="btn btn-primary btn-toolbar-sm" data-testid="settings-stall-heartbeat-save-button" @click="saveStallHeartbeat" :disabled="stallLoading">保存</button>
          </div>
          <div v-if="stallNotice.message" class="settings-prompt-notice" data-testid="settings-stall-notice" :class="'is-' + stallNotice.level">
            {{ stallNotice.message }}
          </div>
        </div>

        <div class="section-header">PROMPT</div>
        <div class="data-card-vue settings-prompt-card" data-testid="settings-lsp-prompt-card">
          <div class="data-row-vue">
            <strong>Playwright/json-render /LSP 系统提示词注入</strong>
            <span>{{ lspPromptLoading ? '加载中...' : '已启用' }}</span>
          </div>
          <div class="settings-prompt-desc">单一注入点。留空并保存可恢复默认值。</div>
          <textarea
            class="settings-prompt-textarea"
            data-testid="settings-lsp-prompt-input"
            rows="6"
            v-model="lspPromptHint"
            :placeholder="lspPromptDefaultHint || '请输入提示词'"
            :disabled="lspPromptLoading || lspPromptSaving"
          ></textarea>
          <div v-if="lspPromptNotice.message" class="settings-prompt-notice" data-testid="settings-lsp-prompt-notice" :class="'is-' + lspPromptNotice.level">
            {{ lspPromptNotice.message }}
          </div>
          <div class="settings-action-row settings-action-inline">
            <button class="btn btn-secondary btn-toolbar-sm" data-testid="settings-lsp-refresh-button" @click="loadLSPPromptHint" :disabled="lspPromptSaving">刷新</button>
            <button class="btn btn-secondary btn-toolbar-sm" data-testid="settings-lsp-reset-button" @click="resetLSPPromptHint" :disabled="lspPromptLoading || lspPromptSaving">恢复默认</button>
            <button class="btn btn-primary btn-toolbar-sm" data-testid="settings-lsp-save-button" @click="saveLSPPromptHint" :disabled="lspPromptLoading || lspPromptSaving">
              {{ lspPromptSaving ? '保存中...' : '保存提示词' }}
            </button>
          </div>
        </div>

        <div class="section-header">UI LOG</div>
        <div class="data-card-vue settings-log-card" data-testid="settings-log-card">
          <div class="data-row-vue">
            <strong>日志级别</strong>
            <span>{{ logLevel }}</span>
          </div>
          <div class="settings-action-row">
            <button class="btn btn-secondary btn-toolbar-sm" data-testid="settings-log-refresh-button" @click="refreshLogPanel">刷新日志</button>
          </div>
          <div v-if="logEntries.length === 0" class="settings-log-empty" data-testid="settings-log-empty">暂无日志</div>
          <div v-else class="settings-log-list" data-testid="settings-log-list">
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
