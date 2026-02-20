import { computed, ref } from '../../lib/vue.esm-browser.prod.js';

/**
 * ActivityPanel — 右下角活动面板，与 ComposerBar 等高。
 *
 * 三个区域:
 *   - 行为记录 (stats): LSP / 命令 / 文件 / 工具 的累计调用次数
 *   - 命令记录 (commandRecords): 点击展开命令/输出详情
 *   - 告警日志 (alerts): 只显示高优先级事件 (error / stall / abort)
 */
export const ActivityPanel = {
  name: 'ActivityPanel',
  props: {
    /** @type {{ lspCalls: number, commands: number, fileEdits: number, toolCalls: Record<string,number> }} */
    stats: { type: Object, default: () => ({}) },
    /** @type {Array<{ id: string, ts?: string, status?: string, exitCode?: number | null, command?: string, output?: string, outputTruncated?: boolean }>} */
    commandRecords: { type: Array, default: () => [] },
    /** @type {Array<{ id: string, time: string, level: string, message: string }>} */
    alerts: { type: Array, default: () => [] },
  },
  setup(props) {
    const expanded = ref(false);
    const expandedCommandID = ref('');

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

    const recentCommands = computed(() => {
      const items = Array.isArray(props.commandRecords) ? props.commandRecords : [];
      return items.slice(-6).reverse();
    });

    const hasCommands = computed(() => recentCommands.value.length > 0);
    const hasAlerts = computed(() => recentAlerts.value.length > 0);

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

    function formatTime(ts) {
      if (!ts) return '--:--';
      const date = new Date(ts);
      if (Number.isNaN(date.getTime())) return '--:--';
      return date.toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
      });
    }

    function commandStatusLabel(status) {
      const value = (status || '').toString().trim().toLowerCase();
      if (value === 'running') return '执行中';
      if (value === 'failed') return '失败';
      return '完成';
    }

    function commandLineClass(status) {
      const value = (status || '').toString().trim().toLowerCase();
      if (value === 'running') return 'command-running';
      if (value === 'failed') return 'command-failed';
      return 'command-completed';
    }

    function commandMeta(record) {
      if (!record || !Number.isFinite(Number(record.exitCode))) return '';
      return `exit ${Number(record.exitCode)}`;
    }

    function isCommandExpanded(record) {
      const id = (record?.id || '').toString();
      if (!id) return false;
      return expandedCommandID.value === id;
    }

    function toggleCommandRecord(commandID) {
      const id = (commandID || '').toString();
      if (!id) return;
      expandedCommandID.value = expandedCommandID.value === id ? '' : id;
    }

    function commandDetailOutput(record) {
      const output = (record?.output || '').toString();
      if (output) return output;
      return '(无输出)';
    }

    function commandDetailCommand(record) {
      const command = (record?.command || '').toString().trim();
      if (command) return command;
      return '(命令内容不可用)';
    }

    return {
      expanded,
      lspCount,
      cmdCount,
      fileCount,
      totalTools,
      toolCallEntries,
      recentAlerts,
      recentCommands,
      hasCommands,
      hasAlerts,
      toggleExpand,
      alertIcon,
      alertClass,
      formatTime,
      commandStatusLabel,
      commandLineClass,
      commandMeta,
      expandedCommandID,
      isCommandExpanded,
      toggleCommandRecord,
      commandDetailOutput,
      commandDetailCommand,
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

      <div class="activity-command-log" :class="{ empty: !hasCommands }">
        <template v-if="hasCommands">
          <div
            v-for="record in recentCommands"
            :key="record.id"
            class="command-record"
          >
            <button
              type="button"
              class="command-line"
              :class="[commandLineClass(record.status), { expanded: isCommandExpanded(record) }]"
              :title="isCommandExpanded(record) ? '点击收起详细记录' : '点击展开详细记录'"
              :aria-expanded="isCommandExpanded(record)"
              @click="toggleCommandRecord(record.id)"
            >
              <span class="command-time">{{ formatTime(record.ts) }}</span>
              <span class="command-mark"></span>
              <span class="command-status">{{ commandStatusLabel(record.status) }}</span>
              <span v-if="commandMeta(record)" class="command-meta">{{ commandMeta(record) }}</span>
              <span class="command-expand">{{ isCommandExpanded(record) ? '收起' : '详情' }}</span>
            </button>
            <div v-if="isCommandExpanded(record)" class="command-detail">
              <div class="command-detail-label">命令</div>
              <pre class="command-detail-code">$ {{ commandDetailCommand(record) }}</pre>
              <div class="command-detail-label">输出</div>
              <pre class="command-detail-output">{{ commandDetailOutput(record) }}</pre>
              <div v-if="record.outputTruncated" class="command-detail-tip">输出过长，已截断显示</div>
            </div>
          </div>
        </template>
        <div v-else class="command-empty">暂无命令记录</div>
      </div>

      <div class="activity-alerts" :class="{ empty: !hasAlerts }">
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
        <div v-else class="alert-empty">无告警</div>
      </div>
    </div>
  `,
};
