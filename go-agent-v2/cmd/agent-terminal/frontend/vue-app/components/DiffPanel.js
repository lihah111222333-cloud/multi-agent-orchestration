import { computed } from '../../lib/vue.esm-browser.prod.js';
import { parseUnifiedDiff, diffStats } from '../services/diff.js';

export const DiffPanel = {
  name: 'DiffPanel',
  props: {
    diffText: { type: String, default: '' },
  },
  setup(props) {
    const files = computed(() => parseUnifiedDiff(props.diffText));
    const fileCountText = computed(() => `${files.value.length} file${files.value.length === 1 ? '' : 's'}`);
    return {
      files,
      fileCountText,
      diffStats,
    };
  },
  template: `
    <div id="diff-panel">
      <div class="diff-header">
        <span>代码变更</span>
        <span class="badge badge-muted">{{ fileCountText }}</span>
      </div>

      <div id="diff-content">
        <div v-if="files.length === 0" class="diff-empty">No staged changes</div>

        <div v-for="file in files" :key="file.filename" class="diff-file-group">
          <div class="diff-file-header">
            <span>▾</span>
            <span>{{ file.filename }}</span>
            <span style="margin-left:auto;font-size:10px;color:var(--text-muted)">
              +{{ diffStats(file).add }} -{{ diffStats(file).del }}
            </span>
          </div>
          <div class="diff-file-lines">
            <div v-for="(line, idx) in file.lines" :key="idx" class="diff-line" :class="line.type">
              <span class="diff-line-num">{{ line.type === 'del' ? '' : idx + 1 }}</span>
              <span class="diff-line-content">{{ line.text }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  `,
};
