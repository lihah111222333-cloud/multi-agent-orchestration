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

/** 自定义确认弹窗（替代 window.confirm，避免自动刷新导致一闪而过） */
function showConfirm(msg) {
    return new Promise((resolve) => {
        _confirmDialogActive = true;
        const overlay = document.createElement('div');
        overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,.55);display:flex;align-items:center;justify-content:center;z-index:9999';
        const box = document.createElement('div');
        box.style.cssText = 'background:var(--card-bg,#1e293b);border:1px solid var(--border,#334155);border-radius:12px;padding:28px 32px 20px;max-width:420px;width:90%;color:var(--text-primary,#e2e8f0);font-size:0.95rem;box-shadow:0 8px 32px rgba(0,0,0,.4)';
        box.innerHTML = `<div style="margin-bottom:20px;line-height:1.6">${escapeHtml(msg)}</div>
            <div style="display:flex;gap:12px;justify-content:flex-end">
                <button id="_confirm-no" class="btn btn-sm btn-secondary" style="min-width:64px">取消</button>
                <button id="_confirm-yes" class="btn btn-sm btn-danger" style="min-width:64px">确认</button>
            </div>`;
        overlay.appendChild(box);
        document.body.appendChild(overlay);
        const cleanup = (val) => { _confirmDialogActive = false; overlay.remove(); resolve(val); };
        box.querySelector('#_confirm-yes').onclick = () => cleanup(true);
        box.querySelector('#_confirm-no').onclick = () => cleanup(false);
        overlay.addEventListener('click', (e) => { if (e.target === overlay) cleanup(false); });
    });
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
    const chips = document.querySelectorAll('.agent-chip[data-agent-id], .agent-card[data-agent-id], .agent-row[data-agent-id]');
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
    const chips = document.querySelectorAll('.agent-chip[data-agent-id], .agent-card[data-agent-id], .agent-row[data-agent-id]');
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

    document.querySelectorAll('.agent-chip[data-agent-id], .agent-card[data-agent-id], .agent-row[data-agent-id]').forEach((chip) => {
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
let _selectedPromptKeys = new Set();
let _popupTimer = null;
let _currentPopupData = null;
let _promptPopupHotkeysBound = false;

function _normalizePromptRows(rows) {
    return (rows || []).map((row) => {
        const tags = Array.isArray(row.tags) ? row.tags : [];
        return {
            promptKey: row.prompt_key || '',
            title: row.title || '',
            description: row.description || '',
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
        const rawKey = row.promptKey;
        const keyHtml = escapeHtml(rawKey);
        const titleHtml = escapeHtml(row.title || row.promptKey);
        const scopeHtml = escapeHtml(`${row.agentKey || '-'} / ${row.toolName || '-'}`);
        const tagsHtml = escapeHtml(_formatPromptTags(row.tags));
        const statusBadge = row.enabled
            ? '<span class="level-badge level-success">enabled</span>'
            : '<span class="level-badge level-disabled">disabled</span>';
        const updated = escapeHtml(row.updatedAt || '-');
        const rowBorder = row.enabled ? '' : ' style="opacity:.68"';
        const descHtml = escapeHtml(row.description || '').slice(0, 60);
        const checked = _selectedPromptKeys.has(rawKey) ? 'checked' : '';
        return `<tr class="prompt-row" data-idx="${idx}"${rowBorder}
                    onclick="copyPromptDirect(${idx})"
                    ondblclick="sendPromptToMaster(${idx})">
            <td style="width:34px;text-align:center"><input type="checkbox" class="prompt-select" data-prompt-key="${keyHtml}" ${checked} onclick="event.stopPropagation()" onchange="togglePromptCheck('${keyHtml}',this.checked)"></td>
            <td style="font-family:var(--font-mono);font-size:0.76rem;color:var(--accent)">${keyHtml}</td>
            <td style="font-size:0.78rem">${titleHtml}</td>
            <td style="font-size:0.74rem;color:var(--text-secondary);max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${descHtml}">${descHtml || '<span style="opacity:.3">—</span>'}</td>
            <td style="font-family:var(--font-mono);font-size:0.74rem;color:var(--text-secondary)">${scopeHtml}</td>
            <td style="font-size:0.74rem;color:var(--text-secondary);max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${tagsHtml}</td>
            <td>${statusBadge}</td>
            <td style="font-size:0.74rem;color:var(--text-secondary)">${updated}</td>
            <td style="text-align:center;white-space:nowrap">
                <button class="btn btn-sm btn-secondary" onclick="event.stopPropagation();openPromptPopup(${idx})" title="编辑" style="cursor:pointer">编辑</button>
                <button class="btn btn-sm btn-secondary" onclick="event.stopPropagation();togglePromptEnabled(${idx})" title="启停" style="cursor:pointer;margin-left:4px">${row.enabled ? '禁用' : '启用'}</button>
            </td>
        </tr>`;
    }).join('');
}

function togglePromptCheck(key, checked) {
    if (checked) _selectedPromptKeys.add(key);
    else _selectedPromptKeys.delete(key);
}

function togglePromptSelectAll(checked) {
    _selectedPromptKeys = new Set();
    if (checked) _promptRows.forEach(r => _selectedPromptKeys.add(r.promptKey));
    document.querySelectorAll('.prompt-select').forEach(cb => cb.checked = checked);
}

async function deleteSelectedPrompts() {
    const keys = Array.from(_selectedPromptKeys);
    if (!keys.length) { toast('请先勾选要删除的提示词', false); return; }

    const confirmed = await showConfirm(`确认删除已勾选的 ${keys.length} 条提示词？`);
    if (!confirmed) return;

    try {
        const r = await fetch('/api/prompt-templates/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ prompt_keys: keys, updated_by: 'dashboard' }),
        });
        const j = await r.json();
        if (!j.ok) { toast(j.error || '删除失败', false); return; }
        _selectedPromptKeys = new Set();
        toast(`已删除 ${j.deleted || 0} 条提示词`, true);
        await loadPrompts();
    } catch (e) { toast('删除失败: ' + e.message, false); }
}

function filterPromptTable() {
    loadPrompts();
}

function copyPromptDirect(idx) {
    const row = _promptRows[idx];
    if (!row) return;
    navigator.clipboard.writeText(row.promptText || '').then(() => toast('已复制到剪贴板', true));
}

async function sendPromptToMaster(idx) {
    const row = _promptRows[idx];
    if (!row || !row.promptText) return;
    try {
        const r = await fetch('/api/session/send', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                agent_key: 'master',
                text: row.promptText,
            }),
        });
        const j = await r.json();
        if (j.ok) {
            toast('已下发到主 Agent', true);
        } else {
            // fallback: copy to clipboard
            await navigator.clipboard.writeText(row.promptText);
            toast(j.error || '下发失败，已复制到剪贴板', false);
        }
    } catch (e) {
        await navigator.clipboard.writeText(row.promptText);
        toast('下发接口不可用，已复制到剪贴板', false);
    }
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
    const descInput = document.getElementById('prompt-popup-description');
    if (descInput) descInput.value = row.description || '';
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

    // enable drag-to-move on header
    if (!popup._dragBound) {
        const header = popup.querySelector('.prompt-popup-header');
        if (header) {
            header.style.cursor = 'move';
            let dragging = false, startX, startY, origLeft, origTop;
            header.addEventListener('mousedown', (e) => {
                if (e.target.tagName === 'BUTTON' || e.target.tagName === 'INPUT') return;
                dragging = true;
                startX = e.clientX;
                startY = e.clientY;
                origLeft = popup.offsetLeft;
                origTop = popup.offsetTop;
                e.preventDefault();
            });
            document.addEventListener('mousemove', (e) => {
                if (!dragging) return;
                popup.style.left = (origLeft + e.clientX - startX) + 'px';
                popup.style.top = (origTop + e.clientY - startY) + 'px';
            });
            document.addEventListener('mouseup', () => { dragging = false; });
        }
        popup._dragBound = true;
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
    const row = _promptRows[idx];
    if (!row) return;
    _currentPopupData = row;
    _showPromptPopup(row, `${row.promptKey || '提示词模板'}（编辑）`);

    const popup = document.getElementById('prompt-popup');
    if (!popup) return;
    // Stable center position (right-center of viewport)
    const popupW = popup.offsetWidth || 700;
    const popupH = popup.offsetHeight || 620;
    popup.style.left = Math.max(8, Math.round((window.innerWidth - popupW) / 2 + 100)) + 'px';
    popup.style.top = Math.max(8, Math.round((window.innerHeight - popupH) / 2)) + 'px';

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
        description: '',
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
        description: (document.getElementById('prompt-popup-description') || {}).value || '',
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
let _commandCardRows = [];
let _selectedCommandCardKeys = new Set();
let _currentCommandPopupData = null;
let _commandPopupHotkeysBound = false;
let _confirmDialogActive = false;

function _normalizeCommandCardRows(rows) {
    return (rows || []).map((row) => {
        const argsSchema = (row.args_schema && typeof row.args_schema === 'object' && !Array.isArray(row.args_schema))
            ? row.args_schema
            : {};
        return {
            cardKey: row.card_key || '',
            title: row.title || '',
            description: row.description || '',
            commandTemplate: row.command_template || '',
            argsSchema,
            riskLevel: row.risk_level || 'normal',
            enabled: !!row.enabled,
            updatedAt: row.updated_at || '',
            lastRunAt: row.last_run_at || '',
            runCount: Number(row.run_count || 0),
        };
    });
}

function _toUTC8(date) {
    // Force UTC+8 regardless of browser timezone
    const utcMs = date.getTime() + date.getTimezoneOffset() * 60000;
    return new Date(utcMs + 8 * 3600000);
}

function _formatCommandCardTime(raw) {
    const text = String(raw || '').trim();
    if (!text) return '-';

    // 已经是后端格式的 2026-2-11:20:30 直接返回
    const m = text.match(/^(\d{4})-(\d{1,2})-(\d{1,2}):(\d{1,2}):(\d{1,2})/);
    if (m) return text;

    const parsed = new Date(text);
    if (!Number.isNaN(parsed.getTime())) {
        const d8 = _toUTC8(parsed);
        return `${d8.getFullYear()}-${d8.getMonth() + 1}-${d8.getDate()} ${String(d8.getHours()).padStart(2, '0')}:${String(d8.getMinutes()).padStart(2, '0')}`;
    }

    return text;
}

function _parseCommandArgsSchemaInput(raw) {
    const text = String(raw || '').trim();
    if (!text) return {};
    try {
        const parsed = JSON.parse(text);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            return parsed;
        }
        throw new Error('args_schema 必须是 JSON 对象');
    } catch (e) {
        throw new Error('参数定义必须是 JSON 对象');
    }
}

