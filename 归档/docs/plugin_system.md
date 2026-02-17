# 插件系统（Wave3 最小可用）

## 目录规范

- 根目录：`plugins/`
- 每个插件一个子目录：`plugins/<plugin_name>/`
- 插件入口固定为：`plugins/<plugin_name>/plugin.py`

## 插件契约

每个 `plugin.py` 必须导出：

- `PLUGIN_NAME: str`
- `PLUGIN_TOOLS: tuple[ToolSpec, ...] | list[ToolSpec]`

说明：

- `PLUGIN_NAME` 必须与声明加载名一致。
- `PLUGIN_TOOLS` 中仅允许 `ToolSpec`，不允许任意 shell 执行能力。

## AgentSpec 插件声明

- `AgentSpec` 新增字段：`plugins: tuple[str, ...] = ()`
- 动态 Agent 可通过 `agents.runtime_agent --plugins http_fetch,db_query` 声明插件
- `config.json` 中 agent 节点支持：`"plugins": ["http_fetch", "db_query"]`

## Loader 行为

- 扫描 `plugins/*/plugin.py`
- 校验重复插件名
- 按声明名加载插件并校验：
  - 插件存在
  - `PLUGIN_NAME` 匹配
  - 存在且校验 `PLUGIN_TOOLS`

## 示例插件

- `plugins/http_fetch/plugin.py`：`http_fetch(url, timeout_sec=5)`
- `plugins/db_query/plugin.py`：`db_query(sql, limit=200)`

> 当前为最小可用骨架，插件工具逻辑为受限模拟实现。
