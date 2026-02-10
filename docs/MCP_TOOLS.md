# 多Agent编排系统 — MCP 工具总览 (v2)

ACP-BUS 提供 **9 个统一工具**，每个工具通过 `action` 参数切换操作。
Agent 由动态拓扑自动创建和管理，无需预定义。

---

## 1. `iterm` — iTerm 会话管理

管理子 Agent 的 iTerm 会话生命周期：启动、通信、清理。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `list` | `iterm(action="list")` | 查看当前所有 Agent 会话状态 |
| `send` | `iterm(action="send", text="分析日志", agent_id="agent_01")` | 向指定 Agent 发送指令 |
| `send` | `iterm(action="send", text="暂停任务", all_agents=True)` | 群发给所有 Agent |
| `read` | `iterm(action="read", agent_id="agent_01", read_lines=50)` | 读取 Agent 最近输出 |
| `launch` | `iterm(action="launch", agent_name="分析专家", launch_cmd="codex")` | 一键启动新子 Agent（自动 cd + 启动） |
| `clean` | `iterm(action="clean")` | 自动清理已断开的死会话 |
| `unregister` | `iterm(action="unregister", agent_id="agent_01")` | 注销指定 Agent |
| `clear_all` | `iterm(action="clear_all")` | 清空所有会话记录 |

**关键参数：** `text`(发送内容), `agent_id`(目标), `all_agents`(广播), `agent_name`(tab名), `launch_cmd`(启动命令,默认codex), `work_dir`(工作目录,默认项目根)

---

## 2. `shared_file` — 共享文件

Agent 间通过 PostgreSQL 共享文件，用于传递数据、配置、中间产物。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `write` | `shared_file(action="write", path="report.md", content="# 分析报告")` | 写入分析结果供其他 Agent 读取 |
| `read` | `shared_file(action="read", path="report.md")` | 读取其他 Agent 产出的文件 |
| `list` | `shared_file(action="list", path="reports/")` | 按前缀列出共享文件 |
| `delete` | `shared_file(action="delete", path="tmp/draft.md")` | 删除临时文件 |

**关键参数：** `path`(文件路径), `content`(内容), `limit`(列表限制)

---

## 3. `interaction` — Agent 交互记录

Agent 间的消息通信 + 角色发现。子 Agent 可以发现并联系其他 Agent。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `roster` | `interaction(action="roster")` | 获取所有 Agent 角色/ID/技能列表 |
| `register` | `interaction(action="register", sender="agent_01", content="Python,数据分析,代码审查")` | 注册 Agent 技能声明 |
| `create` | `interaction(action="create", sender="a01", receiver="a02", msg_type="request", content="请分析")` | Agent 间发消息 |
| `list` | `interaction(action="list", receiver="a01", status="pending")` | 查看收到的消息 |
| `review` | `interaction(action="review", interaction_id=42, status="done")` | 标记已处理 |

