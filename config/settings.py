"""全局配置"""

import os
from dotenv import load_dotenv

load_dotenv()

# ========================
# LLM 配置
# ========================
LLM_MODEL = os.getenv("LLM_MODEL", "gpt-5.2")
LLM_TEMPERATURE = float(os.getenv("LLM_TEMPERATURE", "0.7"))
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "")
OPENAI_BASE_URL = os.getenv("OPENAI_BASE_URL", None)  # 支持第三方中转 API

# LLM 健壮性配置
LLM_TIMEOUT = int(os.getenv("LLM_TIMEOUT", "60"))          # 单次 LLM 调用超时(秒)
LLM_MAX_RETRIES = int(os.getenv("LLM_MAX_RETRIES", "3"))   # LLM 调用最大重试次数
GATEWAY_TIMEOUT = int(os.getenv("GATEWAY_TIMEOUT", "120"))  # 单个 Gateway 执行超时(秒)

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
