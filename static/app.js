/* ===== Dashboard App JS ===== */

const SSE_SYNC_MS = (window.__SSE_SYNC_SEC || 5) * 1000;
const POLL_REFRESH_MS = SSE_SYNC_MS;
const AUDIT_LOG_LIMIT = window.__AUDIT_LOG_LIMIT || 100;
const SYSTEM_LOG_LIMIT = window.__SYSTEM_LOG_LIMIT || 100;

let refreshTimer = null;
let eventSource = null;
let reconnectTimer = null;
const commandRunCache = new Map();
const AGENT_STATUS_CLASSES = ['running', 'idle', 'stuck', 'error', 'disconnected', 'unknown'];

/* ---- Navigation ---- */
function switchPage(pageId) {
    document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
    document.querySelectorAll('.nav-btn').forEach(b => b.classList.remove('active'));
    const page = document.getElementById('page-' + pageId);
    const btn = document.getElementById('nav-' + pageId);
    if (page) page.classList.add('active');
    if (btn) btn.classList.add('active');
}

/* ---- Toast ---- */
function toast(msg, ok) {
    const c = document.getElementById('toast-container');
    const d = document.createElement('div');
    d.className = 'toast ' + (ok ? 'success' : 'error');
    d.textContent = msg;
    c.appendChild(d);
    setTimeout(() => d.remove(), 3500);
}

