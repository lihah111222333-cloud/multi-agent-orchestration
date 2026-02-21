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
    /** @type {Array<{ id: string, time: string, message: string, status: string }>} */
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
      return 'alert-warning';
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
            :class="processClass(entry.status)"
          >
            <span class="alert-time">{{ entry.time }}</span>
            <span class="alert-icon">{{ processIcon(entry.status) }}</span>
            <span class="alert-msg">{{ entry.message }}</span>
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
