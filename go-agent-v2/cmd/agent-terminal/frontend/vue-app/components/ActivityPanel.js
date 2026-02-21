import { computed, ref } from '../../lib/vue.esm-browser.prod.js';

/**
 * ActivityPanel — 右下角活动面板，与 ComposerBar 等高。
 *
 * 两个区域:
 *   - 行为记录 (stats): LSP / 命令 / 文件 / 工具 的累计调用次数
 *   - 告警日志 (alerts): 只显示高优先级事件 (error / stall / abort)
 */
export const ActivityPanel = {
  name: 'ActivityPanel',
  props: {
    /** @type {{ lspCalls: number, commands: number, fileEdits: number, toolCalls: Record<string,number> }} */
    stats: { type: Object, default: () => ({}) },
    /** @type {Array<{ id: string, time: string, level: string, message: string }>} */
    alerts: { type: Array, default: () => [] },
    /** @type {Array<{ id: string, time: string, message: string, status: string, kind?: string, title?: string, command?: string, output?: string, exitCode?: number, multiline?: boolean }>} */
    processEvents: { type: Array, default: () => [] },
  },
  setup(props) {
    const expanded = ref(false);

    const lspCount = computed(() => Number(props.stats?.lspCalls) || 0);
    const cmdCount = computed(() => Number(props.stats?.commands) || 0);
    const fileCount = computed(() => Number(props.stats?.fileEdits) || 0);
    const toolCallMap = computed(() => props.stats?.toolCalls || {});
    const totalTools = computed(() => {
      const map = toolCallMap.value;
      let sum = 0;
      for (const [, v] of Object.entries(map)) {
        sum += Number(v) || 0;
      }
      return sum;
    });

    const toolCallEntries = computed(() => {
      const map = toolCallMap.value;
      const entries = [];
      for (const [key, v] of Object.entries(map)) {
        const count = Number(v) || 0;
        if (count > 0) {
          entries.push({ name: key, count });
        }
      }
      entries.sort((a, b) => b.count - a.count);
      return entries;
    });

    const recentAlerts = computed(() => {
      const items = props.alerts || [];
      return items.slice(-5).reverse();
    });

    const recentProcessEvents = computed(() => {
      const items = Array.isArray(props.processEvents) ? props.processEvents : [];
      return items.slice(0, 12);
    });

    const hasAlerts = computed(() => recentAlerts.value.length > 0);
    const hasProcessEvents = computed(() => recentProcessEvents.value.length > 0);

    function toggleExpand() {
      expanded.value = !expanded.value;
    }

    function alertIcon(level) {
      if (level === 'error') return '✗';
      if (level === 'warning' || level === 'stall') return '⚠';
      return '●';
    }

    function alertClass(level) {
      if (level === 'error') return 'alert-error';
      if (level === 'warning' || level === 'stall') return 'alert-warning';
      return 'alert-info';
    }

    function processIcon(status) {
      if (status === 'done') return '✓';
      if (status === 'active') return '●';
      return '•';
    }

    function processClass(status) {
      if (status === 'done') return 'alert-info';
      if (status === 'failed') return 'alert-error';
      return 'alert-warning';
    }

    function isCommandEntry(entry) {
      const kind = (entry?.kind || '').toString().trim();
      if (kind === 'command') return true;
      if ((entry?.command || '').toString().trim()) return true;
      if ((entry?.output || '').toString().trim()) return true;
      if (Number.isFinite(Number(entry?.exitCode))) return true;
      const msg = (entry?.message || '').toString();
      if (!msg) return false;
      if (msg.includes('命令执行')) return true;
      if (msg.includes('Running command') || msg.includes('Ran command') || msg.includes('Errored command')) return true;
      if (msg.includes('\n$ ') || msg.startsWith('$ ')) return true;
      return false;
    }

    function commandStatusText(entry) {
      const status = (entry?.status || '').toString().trim();
      if (status === 'active') return 'Running command';
      if (status === 'failed') return 'Errored command';
      const msg = (entry?.message || '').toString();
      if (msg.includes('命令执行中') || msg.includes('Running command')) return 'Running command';
      if (msg.includes('执行失败') || msg.includes('Errored command')) return 'Errored command';
      return 'Ran command';
    }

    function commandStatusIcon(entry) {
      const status = (entry?.status || '').toString().trim();
      if (status === 'active') return '◌';
      if (status === 'failed') return '✕';
      return '✓';
    }

    function commandStatusIconClass(entry) {
      const status = (entry?.status || '').toString().trim();
      if (status === 'active') return 'ran-command-card__icon--running ran-command-card__icon--spinning';
      if (status === 'failed') return 'ran-command-card__icon--error';
      return 'ran-command-card__icon--done';
    }

    function commandTitle(entry) {
      const title = (entry?.title || entry?.message || '').toString().trim();
      if (title.startsWith('$ ')) return title;
      const command = (entry?.command || '').toString().trim();
      if (command) return `$ ${command}`;
      if (title.includes('\n')) {
        const lines = title.split('\n').map((line) => line.trim()).filter(Boolean);
        const candidate = lines.find((line) => line.startsWith('$ '));
        if (candidate) return candidate;
      }
      if (title && !title.includes('命令执行')) return title;
      return 'Terminal command';
    }

    function commandOutput(entry) {
      const output = (entry?.output || '').toString();
      if (output.trim()) return output;
      const msg = (entry?.message || '').toString();
      if (!msg.includes('\n')) return '';
      const lines = msg.split('\n').map((line) => line.toString());
      if (lines.length <= 1) return '';
      const payload = lines.slice(1).filter((line) => !line.trim().startsWith('$ ')).join('\n').trim();
      return payload;
    }

    function commandExitText(entry) {
      const code = Number(entry?.exitCode);
      if (!Number.isFinite(code)) return '';
      return `Exit code ${Math.trunc(code)}`;
    }

    return {
      expanded,
      lspCount,
      cmdCount,
      fileCount,
      totalTools,
      toolCallEntries,
      recentAlerts,
      recentProcessEvents,
      hasAlerts,
      hasProcessEvents,
      toggleExpand,
      alertIcon,
      alertClass,
      processIcon,
      processClass,
      isCommandEntry,
      commandStatusText,
      commandStatusIcon,
      commandStatusIconClass,
      commandTitle,
      commandOutput,
      commandExitText,
    };
  },
  template: `
    <div class="activity-panel" :class="{ expanded }">
      <div class="activity-stats" @click="toggleExpand" title="点击展开工具详情">
        <span class="stat stat-lsp">LSP:<strong>{{ lspCount }}</strong></span>
        <span class="stat-sep">·</span>
        <span class="stat stat-cmd">命令:<strong>{{ cmdCount }}</strong></span>
        <span class="stat-sep">·</span>
        <span class="stat stat-file">文件:<strong>{{ fileCount }}</strong></span>
        <span class="stat-sep">·</span>
        <span class="stat stat-tool">工具:<strong>{{ totalTools }}</strong></span>
      </div>

      <div v-if="expanded && toolCallEntries.length > 0" class="activity-tool-detail">
        <span
          v-for="entry in toolCallEntries"
          :key="entry.name"
          class="tool-entry"
        >{{ entry.name }}:<strong>{{ entry.count }}</strong></span>
      </div>

      <div class="activity-alerts" :class="{ empty: !hasAlerts && !hasProcessEvents }">
        <template v-if="hasProcessEvents">
          <div
            v-for="(entry, idx) in recentProcessEvents"
            :key="entry.id + '-' + idx"
            class="alert-line"
            :class="[processClass(entry.status), { 'alert-line-command': isCommandEntry(entry) }]"
          >
            <span class="alert-time">{{ entry.time }}</span>
            <template v-if="isCommandEntry(entry)">
              <div class="activity-command-entry ran-command-card">
                <div class="ran-command-card__header">
                  <span class="ran-command-card__status">{{ commandStatusText(entry) }}</span>
                </div>
                <div class="ran-command-card__main-row">
                  <span class="ran-command-card__icon" :class="commandStatusIconClass(entry)" aria-hidden="true">{{ commandStatusIcon(entry) }}</span>
                  <span class="ran-command-card__title" :title="commandTitle(entry)">{{ commandTitle(entry) }}</span>
                </div>
                <div
                  class="ran-command-card__details"
                  :class="commandOutput(entry) ? 'ran-command-card__details--open' : 'ran-command-card__details--closed'"
                >
                  <pre v-if="commandOutput(entry)" class="ran-command-card__output activity-command-output">{{ commandOutput(entry) }}</pre>
                </div>
                <div class="ran-command-card__footer">
                  <span class="ran-command-card__auto-exec">Terminal command</span>
                  <div class="ran-command-card__footer-right">
                    <span v-if="entry.status === 'active'" class="ran-command-card__cancel-btn">Running...</span>
                    <span v-if="commandExitText(entry)" class="ran-command-card__exit-code">{{ commandExitText(entry) }}</span>
                  </div>
                </div>
              </div>
            </template>
            <template v-else>
              <span class="alert-icon">{{ processIcon(entry.status) }}</span>
              <span class="alert-msg" :class="{ 'alert-msg-wrap': Boolean(entry.multiline) }">{{ entry.message }}</span>
            </template>
          </div>
        </template>
        <template v-if="hasAlerts">
          <div
            v-for="alert in recentAlerts"
            :key="alert.id"
            class="alert-line"
            :class="alertClass(alert.level)"
          >
            <span class="alert-time">{{ alert.time }}</span>
            <span class="alert-icon">{{ alertIcon(alert.level) }}</span>
            <span class="alert-msg">{{ alert.message }}</span>
          </div>
        </template>
        <div v-if="!hasAlerts && !hasProcessEvents" class="alert-empty">无告警</div>
      </div>
    </div>
  `,
};
