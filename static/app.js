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

    // populate terminal agent selector — fetch ALL live sessions (incl. master)
    refreshTerminalSessions();
}

function refreshTerminalSessions() {
    const select = document.getElementById('terminal-agent-select');
    if (!select) return;
    fetch('/api/terminal/sessions')
        .then(r => r.json())
        .then(data => {
            if (!data.ok || !Array.isArray(data.sessions)) return;
            const currentVal = select.value;
            const options = '<option value="">选择会话...</option>' +
                data.sessions.map(s => {
                    const sid = s.session_id || '';
                    const label = s.name || s.agent_label || s.badge || s.session_name || sid.slice(0, 8);
                    const badge = s.badge ? `[${s.badge}] ` : '';
                    return `<option value="${escapeHtml(sid)}" ${sid === currentVal ? 'selected' : ''}>${badge}${escapeHtml(label)}</option>`;
                }).join('');
            select.innerHTML = options;
        })
        .catch(() => { });
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

/* ---- Prompts (Template Management + Popup) ---- */
let _promptRows = [];
let _popupTimer = null;
let _currentPopupData = null;
let _promptPopupHotkeysBound = false;

function _normalizePromptRows(rows) {
    return (rows || []).map((row) => {
        const tags = Array.isArray(row.tags) ? row.tags : [];
        return {
            promptKey: row.prompt_key || '',
            title: row.title || '',
            agentKey: row.agent_key || '',
            toolName: row.tool_name || '',
            promptText: row.prompt_text || '',
            variables: (row.variables && typeof row.variables === 'object' && !Array.isArray(row.variables)) ? row.variables : {},
            tags,
            enabled: !!row.enabled,
            updatedAt: row.updated_at || '',
            desc: String(row.prompt_text || '').replace(/\s+/g, ' ').slice(0, 100),
        };
    });
}

function _formatPromptTags(tags) {
    if (!Array.isArray(tags) || tags.length === 0) return '-';
    return tags.join(', ');
}

function _parseTagsInput(raw) {
    const text = String(raw || '').trim();
    if (!text) return [];
    if (text.startsWith('[')) {
        try {
            const parsed = JSON.parse(text);
            if (Array.isArray(parsed)) {
                return parsed.map(v => String(v || '').trim()).filter(Boolean);
            }
        } catch (_) {
            // fallback to comma split
        }
    }
    return text.split(',').map(v => v.trim()).filter(Boolean);
}

function _parseVariablesInput(raw) {
    const text = String(raw || '').trim();
    if (!text) return {};
    try {
        const parsed = JSON.parse(text);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            return parsed;
        }
        throw new Error('variables 必须是 JSON 对象');
    } catch (e) {
        throw new Error('变量定义必须是 JSON 对象');
    }
}

function _setPromptPopupFullscreenState(enabled) {
    const popup = document.getElementById('prompt-popup');
    const btn = document.getElementById('prompt-popup-fullscreen-btn');
    if (!popup) return;
    if (enabled) {
        popup.classList.add('fullscreen');
    } else {
        popup.classList.remove('fullscreen');
    }
    if (btn) {
        btn.textContent = enabled ? '退出全屏' : '全屏编辑';
    }
}

function _bindPromptPopupHotkeys() {
    if (_promptPopupHotkeysBound) return;
    document.addEventListener('keydown', (event) => {
        const popup = document.getElementById('prompt-popup');
        if (!popup || popup.style.display === 'none') return;

        if ((event.metaKey || event.ctrlKey) && String(event.key || '').toLowerCase() === 's') {
            event.preventDefault();
            savePromptPopup();
            return;
        }

        if (event.key === 'Escape') {
            event.preventDefault();
            closePromptPopup();
        }
    });
    _promptPopupHotkeysBound = true;
}

