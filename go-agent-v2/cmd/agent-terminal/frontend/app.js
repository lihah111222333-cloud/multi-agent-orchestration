// app.js — Agent Orchestrator 主控制台。
//
// 统一架构 (主通道):
//   - 所有后端调用通过 Wails Go 绑定: window.go.main.App.CallAPI / SubmitInput / ...
//   - Agent 事件通过 Wails Events 推送
//   - 零 WebSocket: 前端不走外部通道
'use strict';

// ─── 常量 ───

const RESIZE_DEBOUNCE_MS = 150;
const DATA_REFRESH_MS = 10000;

const XTERM_THEME = Object.freeze({
    background: '#0C1017', foreground: '#EDF1F7',
    cursor: '#6B9BFF', cursorAccent: '#0C1017',
    selectionBackground: 'rgba(107,155,255,.25)',
    black: '#1A2230', red: '#F07070', green: '#5BE88A',
    yellow: '#E0B35B', blue: '#6B9BFF', magenta: '#C084FC',
    cyan: '#38BDF8', white: '#EDF1F7',
    brightBlack: '#3E5068', brightRed: '#F07070', brightGreen: '#5BE88A',
    brightYellow: '#E0B35B', brightBlue: '#6B9BFF', brightMagenta: '#C084FC',
    brightCyan: '#38BDF8', brightWhite: '#FFFFFF',
});

const XTERM_OPTS = Object.freeze({
    fontFamily: '"Menlo","SF Mono",monospace', fontSize: 12,
    lineHeight: 1.3, cursorBlink: true, cursorStyle: 'bar',
    scrollback: 5000, allowProposedApi: true,
    convertEol: true, // \n → \r\n, 防止阶梯式换行
    wordSeparator: ' ',
});

const ANSI = Object.freeze({
    red: (s) => `\x1b[31m${s}\x1b[0m`,
    cyan: (s) => `\x1b[36m${s}\x1b[0m`,
    dim: (s) => `\x1b[90m${s}\x1b[0m`,
    green: (s) => `\x1b[32m${s}\x1b[0m`,
    yellow: (s) => `\x1b[33m${s}\x1b[0m`,
});

// ─── DOM ───

const $ = (id) => document.getElementById(id);
const grid = $('grid');
const overlay = $('overlay');
const overlayPane = $('overlayPane');
const agentCountEl = $('agentCount');
const batchCountEl = $('batchCount');

// ═══════════════════════════════════════════
// Wails Go 绑定 (主通道 — 唯一通道)
// ═══════════════════════════════════════════

// 等待 Wails 绑定就绪
let _appReady = null;

const getApp = () => {
    const app = window.go?.main?.App;
    if (app) return app;
    if (!_appReady) {
        _appReady = new Promise((resolve) => {
            let attempts = 0;
            const check = () => {
                const a = window.go?.main?.App;
                if (a) {
                    console.log('[app] ✓ Wails App bindings ready');
                    resolve(a);
                    return;
                }
                if (++attempts >= 50) {
                    console.warn('[app] ✗ Wails App bindings not available after 10s');
                    resolve(null);
                    return;
                }
                setTimeout(check, 200);
            };
            check();
        });
    }
    return null;
};

const waitApp = () => {
    if (_appReady) return _appReady;
    const app = window.go?.main?.App;
    if (app) return Promise.resolve(app);
    // 触发等待
    getApp();
    return _appReady || Promise.resolve(null);
};

// 通用 API 调用: App.CallAPI(method, JSON) → JSON string
const callAPI = async (method, params = {}) => {
    const app = window.go?.main?.App || await waitApp();
    if (!app?.CallAPI) throw new Error('App bindings not ready');
    const raw = await app.CallAPI(method, JSON.stringify(params));
    return raw ? JSON.parse(raw) : null;
};

// ═══════════════════════════════════════════
// Terminal 面板管理
// ═══════════════════════════════════════════

const panes = new Map();
let zoomedAgent = null;

let resizeTimer = null;
const fitAll = () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => {
        panes.forEach((p) => p.fit.fit());
    }, RESIZE_DEBOUNCE_MS);
};