function _setCommandPopupFullscreenState(enabled) {
    const popup = document.getElementById('cmd-popup');
    const btn = document.getElementById('cmd-popup-fullscreen-btn');
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

function _bindCommandPopupHotkeys() {
    if (_commandPopupHotkeysBound) return;
    document.addEventListener('keydown', (event) => {
        const popup = document.getElementById('cmd-popup');
        if (!popup || popup.style.display === 'none') return;

        if ((event.metaKey || event.ctrlKey) && String(event.key || '').toLowerCase() === 's') {
            event.preventDefault();
            saveCommandPopup();
            return;
        }

        if (event.key === 'Escape') {
            event.preventDefault();
            closeCommandPopup();
        }
    });
    _commandPopupHotkeysBound = true;
}

function _showCommandPopup(row, popupTitle) {
    const popup = document.getElementById('cmd-popup');
    const titleEl = document.getElementById('cmd-popup-title');
    const keyInput = document.getElementById('cmd-popup-key');
    const titleInput = document.getElementById('cmd-popup-title-input');
    const riskInput = document.getElementById('cmd-popup-risk');
    const enabledInput = document.getElementById('cmd-popup-enabled');
    const descInput = document.getElementById('cmd-popup-desc');
    const schemaInput = document.getElementById('cmd-popup-args-schema');
    const commandInput = document.getElementById('cmd-popup-textarea');

    if (!popup || !titleEl || !keyInput || !titleInput || !riskInput || !enabledInput || !descInput || !schemaInput || !commandInput) {
        return;
    }

    titleEl.textContent = popupTitle;
    keyInput.value = row.cardKey || '';
    titleInput.value = row.title || '';
    riskInput.value = row.riskLevel || 'normal';
    enabledInput.checked = !!row.enabled;
    descInput.value = row.description || '';
    schemaInput.value = JSON.stringify(row.argsSchema || {}, null, 2);
    commandInput.value = row.commandTemplate || '';

    _bindCommandPopupHotkeys();
    _setCommandPopupFullscreenState(false);
    popup.style.display = 'flex';

    const savedW = localStorage.getItem('command_popup_w');
    const savedH = localStorage.getItem('command_popup_h');
    if (savedW) popup.style.width = savedW + 'px';
    if (savedH) popup.style.height = savedH + 'px';

    if (!popup._resizeObs) {
        popup._resizeObs = new ResizeObserver(() => {
            if (popup.style.display !== 'none') {
                localStorage.setItem('command_popup_w', popup.offsetWidth);
                localStorage.setItem('command_popup_h', popup.offsetHeight);
            }
        });
        popup._resizeObs.observe(popup);
    }

    if (!popup._dragBound) {
        const header = popup.querySelector('.command-popup-header');
        if (header) {
            header.style.cursor = 'move';
            let dragging = false;
            let startX = 0;
            let startY = 0;
            let origLeft = 0;
            let origTop = 0;

            header.addEventListener('mousedown', (event) => {
                if (event.target.tagName === 'BUTTON' || event.target.tagName === 'INPUT') return;
                dragging = true;
                startX = event.clientX;
                startY = event.clientY;
                origLeft = popup.offsetLeft;
                origTop = popup.offsetTop;
                event.preventDefault();
            });

            document.addEventListener('mousemove', (event) => {
                if (!dragging) return;
                popup.style.left = (origLeft + event.clientX - startX) + 'px';
                popup.style.top = (origTop + event.clientY - startY) + 'px';
            });

            document.addEventListener('mouseup', () => {
                dragging = false;
            });
        }
        popup._dragBound = true;
    }
}

function openCommandPopup(idx) {
    const row = _commandCardRows[idx];
    if (!row) return;

    _currentCommandPopupData = row;
    _showCommandPopup(row, `${row.cardKey || '命令卡'}（编辑）`);

    const popup = document.getElementById('cmd-popup');
    if (!popup) return;
    const popupW = popup.offsetWidth || 760;
    const popupH = popup.offsetHeight || 640;
    popup.style.left = Math.max(8, Math.round((window.innerWidth - popupW) / 2 + 100)) + 'px';
    popup.style.top = Math.max(8, Math.round((window.innerHeight - popupH) / 2)) + 'px';

    setTimeout(() => {
        const ta = document.getElementById('cmd-popup-textarea');
        if (ta) ta.focus();
    }, 100);
}

function openCommandCreatePopup() {
    _currentCommandPopupData = null;
    const popup = document.getElementById('cmd-popup');
    if (!popup) return;

    _showCommandPopup({
        cardKey: '',
        title: '',
        description: '',
        commandTemplate: '',
        argsSchema: {},
        riskLevel: 'normal',
        enabled: true,
    }, '新建命令卡');

    const popupW = popup.offsetWidth || 760;
    const popupH = popup.offsetHeight || 640;
    popup.style.left = Math.max(8, Math.round((window.innerWidth - popupW) / 2)) + 'px';
    popup.style.top = Math.max(8, Math.round((window.innerHeight - popupH) / 2)) + 'px';
}

function openCommandPastePopup() {
    openCommandCreatePopup();
    const titleInput = document.getElementById('cmd-popup-title-input');
    const descInput = document.getElementById('cmd-popup-desc');
    const commandInput = document.getElementById('cmd-popup-textarea');
    if (titleInput && !titleInput.value.trim()) {
        titleInput.value = '快速粘贴命令卡';
    }
    if (descInput && !descInput.value.trim()) {
        descInput.value = '从后台快速粘贴创建';
    }
    setTimeout(() => {
        if (commandInput) commandInput.focus();
    }, 60);
}

function toggleCommandPopupFullscreen() {
    const popup = document.getElementById('cmd-popup');
    if (!popup) return;
    _setCommandPopupFullscreenState(!popup.classList.contains('fullscreen'));
}

function closeCommandPopup() {
    const popup = document.getElementById('cmd-popup');
    if (popup) {
        popup.style.display = 'none';
    }
    _setCommandPopupFullscreenState(false);
    _currentCommandPopupData = null;
}

function copyCommandPopup() {
    const ta = document.getElementById('cmd-popup-textarea');
    if (!ta) return;
    navigator.clipboard.writeText(ta.value).then(() => {
        const ok = document.getElementById('cmd-popup-copy-ok');
        if (ok) {
            ok.classList.add('show');
            setTimeout(() => ok.classList.remove('show'), 1500);
        }
    });
}

function copyCommandDirect(idx) {
    const row = _commandCardRows[idx];
    if (!row) return;
    navigator.clipboard.writeText(row.commandTemplate || '').then(() => toast('命令模板已复制', true));
}

async function sendCommandToMaster(idx) {
    const row = _commandCardRows[idx];
    if (!row || !row.commandTemplate) return;
    try {
        const r = await fetch('/api/session/send', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ agent_key: 'master', text: row.commandTemplate }),
        });
        const j = await r.json();
        if (j.ok) {
            toast('已下发到主 Agent', true);
        } else {
            await navigator.clipboard.writeText(row.commandTemplate);
            toast(j.error || '下发失败，已复制到剪贴板', false);
        }
    } catch (e) {
        await navigator.clipboard.writeText(row.commandTemplate);
        toast('下发接口不可用，已复制到剪贴板', false);
    }
}

