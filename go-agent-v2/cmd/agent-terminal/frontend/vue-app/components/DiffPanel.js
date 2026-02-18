import { computed, watch } from '../../lib/vue.esm-browser.prod.js';
import { parseUnifiedDiff, diffStats } from '../services/diff.js';
import { logDebug } from '../services/log.js';

export const DiffPanel = {
  name: 'DiffPanel',
  props: {
    diffText: { type: String, default: '' },
  },
  setup(props) {
    const files = computed(() => parseUnifiedDiff(props.diffText));
    const fileCountText = computed(() => `${files.value.length} file${files.value.length === 1 ? '' : 's'}`);
    const totals = computed(() => files.value.reduce(
      (acc, file) => {
        const stats = diffStats(file);
        acc.add += stats.add;
        acc.del += stats.del;
        return acc;
      },
      { add: 0, del: 0 },
    ));
    watch(
      () => props.diffText,
      (next, prev) => {
        if (next === prev) return;
        logDebug('ui', 'diffPanel.updated', {
          text_len: (next || '').length,
          files: files.value.length,
        });
      },
      { immediate: true },
    );

    function linePrefix(type) {
      if (type === 'add') return '+';
      if (type === 'del') return '-';
      if (type === 'hunk') return '@';
      if (type === 'meta') return '·';
      return ' ';
    }

    return {
      files,
      fileCountText,
      totals,
      diffStats,
      linePrefix,
    };
  },
  template: `
    <div id="diff-panel">
      <div class="diff-header">
        <div class="diff-header-main">
          <strong>代码变更</strong>
          <small>{{ fileCountText }}</small>
        </div>
        <div class="diff-header-metrics">
          <span class="diff-metric add">+{{ totals.add }}</span>
          <span class="diff-metric del">-{{ totals.del }}</span>
        </div>
      </div>

      <div id="diff-content">
        <div v-if="files.length === 0" class="diff-empty">暂无代码变更</div>

        <div v-for="file in files" :key="file.filename" class="diff-file-group">
          <div class="diff-file-header">
            <div class="diff-file-title">
              <span class="diff-file-caret">▾</span>
              <span class="diff-file-name">{{ file.filename }}</span>
            </div>
            <div class="diff-file-stats">
              <span class="diff-metric add">+{{ diffStats(file).add }}</span>
              <span class="diff-metric del">-{{ diffStats(file).del }}</span>
            </div>
          </div>
          <div class="diff-file-lines">
            <div v-for="(line, idx) in file.lines" :key="idx" class="diff-line" :class="line.type">
              <span class="diff-line-num old">{{ line.oldNo }}</span>
              <span class="diff-line-num new">{{ line.newNo }}</span>
              <span class="diff-line-prefix">{{ linePrefix(line.type) }}</span>
              <span class="diff-line-content">{{ line.text }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  `,
};