const createPane = (threadId, displayName) => {
    if (panes.has(threadId)) return;

    const el = document.createElement('div');
    el.className = 'pane';
    el.dataset.agent = threadId;
    el.innerHTML = `
    <div class="pane-header">
      <span class="pane-dot"></span>
      <span class="pane-title">${displayName || threadId}</span>
      <span class="pane-state">idle</span>
      <button class="pane-btn" data-action="zoom" title="放大">⤢</button>
      <button class="pane-btn" data-action="close" title="关闭">✕</button>
    </div>
    <div class="pane-body"></div>
    <div class="pane-input">
      <textarea rows="1" placeholder="输入消息..."></textarea>
      <button>发送</button>
    </div>`;

    const header = el.querySelector('.pane-header');
    header.addEventListener('click', (e) => {
        const action = e.target.closest('[data-action]')?.dataset?.action;
        if (action === 'zoom') toggleZoom(threadId);
        if (action === 'close') removePane(threadId);
    });
    header.addEventListener('dblclick', () => toggleZoom(threadId));

    // 发送消息: 通过 Wails 主通道
    const input = el.querySelector('.pane-input textarea');
    const handleSend = () => {
        const text = input.value.trim();
        if (!text) return;
        input.value = '';
        const tid = el.dataset.realId || threadId;
        const p = panes.get(tid) || panes.get(threadId);

        // 本地回显
        if (p) {
            p.term.writeln('');
            p.term.writeln(ANSI.green(`> ${text}`));
        }

        // Wails 主通道 — 直接调用 SubmitInput
        const app = window.go?.main?.App;
        if (app?.SubmitInput) {
            app.SubmitInput(tid, text)
                .catch((e) => { if (p) p.term.writeln(ANSI.red(`[error] ${e.message || e}`)); });
        } else {
            // 降级: 走 CallAPI
            callAPI('turn/start', { threadId: tid, input: [{ type: 'text', text }] })
                .catch((e) => { if (p) p.term.writeln(ANSI.red(`[error] ${e.message || e}`)); });
        }
    };
    input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && e.keyCode !== 229) {
            e.preventDefault();
            handleSend();
        }
    });
    el.querySelector('.pane-input button').addEventListener('click', handleSend);

    grid.appendChild(el);

    const term = new Terminal({ ...XTERM_OPTS, theme: XTERM_THEME });
    const fit = new FitAddon.FitAddon();
    term.loadAddon(fit);
    term.open(el.querySelector('.pane-body'));
    requestAnimationFrame(() => fit.fit());

    term.writeln(ANSI.dim(`[${threadId}] ready`));

    panes.set(threadId, {
        el, term, fit, input,
        dot: el.querySelector('.pane-dot'),
        stateEl: el.querySelector('.pane-state'),
        state: 'idle',
    });
    updateState(panes.get(threadId), 'idle');
    updateCount();
};

const removePane = (threadId) => {
    const pane = panes.get(threadId);
    if (!pane) return;
    pane.term.dispose();
    pane.el.remove();
    panes.delete(threadId);
    updateCount();
    // 主通道: Wails 绑定停止 Agent
    const app = window.go?.main?.App;
    if (app?.StopAgent) {
        app.StopAgent(threadId).catch(() => { });
    }
};

const toggleZoom = (threadId) => {
    if (zoomedAgent === threadId) {
        const pane = panes.get(threadId);
        if (pane) {
            const body = overlayPane.querySelector('.pane-body');
            const bar = overlayPane.querySelector('.pane-input');
            if (body) pane.el.appendChild(body);
            if (bar) pane.el.appendChild(bar);
        }
        overlay.classList.add('hidden');
        zoomedAgent = null;
        fitAll();
        return;
    }
    const pane = panes.get(threadId);
    if (!pane) return;
    overlayPane.innerHTML = '';
    const h = pane.el.querySelector('.pane-header').cloneNode(true);
    h.addEventListener('dblclick', () => toggleZoom(threadId));
    h.addEventListener('click', (e) => {
        if (e.target.closest('[data-action="zoom"]')) toggleZoom(threadId);
    });
    overlayPane.append(h, pane.el.querySelector('.pane-body'), pane.el.querySelector('.pane-input'));
    overlay.classList.remove('hidden');
    zoomedAgent = threadId;
    requestAnimationFrame(() => pane.fit.fit());
};

const updateState = (pane, state) => {
    pane.state = state;
    pane.stateEl.textContent = state;
    pane.dot.className = `pane-dot ${state}`;
};

const updateCount = () => { agentCountEl.textContent = `${panes.size} Agents`; };

// ═══════════════════════════════════════════
// Agent 启动 (主通道: Wails 绑定)
// ═══════════════════════════════════════════

