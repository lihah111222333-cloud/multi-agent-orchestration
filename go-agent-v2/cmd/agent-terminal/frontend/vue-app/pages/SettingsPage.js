import { computed } from '../../lib/vue.esm-browser.prod.js';

export const SettingsPage = {
  name: 'SettingsPage',
  props: {
    buildInfo: { type: Object, required: true },
    refreshBuildInfo: { type: Function, required: true },
  },
  setup(props) {
    const versionText = computed(() => `Agent Orchestrator ${props.buildInfo.version || 'dev'}`);
    const runtimeText = computed(() => props.buildInfo.runtime
      ? `Wails WebKit · Go Backend · ${props.buildInfo.runtime}`
      : 'Wails WebKit · Go Backend');
    const buildTimeText = computed(() => props.buildInfo.buildTime || '-');
    const commitText = computed(() => props.buildInfo.commit || '-');

    return {
      versionText,
      runtimeText,
      buildTimeText,
      commitText,
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
        <div style="margin-top:12px">
          <button class="btn btn-secondary" @click="refreshBuildInfo">刷新构建信息</button>
        </div>
      </div>
    </section>
  `,
};