async function loadPrompts() {
    try {
        const kw = (document.getElementById('prompt-search')?.value || '').trim();
        const enabledOnly = !!document.getElementById('prompt-enabled-only')?.checked;
        const params = new URLSearchParams();
        params.set('limit', '500');
        if (kw) params.set('keyword', kw);
        if (enabledOnly) params.set('enabled_only', '1');

        const r = await fetch('/api/prompt-templates?' + params.toString());
        const j = await r.json();
        if (!j.ok) {
            toast(j.error || '加载模板失败', false);
            return;
        }

        _promptRows = _normalizePromptRows(j.templates || []);
        renderPromptTable(_promptRows);
    } catch (e) {
        console.error('loadPrompts error:', e);
        toast('加载模板失败: ' + e.message, false);
    }
}

function renderPromptTable(rows) {
    const tbody = document.getElementById('prompt-tbody');
    const empty = document.getElementById('prompt-empty');
    if (!tbody) return;

    if (!rows.length) {
        tbody.innerHTML = '';
        if (empty) {
            empty.style.display = '';
            empty.textContent = '暂无模板，可点击“导入常用模板”';
        }
        return;
    }
    if (empty) empty.style.display = 'none';

    tbody.innerHTML = rows.map((row, idx) => {
        const keyHtml = escapeHtml(row.promptKey);
        const titleHtml = escapeHtml(row.title || row.promptKey);
        const scopeHtml = escapeHtml(`${row.agentKey || '-'} / ${row.toolName || '-'}`);
        const tagsHtml = escapeHtml(_formatPromptTags(row.tags));
        const statusBadge = row.enabled
            ? '<span class="level-badge level-success">enabled</span>'
            : '<span class="level-badge level-disabled">disabled</span>';
        const updated = escapeHtml(row.updatedAt || '-');
        const rowBorder = row.enabled ? '' : ' style="opacity:.68"';
        return `<tr class="prompt-row" data-idx="${idx}"${rowBorder}
                    onmouseenter="showPromptPopupDelayed(event,${idx})"
                    onmouseleave="hidePromptPopupDelayed()">
            <td style="font-family:var(--font-mono);font-size:0.76rem;color:var(--accent)">${keyHtml}</td>
            <td style="font-size:0.78rem">${titleHtml}</td>
            <td style="font-family:var(--font-mono);font-size:0.74rem;color:var(--text-secondary)">${scopeHtml}</td>
            <td style="font-size:0.74rem;color:var(--text-secondary);max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${tagsHtml}</td>
            <td>${statusBadge}</td>
            <td style="font-size:0.74rem;color:var(--text-secondary)">${updated}</td>
            <td style="text-align:center;white-space:nowrap">
                <button class="btn btn-sm btn-secondary" onclick="event.stopPropagation();openPromptPopup(${idx})" title="编辑" style="cursor:pointer">编辑</button>
                <button class="btn btn-sm btn-secondary" onclick="event.stopPropagation();copyPromptDirect(${idx})" title="复制" style="cursor:pointer;margin-left:4px">复制</button>
                <button class="btn btn-sm btn-secondary" onclick="event.stopPropagation();togglePromptEnabled(${idx})" title="启停" style="cursor:pointer;margin-left:4px">${row.enabled ? '禁用' : '启用'}</button>
            </td>
        </tr>`;
    }).join('');
}

function filterPromptTable() {
    loadPrompts();
}

function copyPromptDirect(idx) {
    const row = _promptRows[idx];
    if (!row) return;
    navigator.clipboard.writeText(row.promptText || '').then(() => toast('已复制到剪贴板', true));
}