async function saveCommandPopup(closeAfter = false) {
    const keyInput = document.getElementById('cmd-popup-key');
    const titleInput = document.getElementById('cmd-popup-title-input');
    const riskInput = document.getElementById('cmd-popup-risk');
    const enabledInput = document.getElementById('cmd-popup-enabled');
    const descInput = document.getElementById('cmd-popup-desc');
    const schemaInput = document.getElementById('cmd-popup-args-schema');
    const commandInput = document.getElementById('cmd-popup-textarea');
    if (!keyInput || !titleInput || !riskInput || !enabledInput || !descInput || !schemaInput || !commandInput) return;

    let argsSchema = {};
    try {
        argsSchema = _parseCommandArgsSchemaInput(schemaInput.value);
    } catch (e) {
        toast(e.message, false);
        return;
    }

    const payload = {
        card_key: keyInput.value.trim(),
        title: titleInput.value.trim(),
        risk_level: riskInput.value || 'normal',
        enabled: !!enabledInput.checked,
        description: descInput.value,
        args_schema: argsSchema,
        command_template: commandInput.value,
        updated_by: 'dashboard',
    };

    try {
        const r = await fetch('/api/command-cards', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        const j = await r.json();
        if (j.ok) {
            toast('命令卡已保存', true);
            await loadCommandCards();
            _currentCommandPopupData = j.command_card || null;
            if (closeAfter) {
                closeCommandPopup();
            }
        } else {
            toast(j.error || '保存失败', false);
        }
    } catch (e) {
        toast('保存失败: ' + e.message, false);
    }
}

async function toggleCommandCardEnabled(idx) {
    const row = _commandCardRows[idx];
    if (!row) return;
    try {
        const r = await fetch('/api/command-cards/toggle', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                card_key: row.cardKey,
                enabled: !row.enabled,
                updated_by: 'dashboard',
            }),
        });
        const j = await r.json();
        if (j.ok) {
            toast(`命令卡已${!row.enabled ? '启用' : '禁用'}`, true);
            await loadCommandCards();
        } else {
            toast(j.error || '状态更新失败', false);
        }
    } catch (e) {
        toast('状态更新失败: ' + e.message, false);
    }
}

function _syncCommandCardSelectionState() {
    const allChecks = Array.from(document.querySelectorAll('#cmd-card-tbody .cmd-card-select'));
    const checkedKeys = allChecks
        .filter((item) => item.checked)
        .map((item) => String(item.dataset.cardKey || '').trim())
        .filter(Boolean);

    _selectedCommandCardKeys = new Set(checkedKeys);

    const allBox = document.getElementById('cmd-card-select-all');
    if (!allBox) return;
    allBox.checked = allChecks.length > 0 && checkedKeys.length === allChecks.length;
    allBox.indeterminate = checkedKeys.length > 0 && checkedKeys.length < allChecks.length;
}

function toggleCommandCardSelectAll(checked) {
    const allChecks = document.querySelectorAll('#cmd-card-tbody .cmd-card-select');
    allChecks.forEach((item) => {
        item.checked = !!checked;
    });
    _syncCommandCardSelectionState();
}

async function deleteSelectedCommandCards() {
    const keys = Array.from(_selectedCommandCardKeys);
    if (!keys.length) {
        toast('请先勾选要删除的命令卡', false);
        return;
    }

    const confirmed = await showConfirm(`确认删除已勾选的 ${keys.length} 张命令卡？`);
    if (!confirmed) return;

    try {
        const r = await fetch('/api/command-cards/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                card_keys: keys,
                updated_by: 'dashboard',
            }),
        });
        const j = await r.json();
        if (!j.ok) {
            toast(j.error || '删除失败', false);
            return;
        }

        const deleted = Number(j.deleted || 0);
        _selectedCommandCardKeys = new Set();
        toast(`已删除 ${deleted} 张命令卡`, true);
        await refreshSections(['command_cards', 'audit', 'system']);
    } catch (e) {
        toast('删除失败: ' + e.message, false);
    }
}

