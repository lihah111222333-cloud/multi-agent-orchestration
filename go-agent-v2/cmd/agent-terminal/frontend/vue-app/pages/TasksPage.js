import { logDebug } from '../services/log.js';

export const TasksPage = {
    name: 'TasksPage',
    props: {
        tasksSubTab: { type: String, default: 'acks' },
        items: { type: Array, default: () => [] },
        fields: { type: Array, default: () => [] },
    },
    emits: ['update:tasksSubTab'],
    setup(props, { emit }) {
        function setSubTab(tab) {
            logDebug('page', 'tasks.subTab.changed', { tab });
            emit('update:tasksSubTab', tab);
        }

        return {
            setSubTab,
        };
    },
    template: `
    <section id="page-tasks" class="page active">
      <div class="panel-header">
        <div class="ph-bar"></div>
        <div class="ph-text"><h2>任务管理</h2></div>
      </div>
      <div class="sub-tabs">
        <button class="sub-tab" :class="{ active: tasksSubTab === 'acks' }" @click="setSubTab('acks')">任务工单</button>
        <button class="sub-tab" :class="{ active: tasksSubTab === 'traces' }" @click="setSubTab('traces')">执行追踪</button>
      </div>
      <div class="panel-body">
        <div v-if="items.length === 0" class="empty-state">
          <div class="es-icon">T</div>
          <h3>暂无任务</h3>
        </div>
        <div v-else class="data-list-vue">
          <article v-for="(item, idx) in items" :key="item.ack_key || item.trace_id || idx" class="data-card-vue">
            <div v-for="field in fields" :key="field.key" class="data-row-vue">
              <strong>{{ field.label }}</strong>
              <span>{{ item[field.key] ?? '-' }}</span>
            </div>
          </article>
        </div>
      </div>
    </section>
  `,
};