function escapeHtml(value) {
    return String(value || '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function classToken(value, fallback = 'unknown') {
    const normalized = String(value || '')
        .replace(/[^A-Za-z0-9_-]/g, '-')
        .replace(/-+/g, '-')
        .replace(/^-|-$/g, '');
    return normalized || fallback;
}

function setLiveStatus(status, detail = '') {
    const el = document.getElementById('live-status');
    if (!el) return;

    el.classList.remove('live-status-pending', 'live-status-online', 'live-status-offline');
    if (status === 'online') {
        el.classList.add('live-status-online');
        el.textContent = detail ? `实时通道已连接 · ${detail}` : '实时通道已连接';
        return;
    }
    if (status === 'offline') {
        el.classList.add('live-status-offline');
        el.textContent = detail ? `实时通道断开 · ${detail}` : '实时通道断开，重连中...';
        return;
    }

    el.classList.add('live-status-pending');
    el.textContent = detail ? `实时通道连接中 · ${detail}` : '实时通道连接中...';
}

function renderAgentSummary(summary, ts, error = '') {
    const summaryEl = document.getElementById('agent-status-summary');
    const healthEl = document.getElementById('agent-health-stat');
    if (!summaryEl && !healthEl) return;

    const total = Number(summary?.total || 0);
    const healthy = Number(summary?.healthy || 0);
    const unhealthy = Number(summary?.unhealthy || 0);
    const running = Number(summary?.running || 0);
    const idle = Number(summary?.idle || 0);
    const stuck = Number(summary?.stuck || 0);
    const disconnected = Number(summary?.disconnected || 0);
    const failed = Number(summary?.error || 0);

    if (healthEl) {
        healthEl.textContent = total > 0 ? `${healthy}/${total}` : '--';
        healthEl.classList.remove('green', 'amber', 'red', 'blue', 'cyan');
        if (total === 0) {
            healthEl.classList.add('amber');
        } else if (unhealthy === 0) {
            healthEl.classList.add('green');
        } else {
            healthEl.classList.add('amber');
        }
    }

    if (!summaryEl) return;

    const tsText = String(ts || '').replace('T', ' ').slice(0, 19) || '--';
    const base = `Agents ${healthy}/${total} healthy | running=${running}, idle=${idle}, stuck=${stuck}, error=${failed}, disconnected=${disconnected} | updated=${tsText}`;
    summaryEl.textContent = error ? `${base} | error=${error}` : base;
}

function setAgentChipStatus(agentId, status, staleSec, hasError) {
    const chips = document.querySelectorAll('.agent-chip[data-agent-id]');
    const normalized = AGENT_STATUS_CLASSES.includes(status) ? status : 'unknown';

    chips.forEach((chip) => {
        if (chip.dataset.agentId !== agentId) return;
        AGENT_STATUS_CLASSES.forEach((name) => chip.classList.remove(`agent-chip-status-${name}`));
        chip.classList.add(`agent-chip-status-${normalized}`);

        const stateEl = chip.querySelector('.agent-chip-state');
        if (stateEl) {
            const staleText = Number(staleSec || 0) > 0 ? ` (${Number(staleSec)}s)` : '';
            stateEl.textContent = `${normalized}${staleText}`;
        }

        if (hasError) {
            chip.title = 'agent output read error';
        }
    });
}

function resetAllAgentChipStatus() {
    const chips = document.querySelectorAll('.agent-chip[data-agent-id]');
    chips.forEach((chip) => {
        AGENT_STATUS_CLASSES.forEach((name) => chip.classList.remove(`agent-chip-status-${name}`));
        chip.classList.add('agent-chip-status-unknown');
        const stateEl = chip.querySelector('.agent-chip-state');
        if (stateEl) stateEl.textContent = 'unknown';
    });
}

async function loadAgentStatus() {
    try {
        const r = await fetch('/api/agent-status?lines=30');
        const j = await r.json();

        if (!j.ok) {
            resetAllAgentChipStatus();
            renderAgentSummary(j.summary || {}, j.ts || '', j.error || 'agent_status_unavailable');
            renderMonitorTable([], {}, '');
            return;
        }

        applyAgentStatusPayload(j);
    } catch (e) {
        resetAllAgentChipStatus();
        renderAgentSummary({}, '', `network_error:${e.message}`);
        renderMonitorTable([], {}, '');
    }
}

function applyAgentStatusPayload(payload = {}) {
    const rows = Array.isArray(payload.agents) ? payload.agents : [];
    const seen = new Set();

    rows.forEach((row) => {
        const agentId = String(row.agent_id || '').trim();
        if (!agentId) return;

        seen.add(agentId);
        setAgentChipStatus(
            agentId,
            String(row.status || 'unknown').toLowerCase(),
            Number(row.stagnant_sec || 0),
            Boolean(row.error),
        );
    });

    document.querySelectorAll('.agent-chip[data-agent-id]').forEach((chip) => {
        const id = chip.dataset.agentId || '';
        if (!seen.has(id)) {
            setAgentChipStatus(id, 'unknown', 0, false);
        }
    });

    renderAgentSummary(payload.summary || {}, payload.ts || '', payload.error || '');
    renderMonitorTable(rows, payload.summary || {}, payload.ts || '');
}

function renderMonitorTable(agents, summary, ts) {
    const tbody = document.getElementById('mon-tbody');
    const emptyEl = document.getElementById('mon-empty');
    if (!tbody) return;

    // Update summary badges
    AGENT_STATUS_CLASSES.forEach(s => {
        const el = document.getElementById('mon-' + s);
        if (el) el.textContent = String(Number(summary[s] || 0));
    });

    // Update timestamp
    const tsEl = document.getElementById('mon-updated');
    if (tsEl) {
        const tsText = String(ts || '').replace('T', ' ').slice(0, 19) || '--';
        tsEl.textContent = '最后更新: ' + tsText;
    }

    // Render rows
    if (!agents.length) {
        tbody.innerHTML = '';
        if (emptyEl) emptyEl.style.display = '';
        return;
    }
    if (emptyEl) emptyEl.style.display = 'none';

    tbody.innerHTML = agents.map(row => {
        const agentId = escapeHtml(row.agent_id || '');
        const name = escapeHtml(row.agent_name || '');
        const status = String(row.status || 'unknown').toLowerCase();
        const statusClass = classToken(status);
        const stale = Number(row.stagnant_sec || 0);
        const error = escapeHtml(row.error || '');
        const output = escapeHtml(String(row.output_tail || '').slice(-120));
        return `<tr>
            <td style="font-family:var(--font-mono);font-size:0.75rem">${agentId}</td>
            <td>${name}</td>
            <td><span class="level-badge level-${statusClass}">${escapeHtml(status)}</span></td>
            <td style="text-align:center">${stale > 0 ? stale : '-'}</td>
            <td style="max-width:160px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--red)">${error}</td>
            <td style="max-width:280px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:var(--font-mono);font-size:0.72rem;color:var(--text-muted)">${output}</td>
        </tr>`;
    }).join('');
}

async function refreshAgentMonitor() {
    await loadAgentStatus();
    toast('Agent 状态已刷新', true);
}

/* ---- Config ---- */
async function saveConfig() {
    const form = document.getElementById('config-form');
    const data = {};
    new FormData(form).forEach((v, k) => { data[k] = v; });
    try {
        const r = await fetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        });
        const j = await r.json();
        if (j.ok) {
            toast('配置已保存，重启 Master 后生效', true);
        } else {
            toast(j.error || '保存失败', false);
        }
    } catch (e) { toast('网络错误: ' + e.message, false); }
}