function renderCommandCards(cards) {
    const tbody = document.getElementById('cmd-card-tbody');
    const empty = document.getElementById('cmd-card-empty');
    if (!tbody) return;

    _commandCardRows = _normalizeCommandCardRows(cards || []);

    if (!_commandCardRows.length) {
        tbody.innerHTML = '';
        _selectedCommandCardKeys = new Set();
        if (empty) {
            empty.style.display = '';
            empty.textContent = '暂无命令卡，可点击“新建命令卡”';
        }
        _syncCommandCardSelectionState();
        return;
    }
    if (empty) empty.style.display = 'none';

    tbody.innerHTML = _commandCardRows.map((card, idx) => {
        const rawKey = card.cardKey || '';
        const key = escapeHtml(rawKey);
        const title = escapeHtml(card.title || card.cardKey || '');
        const desc = escapeHtml(card.description || '');
        const risk = escapeHtml(card.riskLevel || 'normal');
        const riskClass = classToken(card.riskLevel || 'normal');
        const statusBadge = card.enabled
            ? '<span class="level-badge level-enabled">enabled</span>'
            : '<span class="level-badge level-disabled">disabled</span>';
        const updatedAt = escapeHtml(_formatCommandCardTime(card.updatedAt || ''));
        const runCount = Number(card.runCount || 0);
        const lastRun = runCount > 0 ? _formatCommandCardTime(card.lastRunAt || '') : '-';
        const recentExec = escapeHtml(`${lastRun} · ${runCount}次`);
        const checked = _selectedCommandCardKeys.has(rawKey) ? 'checked' : '';
        const rowStyle = card.enabled ? '' : ' style="opacity:.68"';
        return `<tr class="command-row" data-idx="${idx}"${rowStyle}
                    onclick="copyCommandDirect(${idx})"
                    ondblclick="sendCommandToMaster(${idx})">
            <td style="width:34px;text-align:center"><input type="checkbox" class="cmd-card-select" data-card-key="${key}" ${checked} onclick="event.stopPropagation()" onchange="_syncCommandCardSelectionState()"></td>
            <td style="font-family:var(--font-mono);font-size:0.75rem;color:var(--accent)">${key}</td>
            <td style="font-size:0.78rem">${title}</td>
            <td><span class="level-badge level-${riskClass}">${risk}</span></td>
            <td>${statusBadge}</td>
            <td style="font-size:0.74rem;color:var(--text-secondary);font-family:var(--font-mono)">${updatedAt || '-'}</td>
            <td style="font-size:0.74rem;color:var(--text-secondary);font-family:var(--font-mono)">${recentExec}</td>
            <td style="max-width:420px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${desc || '-'}</td>
            <td style="text-align:center;white-space:nowrap">
                <button class="btn btn-sm btn-secondary" onclick="event.stopPropagation();openCommandPopup(${idx})" title="编辑" style="cursor:pointer">编辑</button>
                <button class="btn btn-sm btn-secondary" onclick="event.stopPropagation();toggleCommandCardEnabled(${idx})" title="启停" style="cursor:pointer;margin-left:4px">${card.enabled ? '禁用' : '启用'}</button>
            </td>
        </tr>`;
    }).join('');

    _syncCommandCardSelectionState();
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

    try {
        const kw = (document.getElementById('cmd-search')?.value || '').trim();
        const riskLevel = (document.getElementById('cmd-risk-filter')?.value || '').trim();
        const enabledOnly = !!document.getElementById('cmd-enabled-only')?.checked;
        const params = new URLSearchParams();
        params.set('limit', '500');
        if (kw) params.set('keyword', kw);
        if (riskLevel) params.set('risk_level', riskLevel);
        if (enabledOnly) params.set('enabled_only', '1');

        const r = await fetch('/api/command-cards?' + params.toString());
        const j = await r.json();
        if (j.ok) {
            renderCommandCards(j.cards || []);
        } else {
            toast(j.error || '加载命令卡失败', false);
        }
    } catch (e) {
        toast('加载命令卡失败: ' + e.message, false);
    }
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
    if (_confirmDialogActive) return;   // 确认对话框期间跳过刷新
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
    if (scopes.includes('acks')) tasks.push(loadTaskAcks());
    if (scopes.includes('dags')) tasks.push(loadTaskDags());
    if (scopes.includes('traces')) tasks.push(loadTaskTraces());
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
    } else if (pageId === 'acks') {
        loadTaskAcks();
    } else if (pageId === 'dags') {
        loadTaskDags();
    } else if (pageId === 'traces') {
        loadTaskTraces();
    } else if (pageId === 'lifecycle') {
        loadLifecycleStatus();
        _startLifecycleAutoRefresh();
    }
    if (pageId !== 'lifecycle') {
        _stopLifecycleAutoRefresh();
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

// ─── 任务管理 (ACK) ──────────────────────────────────────────────────────

let _ackRows = [];

const _ACK_STATUS_COLOR = {
    pending: '#f59e0b', in_progress: '#8b5cf6', blocked: '#ef4444',
    done: '#10b981', failed: '#ef4444', cancelled: '#6b7280',
};
const _ACK_PRIORITY_COLOR = {
    critical: '#ef4444', high: '#f59e0b', normal: '#3b82f6', low: '#6b7280',
};

function _fmtCompact(dt) {
    if (!dt) return '-';
    const d = new Date(dt);
    if (isNaN(d)) return dt;
    const d8 = _toUTC8(d);
    return `${d8.getFullYear()}-${String(d8.getMonth() + 1).padStart(2, '0')}-${String(d8.getDate()).padStart(2, '0')} ${String(d8.getHours()).padStart(2, '0')}:${String(d8.getMinutes()).padStart(2, '0')}`;
}

async function loadTaskAcks() {
    try {
        const kw = (document.getElementById('ack-search') || {}).value || '';
        const st = (document.getElementById('ack-status-filter') || {}).value || '';
        const pri = (document.getElementById('ack-priority-filter') || {}).value || '';
        const params = new URLSearchParams();
        if (kw) params.set('keyword', kw);
        if (st) params.set('status', st);
        if (pri) params.set('priority', pri);
        const resp = await fetch(`/api/task-acks?${params}`);
        const j = await resp.json();
        _ackRows = j.acks || [];
        renderTaskAcks();
    } catch (e) { console.error('loadTaskAcks', e); }
}

function renderTaskAcks() {
    const tbody = document.getElementById('ack-tbody');
    const empty = document.getElementById('ack-empty');
    const stats = document.getElementById('ack-stats');
    if (!tbody) return;

    // stats cards
    if (stats) {
        const counts = {};
        _ackRows.forEach(r => { counts[r.status] = (counts[r.status] || 0) + 1; });
        const order = ['pending', 'in_progress', 'done', 'failed', 'cancelled'];
        stats.innerHTML = order.filter(s => counts[s]).map(s =>
            `<div style="background:${_ACK_STATUS_COLOR[s]}22;color:${_ACK_STATUS_COLOR[s]};padding:4px 12px;border-radius:6px;font-size:.82rem;font-weight:600">${s}: ${counts[s]}</div>`
        ).join('') + `<div style="padding:4px 12px;font-size:.82rem;font-weight:600;color:var(--text-secondary)">共 ${_ackRows.length} 项</div>`;
    }

    if (_ackRows.length === 0) {
        tbody.innerHTML = '';
        if (empty) { empty.style.display = ''; empty.textContent = '暂无任务记录'; }
        return;
    }
    if (empty) empty.style.display = 'none';

    tbody.innerHTML = _ackRows.map((r, i) => {
        const priColor = _ACK_PRIORITY_COLOR[r.priority] || '#6b7280';
        const stColor = _ACK_STATUS_COLOR[r.status] || '#6b7280';
        const prog = Math.max(0, Math.min(100, r.progress || 0));
        const deps = (r.depends_on || []);
        const depsText = deps.length > 0 ? deps.map(d => d.replace(/^T/, '')).join(', ') : '-';
        const resultText = r.result_summary || '-';
        return `<tr onclick="copyAckKey(${i})" style="cursor:pointer" title="点击复制 Task ID">
            <td style="font-family:monospace;font-size:.78rem;font-weight:600">${escapeHtml(r.ack_key)}</td>
            <td style="font-size:.82rem">${escapeHtml(r.title)}</td>
            <td style="font-family:monospace;font-size:.75rem;color:var(--text-secondary)">${escapeHtml(r.project_id || '-')}</td>
            <td style="font-size:.78rem">${escapeHtml(r.assigned_to || '-')}</td>
            <td><span style="background:${priColor}22;color:${priColor};padding:2px 8px;border-radius:4px;font-size:.72rem;font-weight:600">${r.priority}</span></td>
            <td><span style="background:${stColor}22;color:${stColor};padding:2px 8px;border-radius:4px;font-size:.72rem;font-weight:600">${r.status}</span></td>
            <td><div style="background:var(--border);border-radius:3px;height:14px;position:relative;overflow:hidden"><div style="background:${stColor};height:100%;width:${prog}%;transition:width .3s"></div><span style="position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);font-size:.60rem;color:var(--text-primary)">${prog}%</span></div></td>
            <td style="font-family:monospace;font-size:.70rem;color:var(--text-secondary);max-width:120px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${escapeHtml(deps.join(', '))}">${escapeHtml(depsText)}</td>
            <td style="font-size:.75rem">${_fmtCompact(r.updated_at)}</td>
            <td style="max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.72rem" title="${escapeHtml(resultText)}">${escapeHtml(resultText.length > 60 ? resultText.substring(0, 60) + '…' : resultText)}</td>
        </tr>`;
    }).join('');
}

function toggleAckCheck(key, checked) {
    if (checked) _selectedAckKeys.add(key); else _selectedAckKeys.delete(key);
    const allCb = document.getElementById('ack-select-all');
    if (allCb) allCb.checked = _selectedAckKeys.size === _ackRows.length && _ackRows.length > 0;
}
function toggleAckSelectAll(checked) {
    _selectedAckKeys.clear();
    if (checked) _ackRows.forEach(r => _selectedAckKeys.add(r.ack_key));
    renderTaskAcks();
}

function copyAckKey(idx) {
    const r = _ackRows[idx]; if (!r) return;
    navigator.clipboard.writeText(r.ack_key).then(() => toast('已复制: ' + r.ack_key));
}

async function quickAckStatus(ackKey, status) {
    if (!status) return;
    try {
        await fetch('/api/task-acks/status', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ack_key: ackKey, status }),
        });
        toast(`${ackKey} → ${status}`);
        loadTaskAcks();
    } catch (e) { toast('状态更新失败: ' + e.message); }
}

