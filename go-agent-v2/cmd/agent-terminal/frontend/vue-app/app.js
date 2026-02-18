import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from '../lib/vue.esm-browser.prod.js';
import { callAPI, getBuildInfo, onAgentEvent, onBridgeEvent } from './services/api.js';
import { SidebarNav } from './components/SidebarNav.js';
import { ProjectModal } from './components/ProjectModal.js';
import { ChatPage } from './pages/ChatPage.js';
import { DataPage } from './pages/DataPage.js';
import { SettingsPage } from './pages/SettingsPage.js';
import { useProjectStore } from './stores/projects.js';
import { useThreadStore } from './stores/threads.js';

const REFRESH_INTERVAL_MS = 10000;

const NAV_ITEMS = Object.freeze([
  { key: 'chat', icon: '💬', label: 'Chat' },
  { key: 'agents', icon: 'A', label: 'Agent' },
  { key: 'dags', icon: 'D', label: 'DAG' },
  { key: 'tasks', icon: 'T', label: '任务' },
  { key: 'skills', icon: 'S', label: '技能' },
  { key: 'commands', icon: 'C', label: '命令' },
  { key: 'memory', icon: 'M', label: '记忆' },
  { key: 'settings', icon: '..', label: '设置' },
]);

export const AppRoot = {
  name: 'AppRoot',
  components: {
    SidebarNav,
    ProjectModal,
    ChatPage,
    DataPage,
    SettingsPage,
  },
  setup() {
    const projectStore = useProjectStore();
    const threadStore = useThreadStore();

    const page = ref('chat');
    const tasksSubTab = ref('acks');
    const buildInfo = reactive({});

    const dashboard = reactive({
      agents: [],
      dags: [],
      taskAcks: [],
      taskTraces: [],
      skills: [],
      commandCards: [],
      prompts: [],
      memory: [],
    });

    let refreshTimer = null;
    let unsubscribeAgentEvent = () => {};
    let unsubscribeBridgeEvent = () => {};

    const agentsFields = Object.freeze([
      { key: 'agent_id', label: 'Agent' },
      { key: 'status', label: '状态' },
      { key: 'updated_at', label: '更新时间' },
    ]);

    const dagsFields = Object.freeze([
      { key: 'dag_key', label: 'DAG' },
      { key: 'status', label: '状态' },
      { key: 'updated_at', label: '更新时间' },
    ]);

    const taskAckFields = Object.freeze([
      { key: 'ack_key', label: 'ACK' },
      { key: 'title', label: '标题' },
      { key: 'status', label: '状态' },
      { key: 'assigned_to', label: '负责人' },
    ]);

    const taskTraceFields = Object.freeze([
      { key: 'trace_id', label: 'Trace' },
      { key: 'span_name', label: 'Span' },
      { key: 'status', label: '状态' },
      { key: 'started_at', label: '开始' },
    ]);

    const skillsFields = Object.freeze([
      { key: 'name', label: '技能' },
      { key: 'path', label: '路径' },
    ]);

    const commandFields = Object.freeze([
      { key: 'card_key', label: '命令卡' },
      { key: 'title', label: '标题' },
      { key: 'risk_level', label: '风险级别' },
    ]);

    const promptFields = Object.freeze([
      { key: 'prompt_key', label: '提示词' },
      { key: 'title', label: '标题' },
      { key: 'agent_key', label: 'Agent' },
    ]);

    const memoryFields = Object.freeze([
      { key: 'path', label: '路径' },
      { key: 'updated_by', label: '更新者' },
      { key: 'updated_at', label: '更新时间' },
    ]);

    const tasksItems = computed(() => (tasksSubTab.value === 'acks' ? dashboard.taskAcks : dashboard.taskTraces));
    const tasksFields = computed(() => (tasksSubTab.value === 'acks' ? taskAckFields : taskTraceFields));

    async function refreshBuildInfo() {
      const info = await getBuildInfo();
      Object.assign(buildInfo, info || {});
    }

    async function ensureActiveThread() {
      let threadId = threadStore.state.activeThreadId || '';
      if (threadId) return threadId;

      threadId = await threadStore.startThread(projectStore.state.active || '.');
      if (!threadId) return '';

      threadStore.saveActiveThread(threadId);
      await threadStore.loadMessages(threadId);
      return threadId;
    }

    async function runCommandCard(card) {
      const command = (card?.command_template || '').toString().trim();
      if (!command) return;
      const threadId = await ensureActiveThread();
      if (!threadId) return;

      await threadStore.sendMessage(threadId, `请执行以下命令并反馈结果：\n${command}`);
      page.value = 'chat';
    }

    async function runPromptTemplate(prompt) {
      const text = (prompt?.prompt_text || prompt?.description || prompt?.title || '').toString().trim();
      if (!text) return;
      const threadId = await ensureActiveThread();
      if (!threadId) return;

      await threadStore.sendMessage(threadId, text);
      page.value = 'chat';
    }

    async function refreshDashboardByPage(targetPage) {
      switch (targetPage) {
        case 'agents': {
          const res = await callAPI('dashboard/agentStatus', {});
          dashboard.agents = res?.agents || [];
          break;
        }
        case 'dags': {
          const res = await callAPI('dashboard/dags', {});
          dashboard.dags = res?.dags || [];
          break;
        }
        case 'tasks': {
          const [acks, traces] = await Promise.all([
            callAPI('dashboard/taskAcks', {}),
            callAPI('dashboard/taskTraces', {}),
          ]);
          dashboard.taskAcks = acks?.acks || [];
          dashboard.taskTraces = traces?.traces || [];
          break;
        }
        case 'skills': {
          const res = await callAPI('dashboard/skills', {});
          dashboard.skills = res?.skills || [];
          break;
        }
        case 'commands': {
          const [cards, prompts] = await Promise.all([
            callAPI('dashboard/commandCards', {}),
            callAPI('dashboard/prompts', {}),
          ]);
          dashboard.commandCards = cards?.cards || [];
          dashboard.prompts = prompts?.prompts || [];
          break;
        }
        case 'memory': {
          const res = await callAPI('dashboard/sharedFiles', {});
          dashboard.memory = res?.files || [];
          break;
        }
        default:
          break;
      }
    }

    async function bootstrap() {
      await Promise.all([
        refreshBuildInfo(),
        threadStore.refreshThreads(),
      ]);

      if (threadStore.state.activeThreadId) {
        await threadStore.loadMessages(threadStore.state.activeThreadId);
      }

      unsubscribeAgentEvent = onAgentEvent((evt) => {
        threadStore.handleAgentEvent(evt);
      });
      unsubscribeBridgeEvent = onBridgeEvent((evt) => {
        threadStore.handleBridgeEvent(evt);
      });

      refreshTimer = setInterval(() => {
        threadStore.refreshThreads();
      }, REFRESH_INTERVAL_MS);
    }

    watch(
      () => page.value,
      (next) => {
        refreshDashboardByPage(next).catch((error) => {
          console.warn(`refresh page failed: ${next}`, error);
        });
      },
      { immediate: true },
    );

    onMounted(() => {
      bootstrap().catch((error) => {
        console.error('bootstrap failed:', error);
      });
    });

    onBeforeUnmount(() => {
      unsubscribeAgentEvent();
      unsubscribeBridgeEvent();
      if (refreshTimer) clearInterval(refreshTimer);
    });

    return {
      NAV_ITEMS,
      page,
      tasksSubTab,
      projectStore,
      threadStore,
      buildInfo,
      dashboard,
      agentsFields,
      dagsFields,
      taskAckFields,
      taskTraceFields,
      skillsFields,
      commandFields,
      promptFields,
      memoryFields,
      tasksItems,
      tasksFields,
      refreshBuildInfo,
      runCommandCard,
      runPromptTemplate,
    };
  },
  template: `
    <div class="app-shell">
      <SidebarNav :items="NAV_ITEMS" :page="page" @change="page = $event" />

      <main id="content">
        <ChatPage
          v-if="page === 'chat'"
          :project-store="projectStore"
          :thread-store="threadStore"
        />

        <DataPage
          v-else-if="page === 'agents'"
          page-id="agents"
          title="Agent 状态"
          icon="A"
          :items="dashboard.agents"
          :fields="agentsFields"
          empty-text="暂无 Agent"
        />

        <DataPage
          v-else-if="page === 'dags'"
          page-id="dags"
          title="DAG 管理"
          icon="D"
          :items="dashboard.dags"
          :fields="dagsFields"
          empty-text="暂无 DAG"
        />

        <section v-else-if="page === 'tasks'" id="page-tasks" class="page active">
          <div class="panel-header">
            <div class="ph-bar"></div>
            <div class="ph-text"><h2>任务管理</h2></div>
          </div>
          <div class="sub-tabs">
            <button class="sub-tab" :class="{ active: tasksSubTab === 'acks' }" @click="tasksSubTab = 'acks'">任务工单</button>
            <button class="sub-tab" :class="{ active: tasksSubTab === 'traces' }" @click="tasksSubTab = 'traces'">执行追踪</button>
          </div>
          <div class="panel-body">
            <div v-if="tasksItems.length === 0" class="empty-state">
              <div class="es-icon">T</div>
              <h3>暂无任务</h3>
            </div>
            <div v-else class="data-list-vue">
              <article v-for="(item, idx) in tasksItems" :key="idx" class="data-card-vue">
                <div v-for="field in tasksFields" :key="field.key" class="data-row-vue">
                  <strong>{{ field.label }}</strong>
                  <span>{{ item[field.key] ?? '-' }}</span>
                </div>
              </article>
            </div>
          </div>
        </section>

        <DataPage
          v-else-if="page === 'skills'"
          page-id="skills"
          title="技能管理"
          icon="S"
          :items="dashboard.skills"
          :fields="skillsFields"
          empty-text="暂无 Skill"
        />

        <section v-else-if="page === 'commands'" id="page-commands" class="page active">
          <div class="panel-header">
            <div class="ph-bar"></div>
            <div class="ph-text"><h2>命令卡 / 提示词</h2></div>
          </div>
          <div class="split-panel">
            <div class="split-left">
              <div class="section-header">COMMANDS</div>
              <div class="panel-body">
                <div v-if="dashboard.commandCards.length === 0" class="empty-state">
                  <div class="es-icon">C</div>
                  <h3>暂无命令卡</h3>
                </div>
                <div v-else class="data-list-vue">
                  <article v-for="(item, idx) in dashboard.commandCards" :key="'cmd-' + idx" class="data-card-vue">
                    <div v-for="field in commandFields" :key="field.key" class="data-row-vue">
                      <strong>{{ field.label }}</strong>
                      <span>{{ item[field.key] ?? '-' }}</span>
                    </div>
                    <div class="data-actions-vue">
                      <button class="btn btn-ghost btn-xs" @click="runCommandCard(item)">发送到当前会话</button>
                    </div>
                  </article>
                </div>
              </div>
            </div>
            <div class="split-divider"></div>
            <div class="split-right">
              <div class="section-header">PROMPTS</div>
              <div class="panel-body">
                <div v-if="dashboard.prompts.length === 0" class="empty-state">
                  <div class="es-icon">P</div>
                  <h3>暂无提示词</h3>
                </div>
                <div v-else class="data-list-vue">
                  <article v-for="(item, idx) in dashboard.prompts" :key="'prompt-' + idx" class="data-card-vue">
                    <div v-for="field in promptFields" :key="field.key" class="data-row-vue">
                      <strong>{{ field.label }}</strong>
                      <span>{{ item[field.key] ?? '-' }}</span>
                    </div>
                    <div class="data-actions-vue">
                      <button class="btn btn-ghost btn-xs" @click="runPromptTemplate(item)">发送到当前会话</button>
                    </div>
                  </article>
                </div>
              </div>
            </div>
          </div>
        </section>

        <DataPage
          v-else-if="page === 'memory'"
          page-id="memory"
          title="记忆"
          icon="M"
          :items="dashboard.memory"
          :fields="memoryFields"
          empty-text="暂无记忆"
        />

        <SettingsPage
          v-else
          :build-info="buildInfo"
          :refresh-build-info="refreshBuildInfo"
        />
      </main>

      <ProjectModal :store="projectStore" />
    </div>
  `,
};