function togglePw(key) {
    const inp = document.getElementById('pw-' + key);
    inp.type = inp.type === 'password' ? 'text' : 'password';
}

/* ---- Approvals ---- */
function renderApprovals(approvals) {
    const box = document.getElementById('approval-list');
    if (!approvals.length) {
        box.innerHTML = '<div class="approval-empty">暂无待审批拓扑变更</div>';
        return;
    }
    box.innerHTML = approvals.map(req => {
        const proposed = req.proposed_architecture || {};
        const gws = (proposed.gateways || []).length;
        const ags = (proposed.gateways || []).reduce((s, g) => s + (g.agents || []).length, 0);
        const ts = (req.created_at || '').slice(0, 19).replace('T', ' ');
        const id = String(req.id || '');
        return `<div class="approval-card">
            <div class="approval-meta">
                <span class="approval-id">${escapeHtml(id || '-')}</span>
                <span class="approval-ts">${escapeHtml(ts)}</span>
            </div>
            <div class="approval-desc">${escapeHtml(req.reason || '拓扑变更审批')}</div>
            <div class="approval-stats">${gws} Gateway / ${ags} Agent</div>
            <div class="approval-actions">
                <button class="btn btn-sm btn-primary" onclick="reviewApproval('${encodeURIComponent(id)}','approve')">批准</button>
                <button class="btn btn-sm btn-danger" onclick="reviewApproval('${encodeURIComponent(id)}','reject')">拒绝</button>
            </div>
        </div>`;
    }).join('');
}

async function reviewApproval(encodedId, action) {
    try {
        const r = await fetch('/api/topology/approvals/' + encodedId + '/' + action, { method: 'POST' });
        const j = await r.json();
        if (j.ok) {
            toast(action === 'approve' ? '审批通过，拓扑已生效' : '审批已拒绝', true);
            refreshSections();
        } else { toast(j.error || '审批失败', false); }
    } catch (e) { toast('网络错误: ' + e.message, false); }
}

/* ---- Audit Logs ---- */
function renderAuditRows(events) {
    const tbody = document.getElementById('audit-tbody');
    tbody.innerHTML = events.map(e => {
        const ts = escapeHtml((e.ts || '').slice(0, 19).replace('T', ' '));
        const eventType = escapeHtml(e.event_type || '');
        const action = escapeHtml(e.action || '');
        const result = escapeHtml(e.result || '');
        const resultClass = classToken(e.result || '');
        const actor = escapeHtml(e.actor || '');
        const detail = escapeHtml(e.detail || '');
        return `<tr>
            <td style="font-family:var(--font-mono);font-size:0.72rem;color:var(--text-muted)">${ts}</td>
            <td>${eventType}</td>
            <td>${action}</td>
            <td><span class="level-badge level-${resultClass}">${result}</span></td>
            <td>${actor}</td>
            <td style="max-width:240px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${detail}</td>
        </tr>`;
    }).join('');
}

function renderFilterOptions(selectId, values) {
    const sel = document.getElementById(selectId);
    if (!sel) return;
    const current = sel.value;
    const opts = '<option value="">全部</option>' + values.map(v => {
        const safeValue = escapeHtml(v);
        return `<option value="${safeValue}" ${v === current ? 'selected' : ''}>${safeValue}</option>`;
    }).join('');
    sel.innerHTML = opts;
}

async function loadAuditLogs() {
    const params = new URLSearchParams();
    params.set('limit', String(AUDIT_LOG_LIMIT));
    ['audit-event-type:event_type', 'audit-action:action', 'audit-result:result', 'audit-actor:actor'].forEach(pair => {
        const [id, key] = pair.split(':');
        const v = document.getElementById(id)?.value;
        if (v) params.set(key, v);
    });
    const kw = document.getElementById('audit-keyword')?.value?.trim();
    if (kw) params.set('keyword', kw);
    const r = await fetch('/api/audit?' + params);
    const j = await r.json();
    if (j.ok) {
        renderAuditRows(j.events || []);
        const f = j.filters || {};
        renderFilterOptions('audit-event-type', f.event_types || []);
        renderFilterOptions('audit-action', f.actions || []);
        renderFilterOptions('audit-result', f.results || []);
        renderFilterOptions('audit-actor', f.actors || []);
    }
}