async function deleteSelectedAcks() {
    if (_selectedAckKeys.size === 0) return toast('请先勾选要删除的 ACK');
    _confirmDialogActive = true;
    const ok = await showConfirm(`确认删除 ${_selectedAckKeys.size} 条 ACK？`);
    _confirmDialogActive = false;
    if (!ok) return;
    try {
        const resp = await fetch('/api/task-acks/delete', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ack_keys: [..._selectedAckKeys] }),
        });
        const j = await resp.json();
        toast(`已删除 ${j.deleted || 0} 条 ACK`);
        _selectedAckKeys.clear();
        loadTaskAcks();
    } catch (e) { toast('删除失败: ' + e.message); }
}

function openAckPopup(idx) {
    const popup = document.getElementById('ack-popup');
    if (!popup) return;
    const r = idx >= 0 ? _ackRows[idx] : null;
    document.getElementById('ack-popup-title').textContent = r ? `编辑 ACK: ${r.ack_key}` : '新建 ACK';
    document.getElementById('ack-popup-key').value = r ? r.ack_key : '';
    document.getElementById('ack-popup-key').readOnly = !!r;
    document.getElementById('ack-popup-title-input').value = r ? r.title : '';
    document.getElementById('ack-popup-desc').value = r ? r.description : '';
    document.getElementById('ack-popup-assigned').value = r ? r.assigned_to : '';
    document.getElementById('ack-popup-requested').value = r ? r.requested_by : 'dashboard';
    document.getElementById('ack-popup-priority').value = r ? r.priority : 'normal';
    document.getElementById('ack-popup-status').value = r ? r.status : 'pending';
    document.getElementById('ack-popup-progress').value = r ? r.progress : 0;
    document.getElementById('ack-popup-message').value = r ? r.ack_message : '';
    document.getElementById('ack-popup-result').value = r ? r.result_summary : '';
    const dueInput = document.getElementById('ack-popup-due');
    if (dueInput) dueInput.value = r && r.due_at ? r.due_at.replace(' ', 'T').substring(0, 16) : '';
    popup.style.display = '';
}

async function saveAckPopup(andClose) {
    const key = document.getElementById('ack-popup-key').value.trim();
    if (!key) return toast('ACK Key 不能为空');
    const body = {
        ack_key: key,
        title: document.getElementById('ack-popup-title-input').value.trim(),
        description: document.getElementById('ack-popup-desc').value.trim(),
        assigned_to: document.getElementById('ack-popup-assigned').value.trim(),
        requested_by: document.getElementById('ack-popup-requested').value.trim() || 'dashboard',
        priority: document.getElementById('ack-popup-priority').value,
        status: document.getElementById('ack-popup-status').value,
        progress: parseInt(document.getElementById('ack-popup-progress').value) || 0,
        ack_message: document.getElementById('ack-popup-message').value.trim(),
        result_summary: document.getElementById('ack-popup-result').value.trim(),
        due_at: document.getElementById('ack-popup-due').value || null,
    };
    try {
        const resp = await fetch('/api/task-acks/save', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        const j = await resp.json();
        if (j.ok) {
            toast('ACK 已保存');
            if (andClose) document.getElementById('ack-popup').style.display = 'none';
            loadTaskAcks();
        } else { toast('保存失败: ' + (j.error || '')); }
    } catch (e) { toast('保存失败: ' + e.message); }
}


// ─── DAG 管理 ──────────────────────────────────────────────────────

let _dagRows = [];
let _selectedDagKeys = new Set();
let _dagDetailKey = '';

const _DAG_STATUS_COLOR = {
    draft: '#6b7280', ready: '#3b82f6', running: '#8b5cf6',
    paused: '#f59e0b', done: '#10b981', failed: '#ef4444',
};
const _NODE_STATUS_COLOR = {
    pending: '#6b7280', running: '#8b5cf6', done: '#10b981',
    failed: '#ef4444', skipped: '#f59e0b',
};
const _NODE_TYPE_LABEL = { task: '任务', gate: '网关', check: '检查' };

async function loadTaskDags() {
    try {
        const kw = (document.getElementById('dag-search') || {}).value || '';
        const st = (document.getElementById('dag-status-filter') || {}).value || '';
        const params = new URLSearchParams();
        if (kw) params.set('keyword', kw);
        if (st) params.set('status', st);
        const resp = await fetch(`/api/task-dags?${params}`);
        const j = await resp.json();
        _dagRows = j.dags || [];
        renderTaskDags();
    } catch (e) { console.error('loadTaskDags', e); }
}

function renderTaskDags() {
    const tbody = document.getElementById('dag-tbody');
    const empty = document.getElementById('dag-empty');
    if (!tbody) return;

    if (_dagRows.length === 0) {
        tbody.innerHTML = '';
        if (empty) { empty.style.display = ''; empty.textContent = '暂无 DAG 记录'; }
        return;
    }
    if (empty) empty.style.display = 'none';

    tbody.innerHTML = _dagRows.map((r, i) => {
        const checked = _selectedDagKeys.has(r.dag_key) ? ' checked' : '';
        const stColor = _DAG_STATUS_COLOR[r.status] || '#6b7280';
        const total = r.node_total || 0;
        const done = r.node_done || 0;
        const progText = total > 0 ? `${done}/${total}` : '-';
        return `<tr onclick="copyDagKey(${i})" style="cursor:pointer">
            <td style="text-align:center" onclick="event.stopPropagation()"><input type="checkbox"${checked} onchange="toggleDagCheck('${escapeHtml(r.dag_key)}',this.checked)"></td>
            <td style="font-family:monospace;font-size:.8rem">${escapeHtml(r.dag_key)}</td>
            <td>${escapeHtml(r.title)}</td>
            <td><span style="background:${stColor}22;color:${stColor};padding:2px 8px;border-radius:4px;font-size:.75rem;font-weight:600">${r.status}</span></td>
            <td style="font-weight:600;font-size:.82rem">${progText}</td>
            <td>${escapeHtml(r.created_by || '-')}</td>
            <td style="font-size:.78rem">${_fmtCompact(r.started_at)}</td>
            <td style="font-size:.78rem">${_fmtCompact(r.updated_at)}</td>
            <td style="max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.78rem" title="${escapeHtml(r.description)}">${escapeHtml(r.description || '-')}</td>
            <td style="text-align:center" onclick="event.stopPropagation()">
                <button class="btn btn-sm btn-secondary" onclick="showDagDetail('${escapeHtml(r.dag_key)}')" style="margin-right:4px">详情</button>
                <button class="btn btn-sm btn-secondary" onclick="openDagPopup(${i})">编辑</button>
            </td>
        </tr>`;
    }).join('');

    const allCb = document.getElementById('dag-select-all');
    if (allCb) allCb.checked = _selectedDagKeys.size > 0 && _selectedDagKeys.size === _dagRows.length;
}

function toggleDagCheck(key, checked) {
    if (checked) _selectedDagKeys.add(key); else _selectedDagKeys.delete(key);
    const allCb = document.getElementById('dag-select-all');
    if (allCb) allCb.checked = _selectedDagKeys.size === _dagRows.length && _dagRows.length > 0;
}
function toggleDagSelectAll(checked) {
    _selectedDagKeys.clear();
    if (checked) _dagRows.forEach(r => _selectedDagKeys.add(r.dag_key));
    renderTaskDags();
}

function copyDagKey(idx) {
    const r = _dagRows[idx]; if (!r) return;
    navigator.clipboard.writeText(r.dag_key).then(() => toast('已复制: ' + r.dag_key));
}

async function deleteSelectedDags() {
    if (_selectedDagKeys.size === 0) return toast('请先勾选要删除的 DAG');
    _confirmDialogActive = true;
    const ok = await showConfirm(`确认删除 ${_selectedDagKeys.size} 个 DAG 及其所有节点？`);
    _confirmDialogActive = false;
    if (!ok) return;
    try {
        const resp = await fetch('/api/task-dags/delete', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ dag_keys: [..._selectedDagKeys] }),
        });
        const j = await resp.json();
        toast(`已删除 ${j.deleted || 0} 个 DAG`);
        _selectedDagKeys.clear();
        if (_dagDetailKey && _selectedDagKeys.has(_dagDetailKey)) closeDagDetail();
        loadTaskDags();
    } catch (e) { toast('删除失败: ' + e.message); }
}