const launchAgent = (tempId, displayName) => {
    createPane(tempId, displayName);
    // 主通道: Wails CallAPI → apiserver thread/start
    callAPI('thread/start', { cwd: '.' })
        .then((res) => {
            const realId = res?.thread?.id;
            if (realId && realId !== tempId) {
                const pane = panes.get(tempId);
                if (pane) {
                    panes.delete(tempId);
                    panes.set(realId, pane);
                    pane.el.dataset.agent = realId;
                    pane.el.querySelector('.pane-title').textContent = `${displayName} [${realId}]`;
                    pane.el.dataset.realId = realId;
                }
            }
            const p = panes.get(realId || tempId);
            if (p) updateState(p, 'running');
        })
        .catch((e) => {
            const p = panes.get(tempId);
            if (p) p.term.writeln(ANSI.red(`[error] ${e.message}`));
        });
};

// ═══════════════════════════════════════════
// Dashboard 数据加载 (主通道: Wails CallAPI)
// ═══════════════════════════════════════════

const renderCard = (title, subtitle, badgeText, badgeClass, rightText) => {
    const badge = badgeText ? `<span class="badge ${badgeClass || 'badge-primary'}">${badgeText}</span>` : '';
    const right = rightText ? `<span class="dc-right-text">${rightText}</span>` : '';
    return `<div class="data-card"><div class="dc-row"><div class="dc-left"><strong>${title}</strong><span>${subtitle || ''}</span></div>${right}${badge}</div></div>`;
};

const badgeFor = (status) => {
    if (!status) return 'badge-muted';
    const s = status.toLowerCase();
    if (s === 'running' || s === 'in_progress') return 'badge-primary';
    if (s === 'completed' || s === 'done' || s === 'success') return 'badge-success';
    if (s === 'pending' || s === 'waiting' || s === 'draft') return 'badge-warning';
    if (s === 'error' || s === 'failed' || s === 'stopped') return 'badge-error';
    return 'badge-muted';
};

const emptyState = (icon, title, msg) =>
    `<div class="empty-state"><div class="es-icon">${icon}</div><h3>${title}</h3><p>${msg}</p></div>`;

// Agent 状态页
const loadAgents = async () => {
    try {
        const res = await callAPI('dashboard/agentStatus', {});
        const list = res?.agents;
        const body = $('agentsBody');
        if (!body) return;
        if (!list || list.length === 0) {
            body.innerHTML = emptyState('A', '暂无 Agent', '启动后在此显示');
        } else {
            body.innerHTML = list.map((a) =>
                renderCard(a.agent_name || a.agent_id, `ID: ${a.agent_id}`, a.status, badgeFor(a.status))
            ).join('');
        }
    } catch (e) { console.warn('loadAgents:', e); }
};

// DAG 管理页
const loadDAGs = async () => {
    try {
        const res = await callAPI('dashboard/dags', {});
        const list = res?.dags;
        const body = $('dagsBody');
        const stats = $('dagStats');
        if (!body) return;
        if (!list || list.length === 0) {
            body.innerHTML = emptyState('D', '暂无 DAG', '创建 DAG 后将在此显示');
            if (stats) stats.innerHTML = '';
        } else {
            const total = list.length;
            const running = list.filter((x) => x.status === 'running').length;
            const done = list.filter((x) => x.status === 'completed' || x.status === 'done').length;
            if (stats) stats.innerHTML = `
                <div class="stat-card"><span class="stat-value">${total}</span><span class="stat-label">总计</span></div>
                <div class="stat-card"><span class="stat-value" style="color:var(--primary)">${running}</span><span class="stat-label">运行中</span></div>
                <div class="stat-card"><span class="stat-value" style="color:var(--success)">${done}</span><span class="stat-label">已完成</span></div>`;
            body.innerHTML = list.map((x) =>
                renderCard(x.title || x.dag_key, x.description || '', x.status, badgeFor(x.status))
            ).join('');
        }
    } catch (e) { console.warn('loadDAGs:', e); }
};

// 任务页
const loadTasks = async () => {
    try {
        const res = await callAPI('dashboard/taskAcks', {});
        const acks = res?.acks;
        const body = $('tasksBody');
        if (!body) return;
        if (!acks || acks.length === 0) {
            body.innerHTML = emptyState('T', '暂无任务', '创建任务后在此显示');
        } else {
            body.innerHTML = acks.map((t) =>
                renderCard(
                    t.title || t.ack_key,
                    `分配: ${t.assigned_to || '-'} · 优先级: ${t.priority || '-'}`,
                    t.status, badgeFor(t.status),
                    t.progress ? `${t.progress}%` : ''
                )
            ).join('');
        }
    } catch (e) { console.warn('loadTasks:', e); }
};

// Skills 页
const loadSkills = async () => {
    try {
        const res = await callAPI('dashboard/skills', {});
        const list = res?.skills;
        const body = $('skillsBody');
        if (!body) return;
        if (!list || list.length === 0) {
            body.innerHTML = emptyState('S', '未找到 Skill', '在 .agent/skills/ 目录创建 Skill');
        } else {
            body.innerHTML = list.map((s) =>
                renderCard(s.Name || s.name, s.Description || s.description || '')
            ).join('');
        }
    } catch (e) { console.warn('loadSkills:', e); }
};