/* ---- System Logs ---- */
function renderSystemRows(logs) {
    const tbody = document.getElementById('system-tbody');
    tbody.innerHTML = logs.map(e => {
        const ts = escapeHtml(e.ts || '');
        const level = escapeHtml(e.level || '');
        const levelClass = classToken(e.level || '');
        const loggerName = escapeHtml(e.logger || '');
        const message = escapeHtml(e.message || '');
        return `<tr>
            <td style="font-family:var(--font-mono);font-size:0.72rem;color:var(--text-muted)">${ts}</td>
            <td><span class="level-badge level-${levelClass}">${level}</span></td>
            <td style="font-family:var(--font-mono);font-size:0.75rem">${loggerName}</td>
            <td style="max-width:400px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${message}</td>
        </tr>`;
    }).join('');
}

async function loadSystemLogs() {
    const params = new URLSearchParams();
    params.set('limit', String(SYSTEM_LOG_LIMIT));
    const level = document.getElementById('system-level')?.value;
    const logger = document.getElementById('system-logger')?.value;
    const kw = document.getElementById('system-keyword')?.value?.trim();
    if (level) params.set('level', level);
    if (logger) params.set('logger', logger);
    if (kw) params.set('keyword', kw);
    const r = await fetch('/api/system-log?' + params);
    const j = await r.json();
    if (j.ok) {
        renderSystemRows(j.logs || []);
        const f = j.filters || {};
        renderFilterOptions('system-level', f.levels || []);
        renderFilterOptions('system-logger', f.loggers || []);
    }
}

function exportSystemLogs() {
    const params = new URLSearchParams();
    params.set('limit', String(SYSTEM_LOG_LIMIT));
    window.location.href = '/api/system-log/export?' + params;
}

/* ---- Prompts ---- */
async function loadPrompts() {
    try {
        const r = await fetch('/api/prompts');
        const j = await r.json();
        if (!j.ok) return;
        const box = document.getElementById('prompts-list');
        box.innerHTML = j.agents.map((agent, ai) => {
            const isPinned = agent.is_pinned;
            const pinnedClass = isPinned ? ' prompt-card--pinned' : '';

            // For pinned system prompt, show the full prompt_text directly
            if (isPinned && agent.prompt_text) {
                return `<div class="prompt-card${pinnedClass}">
                    <div class="prompt-header" onclick="this.parentElement.querySelector('.prompt-body').classList.toggle('hidden')">
                        <div>
                            <span class="prompt-agent-name" style="color:var(--accent)">${agent.description}</span>
                        </div>
                        <span class="prompt-tools-count" style="color:var(--amber)">置顶</span>
                    </div>
                    <div class="prompt-body">
                        <div class="prompt-tool">
                            <textarea class="prompt-textarea" id="prompt-${ai}-sys" style="min-height:300px">${agent.prompt_text}</textarea>
                            <div class="prompt-actions">
                                <span class="copy-ok" id="copy-ok-${ai}-sys">已复制</span>
                                <button class="btn btn-sm btn-secondary" onclick="copyPromptId('prompt-${ai}-sys','copy-ok-${ai}-sys')">
                                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                                    复制
                                </button>
                                <button class="btn btn-sm btn-primary" onclick="savePrompt('_system','system_prompt','prompt-${ai}-sys')">
                                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/></svg>
                                    保存
                                </button>
                            </div>
                        </div>
                    </div>
                </div>`;
            }

            const toolsHtml = agent.tools.map((tool, ti) => {
                const paramsStr = tool.params.map(p => {
                    const def = p.default !== null ? ` = "${p.default}"` : '';
                    return `${p.name}: ${p.type}${def}`;
                }).join(', ');
                const promptText = tool.prompt_text || `你有一个 MCP 工具叫 "${tool.name}"。\n\n功能: ${tool.description}\n参数: ${paramsStr || '无'}\n\n使用场景: 当用户需要${tool.description}时，调用此工具。`;
                return `<div class="prompt-tool">
                    <div class="prompt-tool-name">${tool.name}</div>
                    <div class="prompt-tool-desc">${tool.description}</div>
                    <div class="prompt-params">参数: ${paramsStr || '无'}</div>
                    <textarea class="prompt-textarea" id="prompt-${ai}-${ti}">${promptText}</textarea>
                    <div class="prompt-actions">
                        <span class="copy-ok" id="copy-ok-${ai}-${ti}">已复制</span>
                        <button class="btn btn-sm btn-secondary" onclick="copyPromptId('prompt-${ai}-${ti}','copy-ok-${ai}-${ti}')">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                            复制
                        </button>
                        <button class="btn btn-sm btn-primary" onclick="savePrompt('${agent.key}','${tool.name}','prompt-${ai}-${ti}')">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/></svg>
                            保存
                        </button>
                    </div>
                </div>`;
            }).join('');
            return `<div class="prompt-card${pinnedClass}">
                <div class="prompt-header" onclick="this.parentElement.querySelector('.prompt-body').classList.toggle('hidden')">
                    <div>
                        <span class="prompt-agent-name">${agent.key}</span>
                        <span class="prompt-agent-desc">${agent.description}</span>
                    </div>
                    <span class="prompt-tools-count">${agent.tools.length} tools</span>
                </div>
                <div class="prompt-body">${toolsHtml}</div>
            </div>`;
        }).join('');
    } catch (e) { console.error('loadPrompts error:', e); }
}