function openDagPopup(idx) {
    const popup = document.getElementById('dag-popup');
    if (!popup) return;
    const r = idx >= 0 ? _dagRows[idx] : null;
    document.getElementById('dag-popup-title').textContent = r ? `编辑 DAG: ${r.dag_key}` : '新建 DAG';
    document.getElementById('dag-popup-key').value = r ? r.dag_key : '';
    document.getElementById('dag-popup-key').readOnly = !!r;
    document.getElementById('dag-popup-title-input').value = r ? r.title : '';
    document.getElementById('dag-popup-desc').value = r ? r.description : '';
    document.getElementById('dag-popup-status').value = r ? r.status : 'draft';
    document.getElementById('dag-popup-created-by').value = r ? r.created_by : 'dashboard';
    popup.style.display = '';
}

async function saveDagPopup(andClose) {
    const key = document.getElementById('dag-popup-key').value.trim();
    if (!key) return toast('DAG Key 不能为空');
    const body = {
        dag_key: key,
        title: document.getElementById('dag-popup-title-input').value.trim(),
        description: document.getElementById('dag-popup-desc').value.trim(),
        status: document.getElementById('dag-popup-status').value,
        created_by: document.getElementById('dag-popup-created-by').value.trim() || 'dashboard',
    };
    try {
        const resp = await fetch('/api/task-dags/save', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        const j = await resp.json();
        if (j.ok) {
            toast('DAG 已保存');
            if (andClose) document.getElementById('dag-popup').style.display = 'none';
            loadTaskDags();
        } else { toast('保存失败: ' + (j.error || '')); }
    } catch (e) { toast('保存失败: ' + e.message); }
}

async function showDagDetail(dagKey) {
    _dagDetailKey = dagKey;
    try {
        const resp = await fetch(`/api/task-dags/detail?dag_key=${encodeURIComponent(dagKey)}`);
        const j = await resp.json();
        if (!j.ok) return toast('加载失败: ' + (j.error || ''));
        const dag = j.dag;
        const nodes = dag.nodes || [];
        document.getElementById('dag-detail-title').textContent = `节点明细 — ${dag.title || dag.dag_key} (${nodes.length} 个节点)`;
        const tbody = document.getElementById('dag-node-tbody');
        if (tbody) {
            tbody.innerHTML = nodes.map(n => {
                const nstColor = _NODE_STATUS_COLOR[n.status] || '#6b7280';
                const deps = (n.depends_on || []).join(', ') || '-';
                const typeLabel = _NODE_TYPE_LABEL[n.node_type] || n.node_type;
                return `<tr>
                    <td style="font-family:monospace;font-size:.8rem">${escapeHtml(n.node_key)}</td>
                    <td>${escapeHtml(n.title || '-')}</td>
                    <td><span style="font-size:.75rem">${typeLabel}</span></td>
                    <td>${escapeHtml(n.assigned_to || '-')}</td>
                    <td><span style="background:${nstColor}22;color:${nstColor};padding:2px 8px;border-radius:4px;font-size:.75rem;font-weight:600">${n.status}</span></td>
                    <td style="font-size:.75rem">${escapeHtml(deps)}</td>
                    <td style="font-size:.75rem;font-family:monospace">${escapeHtml(n.command_ref || '-')}</td>
                    <td style="font-size:.78rem">${_fmtCompact(n.started_at)}</td>
                    <td style="font-size:.78rem">${_fmtCompact(n.finished_at)}</td>
                    <td style="text-align:center">
                        <select class="input" style="width:80px;font-size:.72rem;padding:2px 4px" onchange="quickNodeStatus('${escapeHtml(dagKey)}','${escapeHtml(n.node_key)}',this.value)">
                            <option value="">状态...</option>
                            ${['pending', 'running', 'done', 'failed', 'skipped'].filter(s => s !== n.status).map(s => `<option value="${s}">${s}</option>`).join('')}
                        </select>
                    </td>
                </tr>`;
            }).join('');
        }
        document.getElementById('dag-detail-panel').style.display = '';
    } catch (e) { toast('加载详情失败: ' + e.message); }
}

function closeDagDetail() {
    _dagDetailKey = '';
    document.getElementById('dag-detail-panel').style.display = 'none';
}

async function quickNodeStatus(dagKey, nodeKey, status) {
    if (!status) return;
    try {
        await fetch('/api/task-dags/node/status', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ dag_key: dagKey, node_key: nodeKey, status }),
        });
        toast(`${nodeKey} → ${status}`);
        showDagDetail(dagKey);
        loadTaskDags();
    } catch (e) { toast('节点状态更新失败: ' + e.message); }
}

// ─── 任务追踪 ──────────────────────────────────────────────────────

const _TRACE_STATUS_COLOR = {
    running: '#8b5cf6', ok: '#10b981', error: '#ef4444',
};

let _traceRows = [];
let _traceDetailTraceId = '';

async function loadTaskTraces() {
    try {
        const kw = (document.getElementById('trace-search') || {}).value || '';
        const st = (document.getElementById('trace-status-filter') || {}).value || '';
        const comp = (document.getElementById('trace-component-filter') || {}).value || '';
        const params = new URLSearchParams();
        if (kw) params.set('trace_id', kw);
        if (st) params.set('status', st);
        if (comp) params.set('component', comp);
        const resp = await fetch(`/api/task-traces?${params}`);
        const j = await resp.json();
        _traceRows = j.traces || [];
        renderTaskTraces();
    } catch (e) { console.error('loadTaskTraces', e); }
}

