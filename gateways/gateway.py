"""Gateway — MCP Client 路由层

每个 Gateway 管理一组 Agent (MCP Server)，负责：
1. 连接 Agent 的 MCP Server
2. 获取所有可用工具
3. 使用 LLM 选择合适的工具执行任务
4. 返回结果
"""

import logging
from langchain_mcp_adapters.client import MultiServerMCPClient
from langgraph.prebuilt import create_react_agent
from langchain_openai import ChatOpenAI
from config.settings import LLM_MODEL, LLM_TEMPERATURE

logger = logging.getLogger(__name__)


class Gateway:
    """Gateway 管理一组 Agent，将任务路由到合适的 Agent 执行"""

    def __init__(self, name: str, display_name: str, agent_configs: dict):
        """
        Args:
            name: Gateway 标识，如 'gateway_1'
            display_name: 显示名称，如 '数据分析网关'
            agent_configs: 该 Gateway 下的 Agent MCP Server 配置
                格式: {
                    "agent_01": {"command": "python3", "args": [...], "transport": "stdio"},
                    ...
                }
        """
        self.name = name
        self.display_name = display_name
        self.agent_configs = agent_configs

    async def process(self, task: str) -> str:
        """处理任务：连接所有 Agent → 获取工具 → LLM 选择执行 → 返回结果

        Args:
            task: 要执行的任务描述

        Returns:
            执行结果文本
        """
        logger.info(f"[{self.display_name}] 开始处理任务: {task}")

        try:
            async with MultiServerMCPClient(self.agent_configs) as client:
                # 获取该 Gateway 下所有 Agent 注册的工具
                tools = client.get_tools()
                logger.info(
                    f"[{self.display_name}] 已加载 {len(tools)} 个工具 "
                    f"来自 {len(self.agent_configs)} 个 Agent"
                )

                # 创建 ReAct Agent，使用 LLM 智能选择工具
                llm = ChatOpenAI(model=LLM_MODEL, temperature=LLM_TEMPERATURE)
                agent = create_react_agent(llm, tools)

                # 执行任务
                result = await agent.ainvoke(
                    {"messages": [{"role": "user", "content": task}]}
                )

                output = result["messages"][-1].content
                logger.info(f"[{self.display_name}] 任务完成")
                return output

        except Exception as e:
            error_msg = f"[{self.display_name}] 任务执行失败: {e}"
            logger.error(error_msg)
            return error_msg

    def __repr__(self):
        return (
            f"Gateway(name={self.name}, display={self.display_name}, "
            f"agents={list(self.agent_configs.keys())})"
        )
