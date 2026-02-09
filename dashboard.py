"""配置管理 Web 面板

启动: python3 dashboard.py
访问: http://localhost:8080
"""

import json
import os
import http.server
import urllib.parse
from pathlib import Path

from dotenv import load_dotenv, set_key

ENV_FILE = Path(__file__).parent / ".env"

# 配置项定义
CONFIG_SCHEMA = [
    {
        "group": "LLM 设置",
        "icon": "brain",
        "items": [
            {"key": "OPENAI_API_KEY", "label": "API Key", "type": "password", "desc": "OpenAI / 第三方 API Key"},
            {"key": "OPENAI_BASE_URL", "label": "Base URL", "type": "text", "desc": "API 端点（留空使用 OpenAI 官方）"},
            {"key": "LLM_MODEL", "label": "模型", "type": "text", "desc": "LLM 模型名称"},
            {"key": "LLM_TEMPERATURE", "label": "Temperature", "type": "number", "desc": "生成温度 (0-2)"},
        ],
    },
    {
        "group": "健壮性设置",
        "icon": "shield",
        "items": [
            {"key": "LLM_TIMEOUT", "label": "LLM 超时 (秒)", "type": "number", "desc": "单次 LLM 调用超时"},
            {"key": "LLM_MAX_RETRIES", "label": "LLM 重试次数", "type": "number", "desc": "LLM 调用失败重试次数"},
            {"key": "GATEWAY_TIMEOUT", "label": "Gateway 超时 (秒)", "type": "number", "desc": "单个 Gateway 执行超时"},
        ],
    },
    {
        "group": "系统设置",
        "icon": "cog",
        "items": [
            {"key": "LOG_LEVEL", "label": "日志级别", "type": "select",
             "options": ["DEBUG", "INFO", "WARNING", "ERROR"], "desc": "日志输出级别"},
        ],
    },
]

DEFAULTS = {
    "OPENAI_API_KEY": "",
    "OPENAI_BASE_URL": "",
    "LLM_MODEL": "gpt-5.2",
    "LLM_TEMPERATURE": "0.7",
    "LLM_TIMEOUT": "60",
    "LLM_MAX_RETRIES": "3",
    "GATEWAY_TIMEOUT": "120",
    "LOG_LEVEL": "INFO",
}

# SVG Icons (Heroicons / Lucide style)
ICONS = {
    "brain": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M12 2a7 7 0 0 0-7 7c0 2.38 1.19 4.47 3 5.74V17a2 2 0 0 0 2 2h4a2 2 0 0 0 2-2v-2.26c1.81-1.27 3-3.36 3-5.74a7 7 0 0 0-7-7z"/><path d="M10 21h4"/><path d="M9 9h.01M15 9h.01M9.5 13c.83.5 2.17 1 2.5 1s1.67-.5 2.5-1"/></svg>',
    "shield": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/></svg>',
    "cog": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>',
    "eye": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>',
    "eye-off": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>',
    "save": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17,21 17,13 7,13 7,21"/><polyline points="7,3 7,8 15,8"/></svg>',
    "server": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><circle cx="6" cy="6" r="1" fill="currentColor"/><circle cx="6" cy="18" r="1" fill="currentColor"/></svg>',
    "agent": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M9 9h.01M15 9h.01"/><path d="M8 13s1.5 2 4 2 4-2 4-2"/></svg>',
    "check": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20,6 9,17 4,12"/></svg>',
}


def load_current_config() -> dict:
    load_dotenv(ENV_FILE, override=True)
    config = {}
    for key, default in DEFAULTS.items():
        config[key] = os.getenv(key, default)
    return config


def save_config(updates: dict):
    if not ENV_FILE.exists():
        ENV_FILE.touch()
    for key, value in updates.items():
        if key in DEFAULTS:
            set_key(str(ENV_FILE), key, value)