// 命令卡 + 提示词页
const loadCommands = async () => {
    try {
        const [cmdRes, promptRes] = await Promise.all([
            callAPI('dashboard/commandCards', {}),
            callAPI('dashboard/prompts', {}),
        ]);
        const cards = cmdRes?.cards;
        const prompts = promptRes?.prompts;
        const cmdBody = $('cmdCardsBody');
        const pBody = $('promptsBody');
        if (!cmdBody || !pBody) return;

        const cmdCountEl = $('cmdCount');
        const promptCountEl = $('promptCount');
        if (cmdCountEl) cmdCountEl.textContent = (cards?.length ?? 0).toString();
        if (promptCountEl) promptCountEl.textContent = (prompts?.length ?? 0).toString();

        if (!cards || cards.length === 0) {
            cmdBody.innerHTML = emptyState('C', '暂无命令卡', '');
        } else {
            cmdBody.innerHTML = cards.map((c) =>
                renderCard(c.title || c.card_key, c.description || '',
                    c.enabled ? '启用' : '禁用', c.enabled ? 'badge-success' : 'badge-muted',
                    c.risk_level)
            ).join('');
        }

        if (!prompts || prompts.length === 0) {
            pBody.innerHTML = emptyState('P', '暂无提示词', '');
        } else {
            pBody.innerHTML = prompts.map((p) =>
                renderCard(p.title || p.prompt_key, p.description || '')
            ).join('');
        }
    } catch (e) { console.warn('loadCommands:', e); }
};

// 记忆库页
const loadMemory = async () => {
    try {
        const res = await callAPI('dashboard/sharedFiles', {});
        const list = res?.files;
        const body = $('memoryBody');
        if (!body) return;
        const countEl = $('memCount');
        if (countEl) countEl.textContent = `${list?.length ?? 0} files`;
        if (!list || list.length === 0) {
            body.innerHTML = emptyState('M', '记忆库为空', '共享文件将在此显示');
        } else {
            body.innerHTML = list.map((f) =>
                renderCard(f.path, `更新者: ${f.updated_by || '-'}`, null, null,
                    new Date(f.updated_at).toLocaleString())
            ).join('');
        }
    } catch (e) { console.warn('loadMemory:', e); }
};

// 日志页 (审计 + AI + 总线)
const loadLogs = async () => {
    try {
        const [auditRes, aiRes, busRes] = await Promise.all([
            callAPI('dashboard/auditLogs', { limit: 50 }),
            callAPI('dashboard/aiLogs', { limit: 50 }),
            callAPI('dashboard/busLogs', { limit: 50 }),
        ]);
        // 日志数据可以在 settings 页或专门的日志页渲染
        console.log('[logs] audit:', auditRes?.logs?.length,
            'ai:', aiRes?.logs?.length,
            'bus:', busRes?.logs?.length);
    } catch (e) { console.warn('loadLogs:', e); }
};

const loadPageData = {
    agents: loadAgents,
    dags: loadDAGs,
    tasks: loadTasks,
    skills: loadSkills,
    commands: loadCommands,
    memory: loadMemory,
};

// ═══════════════════════════════════════════
// 侧栏路由
// ═══════════════════════════════════════════

let currentPage = 'terminal';

document.querySelectorAll('.sidebar-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
        const page = btn.dataset.page;
        if (page === currentPage) return;

        document.querySelector('.sidebar-btn.active')?.classList.remove('active');
        btn.classList.add('active');
        document.querySelector('.page.active')?.classList.remove('active');
        $(`page-${page}`)?.classList.add('active');

        currentPage = page;
        if (page === 'terminal') requestAnimationFrame(fitAll);
        if (loadPageData[page]) loadPageData[page]();
    });
});

// ─── 子标签切换 ───

document.querySelectorAll('.sub-tab').forEach((tab) => {
    tab.addEventListener('click', () => {
        tab.parentElement.querySelector('.sub-tab.active')?.classList.remove('active');
        tab.classList.add('active');
    });
});

// ═══════════════════════════════════════════
// 工具栏事件 (Terminal 页)
// ═══════════════════════════════════════════

// 批量启动
$('btnBatch').addEventListener('click', () => {
    const count = Math.min(parseInt(batchCountEl.value, 10) || 4, 32);
    for (let i = 1; i <= count; i++) {
        const tempId = `a-${Date.now()}-${i}`;
        launchAgent(tempId, `Agent ${i}`);
    }
});

