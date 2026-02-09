# 多Agent编排 (Multi-Agent Orchestration)

基于 **LangGraph + MCP** 的多 Agent 编排系统。

## 架构

```
Master (LangGraph StateGraph)
├── Gateway 1 → Agent 01~04 (数据分析)
├── Gateway 2 → Agent 05~08 (内容生成)
└── Gateway 3 → Agent 09~12 (系统运维)
```

## 快速开始

### 1. 安装依赖

```bash
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

### 2. 配置环境

```bash
cp .env.example .env
# 编辑 .env 填入你的 LLM API Key
```

### 3. 运行

```bash
# 运行完整编排
python3 run.py "你的任务描述"

# 测试单个 Agent
python3 -m agents.agent_01
```

## 项目结构

```
├── master.py          # Master 编排器 (LangGraph)
├── run.py             # CLI 入口
├── config/
│   └── settings.py    # 全局配置
├── agents/
│   ├── base_agent.py  # Agent 基类
│   └── agent_XX.py    # 12 个 Agent (MCP Server)
└── gateways/
    └── gateway.py     # Gateway (MCP Client + 路由)
```

## 技术栈

- **LangGraph** — 多 Agent 编排
- **MCP (Model Context Protocol)** — Agent 工具协议
- **langchain-mcp-adapters** — LangChain ↔ MCP 桥接