function copyPromptId(textareaId, feedbackId) {
    const ta = document.getElementById(textareaId);
    if (!ta) return;
    navigator.clipboard.writeText(ta.value).then(() => {
        const ok = document.getElementById(feedbackId);
        ok.classList.add('show');
        setTimeout(() => ok.classList.remove('show'), 1500);
    });
}

async function savePrompt(agentKey, toolName, textareaId) {
    const ta = document.getElementById(textareaId);
    if (!ta) return;
    try {
        const r = await fetch('/api/prompts', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ agent_key: agentKey, tool_name: toolName, prompt_text: ta.value })
        });
        const j = await r.json();
        if (j.ok) { toast('提示词已保存到数据库', true); }
        else { toast(j.error || '保存失败', false); }
    } catch (e) { toast('保存失败: ' + e.message, false); }
}

/* ---- Command Cards ---- */
function renderCommandCards(cards) {
    const tbody = document.getElementById('cmd-card-tbody');
    const select = document.getElementById('cmd-card-key');
    if (!tbody || !select) return;

    const current = select.value;
    select.innerHTML = '<option value="">选择命令卡</option>' + cards.map(card => {
        const key = escapeHtml(card.card_key || '');
        const title = escapeHtml(card.title || card.card_key || '');
        const selected = current === card.card_key ? 'selected' : '';
        return `<option value="${key}" ${selected}>${title} (${key})</option>`;
    }).join('');

    if (!cards.length) {
        tbody.innerHTML = '<tr><td colspan="4" style="color:var(--text-muted)">暂无命令卡</td></tr>';
        return;
    }

    tbody.innerHTML = cards.map(card => {
        const key = escapeHtml(card.card_key || '');
        const risk = escapeHtml(card.risk_level || 'normal');
        const riskClass = classToken(card.risk_level || 'normal');
        const enabledText = card.enabled ? 'enabled' : 'disabled';
        const desc = escapeHtml(card.description || '');
        return `<tr>
            <td style="font-family:var(--font-mono);font-size:0.75rem">${key}</td>
            <td><span class="level-badge level-${riskClass}">${risk}</span></td>
            <td>${enabledText}</td>
            <td style="max-width:420px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${desc}</td>
        </tr>`;
    }).join('');
}

function _runActions(run) {
    const status = String(run.status || '').toLowerCase();
    const id = Number(run.id || 0);
    if (!id) return '-';

    const actions = [`<button class="btn btn-sm btn-secondary" onclick="showCommandRunDetail(${id})">详情</button>`];
    if (status === 'pending_review') {
        actions.push(`<button class="btn btn-sm btn-primary" onclick="reviewCommandRun(${id},'approved')">批准</button>`);
        actions.push(`<button class="btn btn-sm btn-danger" onclick="reviewCommandRun(${id},'rejected')">拒绝</button>`);
    } else if (status === 'ready' || status === 'failed') {
        actions.push(`<button class="btn btn-sm btn-primary" onclick="executeCommandRun(${id})">执行</button>`);
    }
    return actions.join(' ');
}