**关键参数：** sender, receiver, msg_type, content(register时为逗号分隔技能), thread_id, interaction_id, status`(审核用)

---

## 4. `prompt_template` — 提示词模板

管理可复用的提示词模板，Agent 可动态加载和切换提示词。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `save` | `prompt_template(action="save", prompt_key="code_review", title="代码审查", prompt_text="请审查以下代码...")` | 保存常用提示词 |
| `get` | `prompt_template(action="get", prompt_key="code_review")` | 加载指定提示词 |
| `list` | `prompt_template(action="list", agent_key="agent_01")` | 查看某 Agent 可用的模板 |
| `toggle` | `prompt_template(action="toggle", prompt_key="code_review", enabled=False)` | 停用模板 |

**关键参数：** `prompt_key`(唯一标识), `title`, `prompt_text`(正文), `agent_key`(绑定Agent), `variables_json`(变量)

---

## 5. `command_card` — 命令卡管理与执行

定义和执行可审批的自动化命令，支持风险分级和审批流程。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `save` | `command_card(action="save", card_key="deploy", title="部署", command_template="./deploy.sh {env}", risk_level="high")` | 创建可复用命令卡 |
| `get` | `command_card(action="get", card_key="deploy")` | 查看命令卡详情 |
| `list` | `command_card(action="list", risk_level="high")` | 查询高风险命令 |
| `toggle` | `command_card(action="toggle", card_key="deploy", enabled=False)` | 停用命令卡 |
| `exec` | `command_card(action="exec", card_key="deploy", params_json='{"env":"staging"}', auto_approve=True)` | 一键执行（准备→审批→执行） |
| `prepare` | `command_card(action="prepare", card_key="deploy", params_json='{"env":"prod"}')` | 仅准备，等待人工审批 |
| `review` | `command_card(action="review", run_id=1, decision="approved")` | 审批执行请求 |
| `exec_run` | `command_card(action="exec_run", run_id=1)` | 执行已审批的任务 |
| `get_run` | `command_card(action="get_run", run_id=1)` | 查看执行详情 |
| `list_runs` | `command_card(action="list_runs", status="pending")` | 查看执行流水 |

**关键参数：** `card_key`(命令卡ID), `params_json`(执行参数), `run_id`(流水ID), `decision`(审批决定), `auto_approve`, `timeout_sec`

---

## 6. `db` — 数据库操作

直接执行 SQL，用于查询系统状态或调试。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `query` | `db(action="query", sql="SELECT * FROM agent_interactions LIMIT 10")` | 只读查询 |
| `execute` | `db(action="execute", sql="UPDATE ... SET status='done'")` | 执行变更 |

**关键参数：** `sql`(SQL语句), `limit`(结果限制)

---

## 7. `task` — 任务管理

Agent 间任务分配与跟踪。支持**任务依赖(DAG)**、**项目分组**、**超时自动重试**。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `create` | `task(action="create", title="优化性能", assignee="a01", priority="high", project_id="proj1", depends_on="T001,T002")` | 创建有依赖的任务 |
| `list` | `task(action="list", status="pending", project_id="proj1")` | 按项目查看待办 |
| `get` | `task(action="get", task_id="T123")` | 查看详情 |
| `update` | `task(action="update", task_id="T123", status="done", result="完成")` | 汇报结果（失败时自动重试） |
| `assign` | `task(action="assign", task_id="T123", assignee="a02")` | 转派 |
| `ready` | `task(action="ready", project_id="proj1")` | 查询依赖已完成、可执行的任务 |
| `progress` | `task(action="progress", project_id="proj1")` | 项目级完成度统计 |
| `cancel` | `task(action="cancel", task_id="T123")` | 取消任务 |

**关键参数：** task_id, title, assignee, creator, priority(low/normal/high/critical), status, result, **depends_on**(逗号分隔task_id), **project_id**(项目分组), **timeout_sec**, **max_retries**

---

## 8. `approval` — 审批与错误处理

Agent 遇到错误或需要决策时，向指定审批 Agent 发起请求，等待回复后继续。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `request` | `approval(action="request", title="DB连接失败", target_agent="审批agent", requester="agent_01", options_json='["重试","跳过","中止"]')` | 遇到错误，请求决策 |
| `respond` | `approval(action="respond", approval_id="A123", decision="重试", approver="审批agent", reason="网络已恢复")` | 回复审批决定 |
| `list` | `approval(action="list", target_agent="审批agent", status="pending")` | 查看待处理审批 |
| `get` | `approval(action="get", approval_id="A123")` | 查看审批详情 |

**关键参数：** `approval_id`, `requester`(发起方), `target_agent`(审批方,必填), `title`(问题描述), `options_json`(可选方案), `decision`(决定), `reason`(理由)

---

## 9. `agent_watchdog` — 看门狗定时唤醒

防止 Agent 对话中断，定期向所有 Agent 发送唤醒提示词。

| Action | 调用示例 | 场景 |
|--------|---------|------|
| `start` | `agent_watchdog(action="start", interval_sec=60, prompt="继续你的任务")` | 启动定时唤醒 |
| `stop` | `agent_watchdog(action="stop")` | 停止唤醒 |
| `status` | `agent_watchdog(action="status")` | 查看当前状态 |

**关键参数：** `interval_sec`(间隔秒数,最小30), `prompt`(唤醒提示词)

---

## 使用方式

1. Agent 通过 MCP 协议连接 acp-bus
2. 所有工具通过 `action` 参数切换操作
3. 子 Agent 启动后先 `interaction(action="register")` 声明技能
4. 通过 `interaction(action="roster")` 发现其他 Agent 及其技能
5. DAG 任务流：`task(create, depends_on)` → `task(ready)` → `iterm(launch)` → `task(update)` → `task(progress)`
6. 错误处理：`approval(request)` → 等处理 → `approval(respond)` → 继续
