"""Agent 规格中心

动态拓扑模式下不再预定义固定 Agent，所有 Agent 运行时通过
factory._default_dynamic_spec() 自动生成通用规格。
"""

AGENT_SPECS: dict = {}