// 添加单个
$('btnAdd').addEventListener('click', () => {
    const n = panes.size + 1;
    const tempId = `a-${Date.now()}`;
    launchAgent(tempId, `Agent ${n}`);
});

// 新窗口
$('btnNewWindow').addEventListener('click', () => {
    const count = parseInt(batchCountEl.value, 10) || 4;
    const groupName = `Grid-${Date.now().toString(36).slice(-4)}`;
    const app = window.go?.main?.App;
    app?.OpenNewWindow?.(groupName, count)
        ?.catch?.((e) => console.error('new window:', e));
});

$('btnReset').addEventListener('click', fitAll);

// ─── 全局事件 ───

document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && zoomedAgent) toggleZoom(zoomedAgent);
});

window.addEventListener('resize', fitAll);

// ─── 定时刷新 ───

setInterval(() => {
    if (loadPageData[currentPage]) loadPageData[currentPage]();
}, DATA_REFRESH_MS);

// ═══════════════════════════════════════════
// 启动
// ═══════════════════════════════════════════

// Wails 事件桥 (唯一的事件接收通道)
if (window.runtime) {
    window.runtime.EventsOn('auto-launch', (data) => {
        const { count, group } = data;
        for (let i = 1; i <= count; i++) {
            createPane(`${group || 'a'}-${i}`, `${group || 'Agent'} ${i}`);
        }
    });

    // Agent 事件: Go handleEvent → Wails Events → 前端
    window.runtime.EventsOn('agent-event', (data) => {
        const { agent_id, type, data: payload } = data;
        const pane = panes.get(agent_id);
        if (!pane) return;

        switch (type) {
            case 'agent_message_delta':
            case 'exec_output_delta':
            case 'item/agentMessage/delta':
            case 'item/commandExecution/outputDelta':
                try {
                    const d = JSON.parse(payload);
                    const text = d.delta ?? d.content ?? '';
                    if (text) pane.term.write(text);
                } catch { pane.term.write(payload); }
                break;
            case 'turn_started':
            case 'turn/started':
                updateState(pane, 'thinking');
                break;
            case 'idle':
            case 'turn_complete':
            case 'turn/completed':
                updateState(pane, 'idle');
                break;
            case 'item/reasoning/textDelta':
                try {
                    const d = JSON.parse(payload);
                    if (d.delta) pane.term.write(ANSI.dim(d.delta));
                } catch { }
                break;
            case 'item/commandExecution/requestApproval':
                try {
                    const d = JSON.parse(payload);
                    pane.term.writeln(ANSI.cyan(`[approval] ${d.command ?? ''}`));
                    updateState(pane, 'waiting');
                } catch { }
                break;
            case 'item/fileChange/started':
                try {
                    const d = JSON.parse(payload);
                    if (d.file) pane.term.writeln(ANSI.cyan(`📝 editing: ${d.file}`));
                } catch { }
                break;
            case 'item/fileChange/completed':
                try {
                    const d = JSON.parse(payload);
                    if (d.file) pane.term.writeln(ANSI.green(`✅ saved: ${d.file}`));
                } catch { }
                break;
            case 'error':
                try {
                    const d = JSON.parse(payload);
                    pane.term.writeln(ANSI.red(`[error] ${d.message ?? payload}`));
                } catch { pane.term.writeln(ANSI.red(`[error] ${payload}`)); }
                break;
            default:
                // 未知事件类型静默忽略 (可开 debug 查看)
                break;
        }
    });

    // 获取窗口分组名
    const app = window.go?.main?.App;
    app?.GetGroup?.()
        ?.then?.((g) => { if (g) document.querySelector('.logo').textContent = `▸ ${g}`; })
        ?.catch?.(() => { });
}

// 等待绑定就绪后加载当前页数据 + 健康检查
waitApp().then((app) => {
    if (app) {
        console.log('[app] ✓ all bindings ready, loading data');
        // 健康检查: 调用 initialize 确认后端就绪
        callAPI('initialize', { protocolVersion: '2.0', clientInfo: { name: 'agent-orchestrator-ui' } })
            .then(() => {
                const el = $('dbStatus');
                if (el) { el.textContent = '已连接'; el.className = 'badge badge-success'; }
            })
            .catch(() => {
                const el = $('dbStatus');
                if (el) { el.textContent = '未连接'; el.className = 'badge badge-error'; }
            });
        if (loadPageData[currentPage]) loadPageData[currentPage]();
    } else {
        console.warn('[app] ✗ bindings not available — all pages will be empty');
    }
});