function _showPromptPopup(row, popupTitle) {
    const popup = document.getElementById('prompt-popup');
    const titleEl = document.getElementById('prompt-popup-title');
    const keyInput = document.getElementById('prompt-popup-key');
    const titleInput = document.getElementById('prompt-popup-title-input');
    const agentInput = document.getElementById('prompt-popup-agent-key');
    const toolInput = document.getElementById('prompt-popup-tool-name');
    const tagsInput = document.getElementById('prompt-popup-tags');
    const enabledInput = document.getElementById('prompt-popup-enabled');
    const varsInput = document.getElementById('prompt-popup-variables');
    const textInput = document.getElementById('prompt-popup-textarea');

    if (!popup || !titleEl || !keyInput || !titleInput || !agentInput || !toolInput || !tagsInput || !enabledInput || !varsInput || !textInput) {
        return;
    }

    titleEl.textContent = popupTitle;
    keyInput.value = row.promptKey || '';
    titleInput.value = row.title || '';
    agentInput.value = row.agentKey || '';
    toolInput.value = row.toolName || '';
    tagsInput.value = Array.isArray(row.tags) ? row.tags.join(',') : '';
    enabledInput.checked = !!row.enabled;
    varsInput.value = JSON.stringify(row.variables || {}, null, 2);
    textInput.value = row.promptText || '';

    _bindPromptPopupHotkeys();
    _setPromptPopupFullscreenState(false);
    popup.style.display = 'flex';

    const savedW = localStorage.getItem('prompt_popup_w');
    const savedH = localStorage.getItem('prompt_popup_h');
    if (savedW) popup.style.width = savedW + 'px';
    if (savedH) popup.style.height = savedH + 'px';

    if (!popup._resizeObs) {
        popup._resizeObs = new ResizeObserver(() => {
            if (popup.style.display !== 'none') {
                localStorage.setItem('prompt_popup_w', popup.offsetWidth);
                localStorage.setItem('prompt_popup_h', popup.offsetHeight);
            }
        });
        popup._resizeObs.observe(popup);
    }
}

function showPromptPopupDelayed(event, idx) {
    clearTimeout(_popupTimer);
    _popupTimer = setTimeout(() => _showPopupAtRow(event.currentTarget, idx), 350);
}

function hidePromptPopupDelayed() {
    clearTimeout(_popupTimer);
    _popupTimer = setTimeout(() => {
        const popup = document.getElementById('prompt-popup');
        if (popup && !popup.matches(':hover')) popup.style.display = 'none';
    }, 400);
}

function _showPopupAtRow(rowEl, idx) {
    const row = _promptRows[idx];
    if (!row) return;
    _currentPopupData = row;
    _showPromptPopup(row, `${row.promptKey || '提示词模板'}（编辑）`);

    const popup = document.getElementById('prompt-popup');
    if (!popup) return;
    const rect = rowEl.getBoundingClientRect();
    const popupW = popup.offsetWidth || 700;
    const popupH = popup.offsetHeight || 620;
    let left = rect.right + 12;
    let top = rect.top;
    if (left + popupW > window.innerWidth) left = rect.left - popupW - 12;
    if (top + popupH > window.innerHeight) top = window.innerHeight - popupH - 16;
    if (top < 8) top = 8;
    if (left < 8) left = 8;
    popup.style.left = left + 'px';
    popup.style.top = top + 'px';
}

function openPromptPopup(idx) {
    clearTimeout(_popupTimer);
    const rowEl = document.querySelector(`.prompt-row[data-idx="${idx}"]`);
    if (rowEl) _showPopupAtRow(rowEl, idx);
    setTimeout(() => {
        const ta = document.getElementById('prompt-popup-textarea');
        if (ta) ta.focus();
    }, 100);
}

function openPromptCreatePopup() {
    clearTimeout(_popupTimer);
    _currentPopupData = null;
    const popup = document.getElementById('prompt-popup');
    if (!popup) return;

    _showPromptPopup({
        promptKey: '',
        title: '',
        agentKey: 'master',
        toolName: 'task',
        tags: ['preset'],
        enabled: true,
        variables: {},
        promptText: '',
    }, '新建提示词模板');

    const popupW = popup.offsetWidth || 700;
    const popupH = popup.offsetHeight || 620;
    popup.style.left = Math.max(8, Math.round((window.innerWidth - popupW) / 2)) + 'px';
    popup.style.top = Math.max(8, Math.round((window.innerHeight - popupH) / 2)) + 'px';
}

function openPromptPastePopup() {
    openPromptCreatePopup();
    const titleInput = document.getElementById('prompt-popup-title-input');
    const tagsInput = document.getElementById('prompt-popup-tags');
    const textInput = document.getElementById('prompt-popup-textarea');
    if (titleInput && !titleInput.value.trim()) {
        titleInput.value = '快速粘贴模板';
    }
    if (tagsInput && !tagsInput.value.trim()) {
        tagsInput.value = 'custom,paste';
    }
    setTimeout(() => {
        if (textInput) {
            textInput.focus();
        }
    }, 60);
}