function renderTaskTraces() {
    const tbody = document.getElementById('trace-tbody');
    const empty = document.getElementById('trace-empty');
    const stats = document.getElementById('trace-stats');
    if (!tbody) return;

    if (stats) {
        const counts = {};
        _traceRows.forEach(r => { counts[r.status] = (counts[r.status] || 0) + 1; });
        const order = ['running', 'ok', 'error'];
        stats.innerHTML = order.filter(s => counts[s]).map(s =>
            `<div style="background:${_TRACE_STATUS_COLOR[s]}22;color:${_TRACE_STATUS_COLOR[s]};padding:4px 12px;border-radius:6px;font-size:.82rem;font-weight:600">${s}: ${counts[s]}</div>`
        ).join('') + `<div style="padding:4px 12px;font-size:.82rem;font-weight:600;color:var(--text-secondary)">共 ${_traceRows.length} 条</div>`;
    }

    if (_traceRows.length === 0) {
        tbody.innerHTML = '';
        if (empty) { empty.style.display = ''; empty.textContent = '暂无追踪记录'; }
        return;
    }
    if (empty) empty.style.display = 'none';

    tbody.innerHTML = _traceRows.map(r => {
        const stColor = _TRACE_STATUS_COLOR[r.status] || '#6b7280';
        const comps = (r.components || []).join(', ') || '-';
        const selected = r.trace_id === _traceDetailTraceId ? 'background:var(--border)' : '';
        return `<tr onclick="loadTraceSpans('${escapeHtml(r.trace_id)}')" style="cursor:pointer;${selected}" title="点击查看 Span 明细">
            <td style="font-family:monospace;font-size:.75rem">${escapeHtml(r.trace_id)}</td>
            <td><span style="background:${stColor}22;color:${stColor};padding:2px 8px;border-radius:4px;font-size:.72rem;font-weight:600">${r.status}</span></td>
            <td style="text-align:center;font-weight:600">${r.span_count || 0}</td>
            <td style="font-size:.78rem">${escapeHtml(comps)}</td>
            <td style="font-size:.78rem">${escapeHtml(r.started_at || '-')}</td>
            <td style="font-size:.78rem">${escapeHtml(r.finished_at || '-')}</td>
        </tr>`;
    }).join('');
}

async function loadTraceSpans(traceId) {
    _traceDetailTraceId = traceId;
    renderTaskTraces(); // highlight selected row
    const panel = document.getElementById('trace-detail-panel');
    const title = document.getElementById('trace-detail-title');
    const tbody = document.getElementById('trace-span-tbody');
    if (!panel || !tbody) return;

    panel.style.display = '';
    if (title) title.textContent = `Span 明细 — ${traceId}`;
    tbody.innerHTML = '<tr><td colspan="7" style="text-align:center;padding:12px">加载中...</td></tr>';

    try {
        const resp = await fetch(`/api/task-traces/spans?trace_id=${encodeURIComponent(traceId)}`);
        const j = await resp.json();
        const spans = j.spans || [];

        if (spans.length === 0) {
            tbody.innerHTML = '<tr><td colspan="7" style="text-align:center;padding:12px;color:var(--text-secondary)">无 Span 数据</td></tr>';
            return;
        }

        tbody.innerHTML = spans.map(s => {
            const stColor = _TRACE_STATUS_COLOR[s.status] || '#6b7280';
            const inputStr = s.input_payload ? JSON.stringify(s.input_payload, null, 1) : '-';
            const outputStr = s.output_payload ? JSON.stringify(s.output_payload, null, 1) : '-';
            const errStr = s.error_text ? `<div style="color:#ef4444;margin-top:4px">错误: ${escapeHtml(s.error_text)}</div>` : '';
            const ioHtml = `<details style="font-size:.72rem;cursor:pointer"><summary style="font-weight:600">展开 I/O</summary><div style="margin-top:4px"><b>Input:</b><pre style="margin:2px 0;max-height:120px;overflow:auto;background:var(--bg-secondary);padding:4px;border-radius:4px;font-size:.68rem">${escapeHtml(inputStr.length > 500 ? inputStr.substring(0, 500) + '…' : inputStr)}</pre><b>Output:</b><pre style="margin:2px 0;max-height:120px;overflow:auto;background:var(--bg-secondary);padding:4px;border-radius:4px;font-size:.68rem">${escapeHtml(outputStr.length > 500 ? outputStr.substring(0, 500) + '…' : outputStr)}</pre>${errStr}</div></details>`;
            return `<tr>
                <td style="font-size:.78rem;font-family:monospace">${escapeHtml(s.span_name || '-')}</td>
                <td style="font-size:.78rem">${escapeHtml(s.component || '-')}</td>
                <td><span style="background:${stColor}22;color:${stColor};padding:2px 8px;border-radius:4px;font-size:.72rem;font-weight:600">${s.status}</span></td>
                <td style="text-align:right;font-family:monospace;font-size:.78rem">${s.duration_ms || 0}</td>
                <td style="font-size:.75rem">${escapeHtml(s.started_at || '-')}</td>
                <td style="font-size:.75rem">${escapeHtml(s.finished_at || '-')}</td>
                <td>${ioHtml}</td>
            </tr>`;
        }).join('');
    } catch (e) {
        tbody.innerHTML = `<tr><td colspan="7" style="text-align:center;color:#ef4444;padding:12px">加载失败: ${escapeHtml(e.message)}</td></tr>`;
    }
}

function closeTraceDetail() {
    _traceDetailTraceId = '';
    const panel = document.getElementById('trace-detail-panel');
    if (panel) panel.style.display = 'none';
    renderTaskTraces();
}

// ── Lifecycle 生命周期监控 ──────────────────────────────────────────────

let _lcAutoTimer = null;
const _LC_POLL_MS = 5000;

const _LC_STATUS_META = {
    completed: { icon: '✅', label: '已完成', color: 'var(--accent)', cls: 'lc-status-completed' },
    running: { icon: '●', label: '运行中', color: 'var(--blue)', cls: 'lc-status-running' },
    errored: { icon: '❌', label: '异常', color: 'var(--red)', cls: 'lc-status-errored' },
    stalled: { icon: '⚠', label: '停滞', color: 'var(--amber)', cls: 'lc-status-stalled' },
    unknown: { icon: '?', label: '未知', color: 'var(--text-muted)', cls: 'lc-status-unknown' },
};

function _startLifecycleAutoRefresh() {
    _stopLifecycleAutoRefresh();
    _lcAutoTimer = setInterval(loadLifecycleStatus, _LC_POLL_MS);
}

function _stopLifecycleAutoRefresh() {
    if (_lcAutoTimer) { clearInterval(_lcAutoTimer); _lcAutoTimer = null; }
}

async function loadLifecycleStatus() {
    try {
        const r = await fetch('/api/lifecycle/status');
        const j = await r.json();
        if (!j.ok) return;

        // update stats
        const el = (id) => document.getElementById(id);
        if (el('lc-cycles')) el('lc-cycles').textContent = String(j.cycles || 0);
        if (el('lc-notified')) el('lc-notified').textContent = String(j.notifications_sent || 0);
        if (el('lc-errors')) el('lc-errors').textContent = String(j.errors || 0);

        // engine badge
        const badge = el('lc-engine-badge');
        if (badge) {
            if (j.running) {
                badge.className = 'badge badge-green';
                badge.textContent = '运行中';
            } else {
                badge.className = 'badge badge-gray';
                badge.textContent = '未启动';
            }
        }

        // buttons
        const startBtn = el('lc-btn-start');
        const stopBtn = el('lc-btn-stop');
        if (startBtn) startBtn.disabled = !!j.running;
        if (stopBtn) stopBtn.disabled = !j.running;

        // cards
        renderLifecycleCards(j.agents || {});

        // timeline
        renderLifecycleTimeline(j.timeline || []);

    } catch (e) {
        console.error('loadLifecycleStatus error:', e);
    }
}