def render_html() -> str:
    config = load_current_config()

    # Config groups
    groups_html = ""
    for group in CONFIG_SCHEMA:
        icon_svg = ICONS.get(group["icon"], "")
        items_html = ""
        for item in group["items"]:
            val = config.get(item["key"], "")

            if item["type"] == "password":
                input_html = f'''
                    <div class="password-wrap">
                        <input type="password" name="{item['key']}" value="{val}"
                               class="input" id="pw-{item['key']}" placeholder="未设置"
                               autocomplete="off">
                        <button type="button" class="pw-toggle" onclick="togglePw('{item['key']}')"
                                aria-label="显示/隐藏密码">
                            <span class="icon icon-sm" id="pw-icon-{item['key']}">{ICONS['eye']}</span>
                        </button>
                    </div>
                '''
            elif item["type"] == "select":
                opts = "".join(
                    f'<option value="{o}" {"selected" if o == val else ""}>{o}</option>'
                    for o in item.get("options", [])
                )
                input_html = f'<select name="{item["key"]}" class="input">{opts}</select>'
            elif item["type"] == "number":
                input_html = f'<input type="number" name="{item["key"]}" value="{val}" class="input" step="0.1">'
            else:
                input_html = f'<input type="text" name="{item["key"]}" value="{val}" class="input" placeholder="未设置">'

            items_html += f'''
                <div class="config-row">
                    <div class="config-meta">
                        <label class="config-label" for="{item['key']}">{item['label']}</label>
                        <span class="config-desc">{item['desc']}</span>
                    </div>
                    <div class="config-control">{input_html}</div>
                </div>
            '''

        groups_html += f'''
            <section class="card" role="group" aria-labelledby="grp-{group['icon']}">
                <header class="card-header">
                    <span class="icon icon-header">{icon_svg}</span>
                    <h2 id="grp-{group['icon']}">{group['group']}</h2>
                </header>
                <div class="card-body">{items_html}</div>
            </section>
        '''

    # Gateway cards
    from config.settings import GATEWAY_AGENT_MAP
    gw_cards = ""
    colors = ["#3b82f6", "#8b5cf6", "#06b6d4"]
    for i, (gw_name, gw_config) in enumerate(GATEWAY_AGENT_MAP.items()):
        color = colors[i % 3]
        agents_list = "".join(
            f'<div class="agent-chip"><span class="icon icon-xs">{ICONS["agent"]}</span>{a}</div>'
            for a in gw_config["agents"].keys()
        )
        gw_cards += f'''
            <div class="gw-card" style="--accent:{color}">
                <div class="gw-dot" style="background:{color}"></div>
                <h3 class="gw-name">{gw_config['name']}</h3>
                <span class="gw-id">{gw_name}</span>
                <div class="gw-agents">{agents_list}</div>
                <div class="gw-count">{len(gw_config['agents'])} agents</div>
            </div>
        '''

    return f'''<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>多Agent编排 — 控制台</title>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
:root {{
    --bg-base: #0a0a0f;
    --bg-elevated: rgba(255,255,255,0.03);
    --bg-card: rgba(255,255,255,0.04);
    --bg-input: rgba(255,255,255,0.06);
    --border: rgba(255,255,255,0.08);
    --border-focus: #3b82f6;
    --text-primary: #f0f0f5;
    --text-secondary: #8b8b9e;
    --text-muted: #5a5a6e;
    --accent: #3b82f6;
    --accent-glow: rgba(59,130,246,0.15);
    --success: #22c55e;
    --error: #ef4444;
    --radius: 12px;
    --radius-sm: 8px;
    --transition: 200ms cubic-bezier(0.4, 0, 0.2, 1);
}}

* {{ margin: 0; padding: 0; box-sizing: border-box; }}

body {{
    font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
    background: var(--bg-base);
    color: var(--text-primary);
    line-height: 1.6;
    min-height: 100vh;
}}

/* Subtle noise background */
body::before {{
    content: '';
    position: fixed;
    inset: 0;
    background: radial-gradient(ellipse 80% 60% at 50% -20%, rgba(59,130,246,0.08), transparent),
                radial-gradient(ellipse 60% 40% at 80% 100%, rgba(139,92,246,0.06), transparent);
    pointer-events: none;
    z-index: 0;
}}

.container {{
    max-width: 820px;
    margin: 0 auto;
    padding: 48px 24px 80px;
    position: relative;
    z-index: 1;
}}

/* Header */
.header {{
    text-align: center;
    margin-bottom: 48px;
}}
.header h1 {{
    font-size: 1.75rem;
    font-weight: 700;
    letter-spacing: -0.02em;
    color: var(--text-primary);
}}
.header p {{
    margin-top: 8px;
    color: var(--text-secondary);
    font-size: 0.9rem;
}}
.header .badge-row {{
    display: flex;
    gap: 8px;
    justify-content: center;
    margin-top: 16px;
}}
.header .badge {{
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 14px;
    border-radius: 20px;
    font-size: 0.75rem;
    font-weight: 600;
    background: var(--bg-input);
    border: 1px solid var(--border);
    color: var(--text-secondary);
}}
.header .badge .icon {{ width: 14px; height: 14px; }}

/* Cards */
.card {{
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    margin-bottom: 16px;
    overflow: hidden;
    transition: border-color var(--transition);
}}
.card:hover {{ border-color: rgba(255,255,255,0.12); }}

.card-header {{
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 20px 24px 0;
}}
.card-header h2 {{
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--text-primary);
}}
.card-body {{ padding: 8px 24px 20px; }}

/* Config rows */
.config-row {{
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 0;
    border-bottom: 1px solid var(--border);
    gap: 24px;
}}
.config-row:last-child {{ border-bottom: none; }}

.config-meta {{ flex: 1; min-width: 0; }}
.config-label {{
    font-size: 0.875rem;
    font-weight: 500;
    color: var(--text-primary);
    display: block;
    cursor: default;
}}
.config-desc {{
    font-size: 0.75rem;
    color: var(--text-muted);
    display: block;
    margin-top: 2px;
}}

.config-control {{
    flex-shrink: 0;
    width: 280px;
}}

/* Inputs */
.input {{
    width: 100%;
    padding: 10px 14px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-primary);
    font-family: inherit;
    font-size: 0.875rem;
    transition: border-color var(--transition), box-shadow var(--transition);
    -webkit-appearance: none;
}}
.input:focus {{
    outline: none;
    border-color: var(--border-focus);
    box-shadow: 0 0 0 3px var(--accent-glow);
}}
.input::placeholder {{ color: var(--text-muted); }}
select.input {{ cursor: pointer; }}

/* Password toggle */
.password-wrap {{
    position: relative;
}}
.password-wrap .input {{
    padding-right: 44px;
}}
.pw-toggle {{
    position: absolute;
    right: 4px;
    top: 50%;
    transform: translateY(-50%);
    background: none;
    border: none;
    padding: 8px;
    cursor: pointer;
    color: var(--text-muted);
    transition: color var(--transition);
    border-radius: 6px;
}}
.pw-toggle:hover {{ color: var(--text-secondary); }}

/* Icons */
.icon {{
    display: inline-flex;
    align-items: center;
    justify-content: center;
}}
.icon svg {{ display: block; }}
.icon-header {{ width: 20px; height: 20px; color: var(--accent); }}
.icon-header svg {{ width: 20px; height: 20px; }}
.icon-sm {{ width: 18px; height: 18px; }}
.icon-sm svg {{ width: 18px; height: 18px; }}
.icon-xs {{ width: 14px; height: 14px; }}
.icon-xs svg {{ width: 14px; height: 14px; }}

/* Save button */
.action-bar {{
    display: flex;
    justify-content: center;
    padding: 32px 0;
}}
.btn-save {{
    display: inline-flex;
    align-items: center;
    gap: 10px;
    padding: 12px 40px;
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius-sm);
    font-family: inherit;
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    transition: all var(--transition);
    box-shadow: 0 2px 12px var(--accent-glow);
}}
.btn-save:hover {{
    filter: brightness(1.1);
    box-shadow: 0 4px 24px rgba(59,130,246,0.3);
}}
.btn-save:active {{ transform: scale(0.98); }}
.btn-save .icon {{ width: 18px; height: 18px; }}
.btn-save .icon svg {{ width: 18px; height: 18px; }}

/* Gateway section */
.section-label {{
    text-align: center;
    font-size: 0.8rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.1em;
    color: var(--text-muted);
    margin: 48px 0 20px;
}}

.gw-grid {{
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
}}
.gw-card {{
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 20px;
    position: relative;
    overflow: hidden;
    transition: border-color var(--transition);
}}
.gw-card:hover {{ border-color: rgba(255,255,255,0.14); }}
.gw-card::after {{
    content: '';
    position: absolute;
    top: 0; left: 0; right: 0;
    height: 2px;
    background: var(--accent);
    opacity: 0.6;
}}
.gw-dot {{
    width: 8px;
    height: 8px;
    border-radius: 50%;
    margin-bottom: 12px;
    box-shadow: 0 0 8px currentColor;
}}
.gw-name {{
    font-size: 0.9rem;
    font-weight: 600;
    color: var(--text-primary);
}}
.gw-id {{
    font-size: 0.7rem;
    font-family: 'SF Mono', 'Fira Code', monospace;
    color: var(--text-muted);
    display: block;
    margin: 4px 0 14px;
}}
.gw-agents {{
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
}}
.agent-chip {{
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 10px;
    border-radius: 6px;
    font-size: 0.7rem;
    font-weight: 500;
    background: var(--bg-input);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-family: 'SF Mono', 'Fira Code', monospace;
}}
.agent-chip .icon {{ color: var(--text-muted); }}
.gw-count {{
    font-size: 0.7rem;
    color: var(--text-muted);
    margin-top: 12px;
    font-weight: 500;
}}

/* Toast */
.toast {{
    position: fixed;
    top: 20px;
    right: 20px;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 24px;
    border-radius: var(--radius-sm);
    font-size: 0.875rem;
    font-weight: 500;
    opacity: 0;
    transform: translateY(-12px) scale(0.96);
    transition: all 300ms cubic-bezier(0.4, 0, 0.2, 1);
    z-index: 1000;
    pointer-events: none;
}}
.toast.show {{
    opacity: 1;
    transform: translateY(0) scale(1);
}}
.toast-ok {{
    background: rgba(34,197,94,0.12);
    border: 1px solid rgba(34,197,94,0.25);
    color: #86efac;
}}
.toast-err {{
    background: rgba(239,68,68,0.12);
    border: 1px solid rgba(239,68,68,0.25);
    color: #fca5a5;
}}
.toast .icon {{ width: 18px; height: 18px; }}
.toast .icon svg {{ width: 18px; height: 18px; }}

/* Responsive */
@media (max-width: 640px) {{
    .container {{ padding: 24px 16px 60px; }}
    .config-row {{ flex-direction: column; align-items: stretch; gap: 8px; }}
    .config-control {{ width: 100%; }}
    .gw-grid {{ grid-template-columns: 1fr; }}
    .header h1 {{ font-size: 1.4rem; }}
}}

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {{
    *, *::before, *::after {{
        transition-duration: 0.01ms !important;
        animation-duration: 0.01ms !important;
    }}
}}
</style>
</head>
<body>
    <div class="container">
        <header class="header">
            <h1>Control Center</h1>
            <p>多Agent编排系统 配置管理</p>
            <div class="badge-row">
                <span class="badge"><span class="icon">{ICONS['server']}</span>1 Master</span>
                <span class="badge"><span class="icon">{ICONS['shield']}</span>3 Gateway</span>
                <span class="badge"><span class="icon">{ICONS['agent']}</span>12 Agent</span>
            </div>
        </header>

        <form id="cf">{groups_html}
            <div class="action-bar">
                <button type="submit" class="btn-save">
                    <span class="icon">{ICONS['save']}</span>保存配置
                </button>
            </div>
        </form>

        <div class="section-label">Gateway Architecture</div>
        <div class="gw-grid">{gw_cards}</div>
    </div>

    <div id="toast" class="toast" role="status" aria-live="polite">
        <span class="icon" id="toast-icon"></span>
        <span id="toast-msg"></span>
    </div>

    <script>
        function togglePw(key) {{
            const inp = document.getElementById('pw-' + key);
            const ico = document.getElementById('pw-icon-' + key);
            if (inp.type === 'password') {{
                inp.type = 'text';
                ico.innerHTML = '{ICONS["eye-off"]}';
            }} else {{
                inp.type = 'password';
                ico.innerHTML = '{ICONS["eye"]}';
            }}
        }}

        document.getElementById('cf').addEventListener('submit', async (e) => {{
            e.preventDefault();
            const data = Object.fromEntries(new FormData(e.target));
            try {{
                const r = await fetch('/api/config', {{
                    method: 'POST',
                    headers: {{'Content-Type': 'application/json'}},
                    body: JSON.stringify(data),
                }});
                const j = await r.json();
                toast(j.ok ? '配置已保存' : '保存失败', j.ok);
            }} catch (err) {{
                toast('网络错误: ' + err.message, false);
            }}
        }});

        function toast(msg, ok) {{
            const t = document.getElementById('toast');
            document.getElementById('toast-msg').textContent = msg;
            document.getElementById('toast-icon').innerHTML = ok
                ? '{ICONS["check"]}'
                : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>';
            t.className = 'toast show ' + (ok ? 'toast-ok' : 'toast-err');
            setTimeout(() => t.className = 'toast', 3000);
        }}
    </script>
</body>
</html>'''


class DashboardHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path in ("/", "/index.html"):
            html = render_html()
            self._respond(200, "text/html; charset=utf-8", html.encode("utf-8"))
        elif self.path == "/api/config":
            config = load_current_config()
            if config.get("OPENAI_API_KEY"):
                k = config["OPENAI_API_KEY"]
                config["OPENAI_API_KEY"] = k[:8] + "..." + k[-4:] if len(k) > 12 else "***"
            self._respond(200, "application/json", json.dumps(config).encode("utf-8"))
        else:
            self.send_error(404)

    def do_POST(self):
        if self.path == "/api/config":
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            try:
                save_config(json.loads(body))
                self._respond(200, "application/json", b'{"ok":true}')
            except Exception as e:
                self._respond(500, "application/json",
                              json.dumps({"ok": False, "error": str(e)}).encode("utf-8"))
        else:
            self.send_error(404)

    def _respond(self, code, content_type, body):
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        pass


def main():
    port = 8080
    server = http.server.HTTPServer(("0.0.0.0", port), DashboardHandler)
    print(f"\033[1m\033[36m Control Center\033[0m  http://localhost:{port}")
    print(" Press Ctrl+C to stop\n")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n Stopped")
        server.server_close()


if __name__ == "__main__":
    main()