function togglePromptPopupFullscreen() {
    const popup = document.getElementById('prompt-popup');
    if (!popup) return;
    _setPromptPopupFullscreenState(!popup.classList.contains('fullscreen'));
}

function closePromptPopup() {
    clearTimeout(_popupTimer);
    const popup = document.getElementById('prompt-popup');
    if (popup) {
        popup.style.display = 'none';
    }
    _setPromptPopupFullscreenState(false);
    _currentPopupData = null;
}

function copyPromptPopup() {
    const ta = document.getElementById('prompt-popup-textarea');
    if (!ta) return;
    navigator.clipboard.writeText(ta.value).then(() => {
        const ok = document.getElementById('prompt-popup-copy-ok');
        if (ok) {
            ok.classList.add('show');
            setTimeout(() => ok.classList.remove('show'), 1500);
        }
    });
}

async function savePromptPopup(closeAfter = false) {
    const keyInput = document.getElementById('prompt-popup-key');
    const titleInput = document.getElementById('prompt-popup-title-input');
    const agentInput = document.getElementById('prompt-popup-agent-key');
    const toolInput = document.getElementById('prompt-popup-tool-name');
    const tagsInput = document.getElementById('prompt-popup-tags');
    const enabledInput = document.getElementById('prompt-popup-enabled');
    const varsInput = document.getElementById('prompt-popup-variables');
    const textInput = document.getElementById('prompt-popup-textarea');
    if (!keyInput || !titleInput || !agentInput || !toolInput || !tagsInput || !enabledInput || !varsInput || !textInput) return;

    let variables = {};
    try {
        variables = _parseVariablesInput(varsInput.value);
    } catch (e) {
        toast(e.message, false);
        return;
    }

    const payload = {
        prompt_key: keyInput.value.trim(),
        title: titleInput.value.trim(),
        agent_key: agentInput.value.trim(),
        tool_name: toolInput.value.trim(),
        tags: _parseTagsInput(tagsInput.value),
        enabled: !!enabledInput.checked,
        variables,
        prompt_text: textInput.value,
        updated_by: 'dashboard',
    };

    try {
        const r = await fetch('/api/prompt-templates', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        const j = await r.json();
        if (j.ok) {
            toast('模板已保存', true);
            await loadPrompts();
            _currentPopupData = j.prompt || null;
            if (closeAfter) {
                closePromptPopup();
            }
        } else {
            toast(j.error || '保存失败', false);
        }
    } catch (e) {
        toast('保存失败: ' + e.message, false);
    }
}

async function togglePromptEnabled(idx) {
    const row = _promptRows[idx];
    if (!row) return;
    try {
        const r = await fetch('/api/prompt-templates/toggle', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                prompt_key: row.promptKey,
                enabled: !row.enabled,
                updated_by: 'dashboard',
            }),
        });
        const j = await r.json();
        if (j.ok) {
            toast(`模板已${!row.enabled ? '启用' : '禁用'}`, true);
            await loadPrompts();
        } else {
            toast(j.error || '状态更新失败', false);
        }
    } catch (e) {
        toast('状态更新失败: ' + e.message, false);
    }
}