function renderCommandRuns(runs) {
    const tbody = document.getElementById('cmd-run-tbody');
    if (!tbody) return;

    commandRunCache.clear();
    for (const run of runs) {
        commandRunCache.set(Number(run.id || 0), run);
    }

    if (!runs.length) {
        tbody.innerHTML = '<tr><td colspan="7" style="color:var(--text-muted)">暂无执行流水</td></tr>';
        return;
    }

    tbody.innerHTML = runs.map(run => {
        const id = Number(run.id || 0);
        const cardKey = escapeHtml(run.card_key || '');
        const status = escapeHtml(run.status || '');
        const statusClass = classToken(run.status || '');
        const risk = escapeHtml(run.risk_level || 'normal');
        const riskClass = classToken(run.risk_level || 'normal');
        const requestedBy = escapeHtml(run.requested_by || '');
        const updatedAt = escapeHtml((run.updated_at || '').slice(0, 19).replace('T', ' '));
        return `<tr>
            <td style="font-family:var(--font-mono);font-size:0.75rem">${id}</td>
            <td style="font-family:var(--font-mono);font-size:0.75rem">${cardKey}</td>
            <td><span class="level-badge level-${statusClass}">${status}</span></td>
            <td><span class="level-badge level-${riskClass}">${risk}</span></td>
            <td>${requestedBy}</td>
            <td>${updatedAt}</td>
            <td>${_runActions(run)}</td>
        </tr>`;
    }).join('');
}

async function loadCommandCards() {
    const cardSelect = document.getElementById('cmd-card-key');
    if (!cardSelect) return;

    const r = await fetch('/api/command-cards?limit=200');
    const j = await r.json();
    if (j.ok) renderCommandCards(j.cards || []);
}

async function loadCommandRuns() {
    const tbody = document.getElementById('cmd-run-tbody');
    if (!tbody) return;

    const r = await fetch('/api/command-card-runs?limit=200');
    const j = await r.json();
    if (j.ok) renderCommandRuns(j.runs || []);
}

function _readCommandParams() {
    const area = document.getElementById('cmd-params');
    if (!area) return {};
    const text = String(area.value || '').trim();
    if (!text) return {};
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed;
    }
    throw new Error('参数必须是 JSON 对象');
}

async function submitCommandCardRun() {
    const cardKey = document.getElementById('cmd-card-key')?.value || '';
    const requestedBy = document.getElementById('cmd-requested-by')?.value || 'dashboard';
    const autoApprove = !!document.getElementById('cmd-auto-approve')?.checked;

    if (!cardKey) {
        toast('请先选择命令卡', false);
        return;
    }

    let params = {};
    try {
        params = _readCommandParams();
    } catch (e) {
        toast(e.message || '参数 JSON 解析失败', false);
        return;
    }

    try {
        const r = await fetch('/api/command-cards/execute', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                card_key: cardKey,
                params,
                requested_by: requestedBy,
                auto_approve: autoApprove,
            })
        });
        const j = await r.json();
        if (!j.ok) {
            toast(j.error || j.message || '提交失败', false);
            return;
        }

        const runId = j?.run?.id || 0;
        if (j.pending_review) {
            toast(`已创建审批单 run#${runId}，等待审核`, true);
        } else {
            toast(`执行完成 run#${runId} (${j?.run?.status || 'unknown'})`, true);
        }
        await refreshSections(['command_cards', 'audit', 'system']);
    } catch (e) {
        toast('网络错误: ' + e.message, false);
    }
}

async function reviewCommandRun(runId, decision) {
    try {
        const r = await fetch('/api/command-card-runs/review', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ run_id: runId, decision, reviewer: 'dashboard' }),
        });
        const j = await r.json();
        if (!j.ok) {
            toast(j.error || j.message || '审核失败', false);
            return;
        }
        toast(`run#${runId} 已${decision === 'approved' ? '批准' : '拒绝'}`, true);
        await refreshSections(['command_cards', 'audit', 'system']);
    } catch (e) {
        toast('网络错误: ' + e.message, false);
    }
}

async function executeCommandRun(runId) {
    try {
        const r = await fetch('/api/command-card-runs/execute', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ run_id: runId, actor: 'dashboard' }),
        });
        const j = await r.json();
        if (!j.ok) {
            toast(j.error || j.message || '执行失败', false);
            return;
        }
        toast(`run#${runId} 执行状态: ${j?.run?.status || 'unknown'}`, true);
        await refreshSections(['command_cards', 'audit', 'system']);
    } catch (e) {
        toast('网络错误: ' + e.message, false);
    }
}

function showCommandRunDetail(runId) {
    const run = commandRunCache.get(Number(runId));
    if (!run) {
        toast('未找到 run 详情', false);
        return;
    }
    const summary = [
        `run_id: ${run.id}`,
        `card_key: ${run.card_key}`,
        `status: ${run.status}`,
        `risk: ${run.risk_level}`,
        `exit_code: ${run.exit_code}`,
        `updated_at: ${run.updated_at}`,
        '',
        '[command]',
        run.rendered_command || '',
        '',
        '[stdout]',
        run.output || '',
        '',
        '[stderr]',
        run.error || '',
    ].join('\n');

    window.alert(summary);
}