function renderLifecycleCards(agentsObj) {
    const grid = document.getElementById('lc-grid');
    if (!grid) return;

    const agents = Object.values(agentsObj);
    if (!agents.length) {
        grid.innerHTML = '<div class="lifecycle-empty">暂无子 Agent 数据，请先启动监控</div>';
        return;
    }

    grid.innerHTML = agents.map(a => {
        const st = _LC_STATUS_META[a.llm_status] || _LC_STATUS_META.unknown;
        const conf = Math.min(1, Math.max(0, Number(a.confidence) || 0));
        const confPct = Math.round(conf * 100);
        const confBarW = confPct + '%';
        const reason = escapeHtml(a.reason || '等待分析...');
        const tail = escapeHtml(String(a.output_tail || '').slice(-80));
        const runtimeCls = classToken(a.runtime_status || 'unknown');

        return `<div class="lifecycle-card ${st.cls}">
            <div class="lc-card-header">
                <span class="lc-card-id">${escapeHtml(a.agent_id)}</span>
                <span class="lc-card-name">${escapeHtml(a.agent_name || '')}</span>
                <span class="level-badge level-${runtimeCls}" style="margin-left:auto;font-size:0.68rem">${escapeHtml(a.runtime_status || 'unknown')}</span>
            </div>
            <div class="lc-card-decision">
                <span style="color:${st.color};font-size:0.92rem;font-weight:600">${st.icon} ${st.label}</span>
                <span style="font-family:var(--font-mono);font-size:0.72rem;color:var(--text-muted)">GPT-5.2</span>
            </div>
            <div class="lc-card-reason">${reason}</div>
            <div class="lc-card-confidence">
                <div class="lc-conf-bar">
                    <div class="lc-conf-fill" style="width:${confBarW};background:${st.color}"></div>
                </div>
                <span class="lc-conf-value">${conf.toFixed(2)}</span>
            </div>
            <div class="lc-card-tail">${tail || '<span style="color:var(--text-muted)">—</span>'}</div>
        </div>`;
    }).join('');
}

function renderLifecycleTimeline(events) {
    const tl = document.getElementById('lc-timeline');
    if (!tl) return;

    if (!events.length) {
        tl.innerHTML = '<div class="lifecycle-empty">暂无通知记录</div>';
        return;
    }

    // reverse chronological
    const items = [...events].reverse();
    tl.innerHTML = items.map(e => {
        const ts = String(e.ts || '').replace('T', ' ').slice(11, 19);
        const icon = escapeHtml(e.icon || '🔔');
        const text = escapeHtml(e.text || '');
        return `<div class="lc-tl-item">
            <span class="lc-tl-ts">${ts}</span>
            <span class="lc-tl-icon">${icon}</span>
            <span class="lc-tl-text">${text}</span>
        </div>`;
    }).join('');
}

async function lifecycleWatch(action) {
    const dryRun = !!document.getElementById('lc-dry-run')?.checked;
    try {
        const r = await fetch('/api/lifecycle/watch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action, dry_run: dryRun }),
        });
        const j = await r.json();
        if (j.ok) {
            toast(j.message || (action === 'start' ? '监控已启动' : '监控已停止'), true);
            setTimeout(loadLifecycleStatus, 800);
        } else {
            toast(j.error || '操作失败', false);
        }
    } catch (e) { toast('网络错误: ' + e.message, false); }
}

async function lifecycleSendNotify() {
    const msg = prompt('输入通知内容 (将发送到主 Agent):');
    if (!msg) return;
    try {
        const r = await fetch('/api/session/send', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ agent_key: 'master', text: msg }),
        });
        const j = await r.json();
        if (j.ok) toast('已通知主 Agent', true);
        else toast(j.error || '通知失败', false);
    } catch (e) { toast('网络错误: ' + e.message, false); }
}

function clearLifecycleTimeline() {
    const tl = document.getElementById('lc-timeline');
    if (tl) tl.innerHTML = '<div class="lifecycle-empty">已清空</div>';
}

// ── LLM Config 配置管理 ──────────────────────────────────────────────

async function loadLlmConfig() {
    try {
        const r = await fetch('/api/llm/config');
        const j = await r.json();
        if (!j.ok) return;
        const cfg = j.config;

        const el = (id) => document.getElementById(id);
        if (el('llm-api-key')) el('llm-api-key').value = cfg.api_key || '';
        if (el('llm-base-url')) el('llm-base-url').value = cfg.base_url || '';
        if (el('llm-model')) el('llm-model').value = cfg.model || '';
        if (el('llm-reasoning-effort')) el('llm-reasoning-effort').value = cfg.reasoning_effort || 'high';
        if (el('llm-timeout')) el('llm-timeout').value = cfg.timeout || '30';
        if (el('llm-poll-interval')) el('llm-poll-interval').value = cfg.poll_interval || '8';
        if (el('llm-cooldown')) el('llm-cooldown').value = cfg.cooldown_sec || '60';
        if (el('llm-master-id')) el('llm-master-id').value = cfg.master_agent_id || 'agent_01';

        // status badge
        const badge = el('llm-status-badge');
        if (badge) {
            const hasKey = (cfg.api_key_full_length || 0) > 0;
            badge.className = hasKey ? 'badge badge-green' : 'badge badge-red';
            badge.textContent = hasKey ? `${cfg.model || 'unknown'}` : 'Key 未设置';
        }

        updateLlmApiExample(cfg);
    } catch (e) {
        console.error('loadLlmConfig error:', e);
    }
}

async function saveLlmConfig() {
    const el = (id) => document.getElementById(id);
    const data = {};

    const apiKey = el('llm-api-key')?.value?.trim();
    // Only send api_key if user typed a real key (not a masked one)
    if (apiKey && !apiKey.includes('...') && !apiKey.includes('***')) {
        data.api_key = apiKey;
    }
    const fields = [
        ['llm-base-url', 'base_url'],
        ['llm-model', 'model'],
        ['llm-reasoning-effort', 'reasoning_effort'],
        ['llm-timeout', 'timeout'],
        ['llm-poll-interval', 'poll_interval'],
        ['llm-cooldown', 'cooldown_sec'],
        ['llm-master-id', 'master_agent_id'],
    ];
    for (const [elemId, key] of fields) {
        const v = el(elemId)?.value?.trim();
        if (v) data[key] = v;
    }

    try {
        const r = await fetch('/api/llm/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data),
        });
        const j = await r.json();
        if (j.ok) {
            toast(j.message || '配置已保存', true);
            setTimeout(loadLlmConfig, 500);
        } else {
            toast(j.error || '保存失败', false);
        }
    } catch (e) { toast('网络错误: ' + e.message, false); }
}

async function testLlmConnection() {
    const resultEl = document.getElementById('llm-test-result');
    if (resultEl) {
        resultEl.style.color = 'var(--amber)';
        resultEl.textContent = '⏳ 正在测试连接，请稍等...';
    }
    try {
        const r = await fetch('/api/llm/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: '{}',
        });
        const j = await r.json();
        if (resultEl) {
            if (j.ok) {
                resultEl.style.color = 'var(--accent)';
                resultEl.textContent =
                    `✅ 连接成功!\n` +
                    `\n模型: ${j.model}` +
                    `\n响应ID: ${j.response_id}` +
                    `\n耗时: ${j.elapsed_ms}ms` +
                    `\n\n--- 回复内容 ---\n${j.response_text || '(空)'}`;
            } else {
                resultEl.style.color = 'var(--red)';
                resultEl.textContent =
                    `❌ 连接失败\n` +
                    `\nHTTP ${j.status_code || 'N/A'}` +
                    `\n耗时: ${j.elapsed_ms || 0}ms` +
                    `\n错误: ${j.error || '未知错误'}`;
            }
        }
    } catch (e) {
        if (resultEl) {
            resultEl.style.color = 'var(--red)';
            resultEl.textContent = `❌ 网络错误: ${e.message}`;
        }
    }
}

function toggleLlmPw(inputId) {
    const el = document.getElementById(inputId);
    if (!el) return;
    el.type = el.type === 'password' ? 'text' : 'password';
}

function updateLlmApiExample(cfg) {
    const el = document.getElementById('llm-api-example');
    if (!el) return;
    const baseUrl = cfg.base_url || 'https://api.gpteamservices.com/v1';
    const model = cfg.model || 'gpt-5.2';
    const effort = cfg.reasoning_effort || 'high';
    const keyDisplay = cfg.api_key || 'sk-...';

    el.textContent =
        `curl -sS -X POST "${baseUrl}/responses" \\\n` +
        `  -H "Authorization: Bearer ${keyDisplay}" \\\n` +
        `  -H "Content-Type: application/json" \\\n` +
        `  -d '{"model":"${model}","input":"hello","reasoning":{"effort":"${effort}"}}'`;
}