async function seedPromptTemplates(overwrite) {
    try {
        const r = await fetch('/api/prompt-templates/seed', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ overwrite: !!overwrite, updated_by: 'dashboard' }),
        });
        const j = await r.json();
        if (j.ok) {
            toast(`模板导入完成：新增${j.inserted || 0}，更新${j.updated || 0}，跳过${j.skipped || 0}`, true);
            await loadPrompts();
        } else {
            toast(j.error || '导入失败', false);
        }
    } catch (e) {
        toast('导入失败: ' + e.message, false);
    }
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

    eventSource.addEventListener('tg_message', (evt) => {
        try {
            const data = JSON.parse(evt.data || '{}');
            const msg = data.payload;
            if (msg && msg.role) appendTgMessage(msg);
        } catch (e) {
            console.error('SSE tg_message parse failed:', e);
        }
    });

    eventSource.addEventListener('terminal_output', (evt) => {
        try {
            const data = JSON.parse(evt.data || '{}');
            const p = data.payload || {};
            if (p.session_id && Array.isArray(p.lines)) {
                appendTerminalLines(p.session_id, p.lines);
            }
        } catch (e) {
            console.error('SSE terminal_output parse failed:', e);
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
    container.innerHTML = history.map(item => _renderTgMessageHtml(item)).join('');
    container.scrollTop = container.scrollHeight;
}

function _renderTgMessageHtml(item) {
    const ts = (item.ts || '').replace('T', ' ').substring(0, 19);
    const role = item.role || 'system';
    let roleLabel, roleColor;
    if (role === 'user') { roleLabel = '👤 用户'; roleColor = 'var(--blue)'; }
    else if (role === 'bot') { roleLabel = '🤖 Bot'; roleColor = 'var(--accent)'; }
    else { roleLabel = '⚙️ 系统'; roleColor = 'var(--text-muted)'; }
    const statusBadge = item.status === 'error'
        ? ' <span class="level-badge level-error">error</span>' : '';
    return `<div style="margin-bottom:10px;padding:8px 12px;border-radius:var(--radius-xs);background:var(--bg-surface);border-left:3px solid ${roleColor};animation:fadeInMsg .25s ease">
        <div style="display:flex;justify-content:space-between;margin-bottom:4px">
            <span style="font-size:0.78rem;font-weight:600;color:${roleColor}">${roleLabel}${item.user ? ' (' + escapeHtml(item.user) + ')' : ''}${statusBadge}</span>
            <span style="font-size:0.68rem;color:var(--text-muted);font-family:var(--font-mono)">${ts}</span>
        </div>
        <div style="font-size:0.82rem;color:var(--text-primary);white-space:pre-wrap;word-break:break-word">${escapeHtml(item.text)}</div>
    </div>`;
}

function appendTgMessage(item) {
    const container = document.getElementById('tg-chat-log');
    if (!container) return;
    // remove "暂无" placeholder if present
    const empty = container.querySelector('.approval-empty');
    if (empty) empty.remove();
    container.insertAdjacentHTML('beforeend', _renderTgMessageHtml(item));
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

// Auto-refresh TG when switching to telegram page + configurable poll
let _tgAutoTimer = null;
const _origSwitchPage = switchPage;
switchPage = function (pageId) {
    _origSwitchPage(pageId);
    clearInterval(_tgAutoTimer);
    _tgAutoTimer = null;
    if (pageId === 'telegram') {
        tgRefresh();
        const sec = window.__TG_AUTO_REFRESH_SEC || 60;
        if (sec > 0) _tgAutoTimer = setInterval(tgRefresh, sec * 1000);
    }
};

// ── Terminal Live Viewer ──────────────────────────────────────────────

let _termMode = 'stream';         // 'stream' | 'stream-cmd' | 'stream-cmd-snap'
let _termSessionId = '';
let _termStreaming = false;
let _termSnapTimer = null;
const _TERM_MAX_LINES = 500;

function switchTerminalMode(mode) {
    _termMode = mode;
    document.querySelectorAll('.terminal-mode-btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.mode === mode);
    });

    const cmdBar = document.getElementById('terminal-cmd-bar');
    if (cmdBar) cmdBar.style.display = (mode === 'stream') ? 'none' : 'flex';

    // manage snapshot timer
    clearInterval(_termSnapTimer);
    _termSnapTimer = null;
    if (mode === 'stream-cmd-snap' && _termSessionId) {
        _termSnapTimer = setInterval(termReadSnapshot, 2000);
    }
}

function onTerminalAgentChange() {
    const select = document.getElementById('terminal-agent-select');
    const sessionId = select ? select.value : '';

    // stop previous stream
    if (_termSessionId && _termStreaming) {
        fetch('/api/terminal/stream/stop', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: _termSessionId }),
        }).catch(() => { });
    }
    _termStreaming = false;
    _termSessionId = sessionId;
    clearInterval(_termSnapTimer);
    _termSnapTimer = null;

    const output = document.getElementById('terminal-output');
    if (output) output.innerHTML = '<span style="color:var(--text-muted)">连接中...</span>';

    const statusBadge = document.getElementById('terminal-stream-status');

    if (!sessionId) {
        if (output) output.innerHTML = '<span style="color:var(--text-muted)">选择 Agent 后开始实时推流...</span>';
        if (statusBadge) { statusBadge.className = 'badge badge-gray'; statusBadge.textContent = '未连接'; }
        return;
    }

    // start stream
    fetch('/api/terminal/stream/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId }),
    }).then(r => r.json()).then(data => {
        if (data.ok) {
            _termStreaming = true;
            if (statusBadge) { statusBadge.className = 'badge badge-green'; statusBadge.textContent = '推流中'; }
            // do initial read
            termReadSnapshot();
            // start snapshot timer for mode 3
            if (_termMode === 'stream-cmd-snap') {
                _termSnapTimer = setInterval(termReadSnapshot, 2000);
            }
        } else {
            if (statusBadge) { statusBadge.className = 'badge badge-red'; statusBadge.textContent = '连接失败'; }
            if (output) output.innerHTML = '<span style="color:var(--red)">连接失败: ' + escapeHtml(data.error || '') + '</span>';
        }
    }).catch(e => {
        if (statusBadge) { statusBadge.className = 'badge badge-red'; statusBadge.textContent = '错误'; }
    });
}

