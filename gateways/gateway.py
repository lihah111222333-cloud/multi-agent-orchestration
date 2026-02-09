"""Gateway — MCP Client 路由层

每个 Gateway 管理一组 Agent (MCP Server)，负责：
1. 连接 Agent 的 MCP Server
2. 获取所有可用工具
3. 使用 LLM 选择合适的工具执行任务
4. 返回结果
"""

import asyncio
import logging
import traceback
from langchain_mcp_adapters.client import MultiServerMCPClient
from langgraph.prebuilt import create_react_agent
from langchain_openai import ChatOpenAI
from config.settings import (
    LLM_MODEL, LLM_TEMPERATURE, OPENAI_BASE_URL,
    LLM_TIMEOUT, LLM_MAX_RETRIES, GATEWAY_TIMEOUT,
)
from utils import extract_text

logger = logging.getLogger(__name__)


class Gateway:
    """Gateway 管理一组 Agent，将任务路由到合适的 Agent 执行"""

    def __init__(self, name: str, display_name: str, agent_configs: dict):
        """
        Args:
            name: Gateway 标识，如 'gateway_1'
            display_name: 显示名称，如 '数据分析网关'
            agent_configs: 该 Gateway 下的 Agent MCP Server 配置
        """
        self.name = name
        self.display_name = display_name
        self.agent_configs = agent_configs

    async def process(self, task: str) -> str:
        """处理任务（带超时保护）"""
        try:
            return await asyncio.wait_for(
                self._do_process(task),
                timeout=GATEWAY_TIMEOUT,
            )
        except asyncio.TimeoutError:
            error_msg = f"[{self.display_name}] 执行超时 ({GATEWAY_TIMEOUT}s)"
            logger.error(error_msg)
            return error_msg
        except Exception as e:
            error_msg = f"[{self.display_name}] 执行异常: {e}"
            logger.error(error_msg)
            logger.debug(traceback.format_exc())
            return error_msg

    async def _do_process(self, task: str) -> str:
        """实际处理逻辑：连接 Agent → 获取工具 → LLM 选择执行 → 返回结果"""
        logger.info(f"[{self.display_name}] 开始处理任务: {task}")

        client = None
        try:
            # 连接所有 Agent MCP Server
            client = MultiServerMCPClient(self.agent_configs)
            tools = await client.get_tools()

            if not tools:
                raise RuntimeError(f"未能从 {len(self.agent_configs)} 个 Agent 加载任何工具")

            logger.info(
                f"[{self.display_name}] 已加载 {len(tools)} 个工具 "
                f"来自 {len(self.agent_configs)} 个 Agent"
            )

            # 创建 ReAct Agent，使用 LLM 智能选择工具
            llm = ChatOpenAI(
                model=LLM_MODEL,
                temperature=LLM_TEMPERATURE,
                base_url=OPENAI_BASE_URL,
                max_retries=LLM_MAX_RETRIES,
                request_timeout=LLM_TIMEOUT,
            )
            agent = create_react_agent(llm, tools)

            # 执行任务
            result = await agent.ainvoke(
                {"messages": [{"role": "user", "content": task}]}
            )

            output = extract_text(result["messages"][-1].content)
            logger.info(f"[{self.display_name}] 任务完成")
            return output

        except Exception as e:
            error_msg = f"[{self.display_name}] 任务执行失败: {e}"
            logger.error(error_msg)
            logger.debug(traceback.format_exc())
            return error_msg
        finally:
            # 清理 MCP Client 资源（关闭 Agent 子进程）
            if client is not None:
                try:
                    await self._cleanup_client(client)
                except Exception as cleanup_err:
                    logger.warning(
                        f"[{self.display_name}] MCP Client 清理时出错: {cleanup_err}"
                    )

    @staticmethod
    async def _cleanup_client(client: MultiServerMCPClient):
        """清理 MCP Client 创建的子进程和连接"""
        # 尝试关闭所有 session
        if hasattr(client, '_sessions'):
            for name, session in client._sessions.items():
                try:
                    if hasattr(session, 'close'):
                        await session.close()
                except Exception:
                    pass
        if hasattr(client, '_cleanup'):
            try:
                await client._cleanup()
            except Exception:
                pass

    def __repr__(self):
        return (
            f"Gateway(name={self.name}, display={self.display_name}, "
            f"agents={list(self.agent_configs.keys())})"
        )