/* ---- Refresh ---- */
async function loadApprovals() {
    const r = await fetch('/api/topology/approvals?status=pending');
    const j = await r.json();
    if (j.ok) renderApprovals(j.approvals || []);
}

async function refreshSections(scope = ['approvals', 'audit', 'system', 'command_cards', 'agent_status']) {
    const scopes = Array.isArray(scope) ? scope : ['approvals', 'audit', 'system', 'command_cards', 'agent_status'];
    const tasks = [];

    if (scopes.includes('approvals')) tasks.push(loadApprovals());
    if (scopes.includes('audit')) tasks.push(loadAuditLogs());
    if (scopes.includes('system')) tasks.push(loadSystemLogs());
    if (scopes.includes('command_cards')) {
        tasks.push(loadCommandCards());
        tasks.push(loadCommandRuns());
    }
    if (scopes.includes('prompts')) tasks.push(loadPrompts());
    if (scopes.includes('agent_status')) tasks.push(loadAgentStatus());

    try {
        if (tasks.length > 0) await Promise.all(tasks);
    } catch (e) {
        console.error('refresh error:', e);
    }
}

function startPollingFallback() {
    if (refreshTimer) return;
    refreshTimer = setInterval(() => refreshSections(['approvals', 'audit', 'system', 'command_cards', 'agent_status']), POLL_REFRESH_MS);
}

function stopPollingFallback() {
    if (!refreshTimer) return;
    clearInterval(refreshTimer);
    refreshTimer = null;
}

function scheduleReconnect() {
    if (reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        startEventStream();
    }, Math.max(1000, SSE_SYNC_MS));
}

function startEventStream() {
    if (typeof EventSource === 'undefined') {
        setLiveStatus('offline', '浏览器不支持 SSE，已降级轮询');
        startPollingFallback();
        return;
    }

    if (eventSource) {
        eventSource.close();
        eventSource = null;
    }

    setLiveStatus('pending');
    eventSource = new EventSource('/api/events/stream');

    eventSource.addEventListener('connected', (evt) => {
        stopPollingFallback();
        let syncSec = '';
        try {
            const data = JSON.parse(evt.data || '{}');
            syncSec = data?.payload?.sync_interval_sec ? `${data.payload.sync_interval_sec}s` : '';
        } catch (_) {
            syncSec = '';
        }
        setLiveStatus('online', syncSec);
    });

    eventSource.addEventListener('sync', async (evt) => {
        try {
            const data = JSON.parse(evt.data || '{}');
            const scope = Array.isArray(data?.payload?.scope)
                ? data.payload.scope
                : ['approvals', 'audit', 'system', 'command_cards', 'agent_status'];
            await refreshSections(scope);
        } catch (e) {
            console.error('SSE sync event parse failed:', e);
        }
    });

    eventSource.addEventListener('agent_status', (evt) => {
        try {
            const data = JSON.parse(evt.data || '{}');
            applyAgentStatusPayload(data.payload || {});
        } catch (e) {
            console.error('SSE agent_status event parse failed:', e);
        }
    });

    eventSource.onerror = () => {
        setLiveStatus('offline');
        startPollingFallback();
        if (eventSource && eventSource.readyState === 2) {
            eventSource.close();
            eventSource = null;
            scheduleReconnect();
        }
    };
}

/* ---- Init ---- */
document.addEventListener('DOMContentLoaded', () => {
    document.querySelectorAll('.nav-btn').forEach(btn => {
        btn.addEventListener('click', () => switchPage(btn.dataset.page));
    });

    ['audit-event-type', 'audit-action', 'audit-result', 'audit-actor'].forEach(id => {
        document.getElementById(id)?.addEventListener('change', loadAuditLogs);
    });
    ['system-level', 'system-logger'].forEach(id => {
        document.getElementById(id)?.addEventListener('change', loadSystemLogs);
    });
    document.getElementById('audit-keyword')?.addEventListener('keydown', e => {
        if (e.key === 'Enter') { e.preventDefault(); loadAuditLogs(); }
    });
    document.getElementById('system-keyword')?.addEventListener('keydown', e => {
        if (e.key === 'Enter') { e.preventDefault(); loadSystemLogs(); }
    });

    refreshSections(['approvals', 'audit', 'system', 'command_cards', 'agent_status']);
    loadPrompts();
    startEventStream();

    document.head.insertAdjacentHTML('beforeend', '<style>.hidden{display:none!important}</style>');
});