function termReadSnapshot() {
    if (!_termSessionId) return;
    fetch('/api/terminal/read?session_id=' + encodeURIComponent(_termSessionId) + '&lines=60')
        .then(r => r.json())
        .then(data => {
            if (data.ok && data.lines) {
                const output = document.getElementById('terminal-output');
                if (output) {
                    const wasNearBottom = output.scrollHeight - output.scrollTop - output.clientHeight < 50;
                    output.textContent = data.lines.join('\n');
                    if (wasNearBottom) output.scrollTop = output.scrollHeight;
                }
            }
        })
        .catch(() => { });
}

function termSendCommand() {
    const input = document.getElementById('terminal-cmd-input');
    if (!input || !_termSessionId) return;
    const text = input.value;
    if (!text) return;

    fetch('/api/terminal/send', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: _termSessionId, text: text + '\n' }),
    }).then(r => r.json()).then(data => {
        if (data.ok) {
            input.value = '';
            // read snapshot after short delay to see result
            setTimeout(termReadSnapshot, 500);
        } else {
            toast('发送失败: ' + (data.error || ''), false);
        }
    }).catch(e => toast('发送失败: ' + e.message, false));
}

function appendTerminalLines(sessionId, lines) {
    if (sessionId !== _termSessionId) return;
    const output = document.getElementById('terminal-output');
    if (!output) return;

    // if it has the placeholder, clear it
    const placeholder = output.querySelector('span');
    if (placeholder && placeholder.style.color) output.innerHTML = '';

    // only auto-scroll if user is near bottom
    const wasNearBottom = output.scrollHeight - output.scrollTop - output.clientHeight < 50;
    const text = lines.join('\n');
    output.textContent += (output.textContent ? '\n' : '') + text;

    // trim if too long
    const allLines = output.textContent.split('\n');
    if (allLines.length > _TERM_MAX_LINES) {
        output.textContent = allLines.slice(-_TERM_MAX_LINES).join('\n');
    }

    if (wasNearBottom) output.scrollTop = output.scrollHeight;
}

// Enter key for command input
document.addEventListener('DOMContentLoaded', () => {
    const cmdInput = document.getElementById('terminal-cmd-input');
    if (cmdInput) {
        cmdInput.addEventListener('keydown', e => {
            if (e.key === 'Enter') { e.preventDefault(); termSendCommand(); }
        });
    }
});
