"""全局配置"""

import os
from dotenv import load_dotenv

load_dotenv()

# ========================
# LLM 配置
# ========================
LLM_MODEL = os.getenv("LLM_MODEL", "gpt-4o")
LLM_TEMPERATURE = float(os.getenv("LLM_TEMPERATURE", "0.7"))
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "")

# ========================
# Gateway → Agent 映射
# ========================
GATEWAY_AGENT_MAP = {
    "gateway_1": {
        "name": "数据分析网关",
        "agents": {
            "agent_01": {"command": "python3", "args": ["-m", "agents.agent_01"], "transport": "stdio"},
            "agent_02": {"command": "python3", "args": ["-m", "agents.agent_02"], "transport": "stdio"},
            "agent_03": {"command": "python3", "args": ["-m", "agents.agent_03"], "transport": "stdio"},
            "agent_04": {"command": "python3", "args": ["-m", "agents.agent_04"], "transport": "stdio"},
        },
    },
    "gateway_2": {
        "name": "内容生成网关",
        "agents": {
            "agent_05": {"command": "python3", "args": ["-m", "agents.agent_05"], "transport": "stdio"},
            "agent_06": {"command": "python3", "args": ["-m", "agents.agent_06"], "transport": "stdio"},
            "agent_07": {"command": "python3", "args": ["-m", "agents.agent_07"], "transport": "stdio"},
            "agent_08": {"command": "python3", "args": ["-m", "agents.agent_08"], "transport": "stdio"},
        },
    },
    "gateway_3": {
        "name": "系统运维网关",
        "agents": {
            "agent_09": {"command": "python3", "args": ["-m", "agents.agent_09"], "transport": "stdio"},
            "agent_10": {"command": "python3", "args": ["-m", "agents.agent_10"], "transport": "stdio"},
            "agent_11": {"command": "python3", "args": ["-m", "agents.agent_11"], "transport": "stdio"},
            "agent_12": {"command": "python3", "args": ["-m", "agents.agent_12"], "transport": "stdio"},
        },
    },
}

# ========================
# 日志配置
# ========================
LOG_LEVEL = os.getenv("LOG_LEVEL", "INFO")