/* ---- Telegram Bot Management ---- */
async function tgRefresh() {
    try {
        const [infoRes, histRes] = await Promise.all([
            fetch('/api/tg/info').then(r => r.json()),
            fetch('/api/tg/history?limit=100').then(r => r.json()),
        ]);
        // Status badge
        const badge = document.getElementById('tg-running-badge');
        if (badge) {
            const running = infoRes.running;
            badge.className = 'badge ' + (running ? 'badge-green' : 'badge-gray');
            badge.textContent = '状态: ' + (running ? '运行中' : '已停止');
        }
        // Bot name
        const nameEl = document.getElementById('tg-bot-name');
        if (nameEl) {
            nameEl.textContent = infoRes.bot_username ? '@' + infoRes.bot_username : (infoRes.bot_name || '');
        }
        // Chat ID
        const chatEl = document.getElementById('tg-chat-id');
        if (chatEl) {
            chatEl.textContent = 'Chat ID: ' + (infoRes.chat_id || '未绑定');
        }
        // History
        renderTgChatLog(histRes.history || []);
    } catch (e) {
        console.error('tgRefresh error:', e);
    }
}

function renderTgChatLog(history) {
    const container = document.getElementById('tg-chat-log');
    if (!container) return;
    if (!history || history.length === 0) {
        container.innerHTML = '<div class="approval-empty">暂无对话记录</div>';
        return;
    }
    const html = history.map(item => {
        const ts = (item.ts || '').replace('T', ' ').substring(0, 19);
        const role = item.role || 'system';
        let roleLabel, roleColor;
        if (role === 'user') { roleLabel = '👤 用户'; roleColor = 'var(--blue)'; }
        else if (role === 'bot') { roleLabel = '🤖 Bot'; roleColor = 'var(--accent)'; }
        else { roleLabel = '⚙️ 系统'; roleColor = 'var(--text-muted)'; }

        const statusBadge = item.status === 'error'
            ? ' <span class="level-badge level-error">error</span>'
            : '';

        return `<div style="margin-bottom:10px;padding:8px 12px;border-radius:var(--radius-xs);background:var(--bg-surface);border-left:3px solid ${roleColor}">
            <div style="display:flex;justify-content:space-between;margin-bottom:4px">
                <span style="font-size:0.78rem;font-weight:600;color:${roleColor}">${roleLabel}${item.user ? ' (' + escapeHtml(item.user) + ')' : ''}${statusBadge}</span>
                <span style="font-size:0.68rem;color:var(--text-muted);font-family:var(--font-mono)">${ts}</span>
            </div>
            <div style="font-size:0.82rem;color:var(--text-primary);white-space:pre-wrap;word-break:break-word">${escapeHtml(item.text)}</div>
        </div>`;
    }).join('');
    container.innerHTML = html;
    container.scrollTop = container.scrollHeight;
}

async function tgStartBridge() {
    try {
        const res = await fetch('/api/tg/start', { method: 'POST' });
        const data = await res.json();
        toast(data.message || (data.ok ? '已启动' : '启动失败'), data.ok);
        setTimeout(tgRefresh, 1500);
    } catch (e) { toast('启动失败: ' + e.message, false); }
}

async function tgStopBridge() {
    try {
        const res = await fetch('/api/tg/stop', { method: 'POST' });
        const data = await res.json();
        toast(data.message || '已停止', data.ok);
        setTimeout(tgRefresh, 500);
    } catch (e) { toast('停止失败: ' + e.message, false); }
}

async function tgSendTest() {
    const input = document.getElementById('tg-test-input');
    const text = (input?.value || '').trim();
    if (!text) { toast('请输入消息内容', false); return; }
    try {
        const res = await fetch('/api/tg/send', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ text }),
        });
        const data = await res.json();
        toast(data.message || (data.ok ? '已发送' : '发送失败'), data.ok);
        if (data.ok) { input.value = ''; setTimeout(tgRefresh, 1000); }
    } catch (e) { toast('发送失败: ' + e.message, false); }
}

async function tgClearHistory() {
    try {
        const res = await fetch('/api/tg/clear-history', { method: 'POST' });
        const data = await res.json();
        toast(data.message || '已清空', data.ok);
        tgRefresh();
    } catch (e) { toast('清空失败: ' + e.message, false); }
}

// Auto-refresh TG when switching to telegram page
const _origSwitchPage = switchPage;
switchPage = function (pageId) {
    _origSwitchPage(pageId);
    if (pageId === 'telegram') { tgRefresh(); }
};
