import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from '../lib/vue.esm-browser.prod.js';
import { callAPI, getBuildInfo, onAgentEvent, onBridgeEvent } from './services/api.js';
import { SidebarNav } from './components/SidebarNav.js';
import { ProjectModal } from './components/ProjectModal.js';
import { UnifiedChatPage } from './pages/UnifiedChatPage.js';
import { DataPage } from './pages/DataPage.js';
import { TasksPage } from './pages/TasksPage.js';
import { CommandsPage } from './pages/CommandsPage.js';
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
    UnifiedChatPage,
    DataPage,
    TasksPage,
    CommandsPage,
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
    let unsubscribeAgentEvent = () => { };
    let unsubscribeBridgeEvent = () => { };

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
      if (targetPage === 'chat' || targetPage === 'settings') return;
      const res = await callAPI('ui/dashboard/get', { page: targetPage });
      dashboard.agents = Array.isArray(res?.agents) ? res.agents : [];
      dashboard.dags = Array.isArray(res?.dags) ? res.dags : [];
      dashboard.taskAcks = Array.isArray(res?.taskAcks) ? res.taskAcks : [];
      dashboard.taskTraces = Array.isArray(res?.taskTraces) ? res.taskTraces : [];
      dashboard.skills = Array.isArray(res?.skills) ? res.skills : [];
      dashboard.commandCards = Array.isArray(res?.commandCards) ? res.commandCards : [];
      dashboard.prompts = Array.isArray(res?.prompts) ? res.prompts : [];
      dashboard.memory = Array.isArray(res?.memory) ? res.memory : [];
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
        <UnifiedChatPage
          v-if="page === 'chat'"
          mode="chat"
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

        <TasksPage
          v-else-if="page === 'tasks'"
          :tasks-sub-tab="tasksSubTab"
          :items="tasksItems"
          :fields="tasksFields"
          @update:tasks-sub-tab="tasksSubTab = $event"
        />

        <DataPage
          v-else-if="page === 'skills'"
          page-id="skills"
          title="技能管理"
          icon="S"
          :items="dashboard.skills"
          :fields="skillsFields"
          empty-text="暂无 Skill"
        />

        <CommandsPage
          v-else-if="page === 'commands'"
          :command-cards="dashboard.commandCards"
          :prompts="dashboard.prompts"
          :command-fields="commandFields"
          :prompt-fields="promptFields"
          @run-command="runCommandCard"
          @run-prompt="runPromptTemplate"
        />

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
          @refresh="refreshBuildInfo"
        />
      </main>

      <ProjectModal :store="projectStore" />
    </div>
  `,
};
