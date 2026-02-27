---
name: MCP服务器构建
description: 构建 Model Context Protocol (MCP) 服务器的指南，用于将外部 API 和服务与 LLM 集成。支持 Python 和 TypeScript 实现。
tags: [mcp, 集成, api, 服务器, python, typescript, llm, 工具]
---

# MCP 服务器构建

## 何时使用

在以下场景使用此技能：
- 为 AI 助手添加新的外部服务集成
- 创建自定义工具供 LLM 调用
- 构建资源提供者服务
- 扩展 AI 助手能力

---

## MCP 概述

Model Context Protocol (MCP) 是一个开放协议，用于将 LLM 与外部数据源和工具连接。

### 核心概念

| 概念 | 描述 |
|------|------|
| **Server** | 提供工具和资源的服务端 |
| **Tool** | 可被 LLM 调用的函数 |
| **Resource** | 可被读取的数据资源 |
| **Prompt** | 预定义的提示模板 |

---

## 第一部分：Python 实现

### 项目结构

```
my-mcp-server/
├── src/
│   └── my_server/
│       ├── __init__.py
│       ├── server.py
│       └── tools.py
├── pyproject.toml
└── README.md
```

### 基础服务器

```python
# src/my_server/server.py
from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import Tool, TextContent

# 创建服务器实例
server = Server("my-mcp-server")

# 注册工具列表处理器
@server.list_tools()
async def list_tools():
    return [
        Tool(
            name="greet",
            description="向用户问候",
            inputSchema={
                "type": "object",
                "properties": {
                    "name": {
                        "type": "string",
                        "description": "用户姓名"
                    }
                },
                "required": ["name"]
            }
        ),
        Tool(
            name="calculate",
            description="执行数学计算",
            inputSchema={
                "type": "object",
                "properties": {
                    "expression": {
                        "type": "string",
                        "description": "数学表达式"
                    }
                },
                "required": ["expression"]
            }
        )
    ]

# 注册工具调用处理器
@server.call_tool()
async def call_tool(name: str, arguments: dict):
    if name == "greet":
        user_name = arguments.get("name", "用户")
        return [TextContent(type="text", text=f"你好，{user_name}！欢迎使用 MCP 服务器。")]
    
    elif name == "calculate":
        expression = arguments.get("expression", "")
        try:
            # 安全计算（仅支持基础运算）
            result = eval(expression, {"__builtins__": {}}, {})
            return [TextContent(type="text", text=f"计算结果: {result}")]
        except Exception as e:
            return [TextContent(type="text", text=f"计算错误: {str(e)}")]
    
    return [TextContent(type="text", text=f"未知工具: {name}")]

# 主入口
async def main():
    async with stdio_server() as (read_stream, write_stream):
        await server.run(read_stream, write_stream)

if __name__ == "__main__":
    import asyncio
    asyncio.run(main())
```

### pyproject.toml

```toml
[project]
name = "my-mcp-server"
version = "0.1.0"
description = "自定义 MCP 服务器"
requires-python = ">=3.10"
dependencies = [
    "mcp>=0.9.0",
]

[project.scripts]
my-mcp-server = "my_server.server:main"

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"
```

---

## 第二部分：TypeScript 实现

### 项目结构

```
my-mcp-server/
├── src/
│   ├── index.ts
│   └── tools/
│       └── weather.ts
├── package.json
└── tsconfig.json
```

### 基础服务器

```typescript
// src/index.ts
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  ListToolsRequestSchema,
  CallToolRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

const server = new Server(
  { name: "my-mcp-server", version: "0.1.0" },
  { capabilities: { tools: {} } }
);

// 工具列表
server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [
    {
      name: "get_weather",
      description: "获取指定城市的天气信息",
      inputSchema: {
        type: "object",
        properties: {
          city: { type: "string", description: "城市名称" }
        },
        required: ["city"]
      }
    }
  ]
}));

// 工具调用
server.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args } = request.params;
  
  if (name === "get_weather") {
    const city = args?.city as string;
    // 模拟天气数据
    return {
      content: [
        { type: "text", text: `${city}今日天气：晴，温度 22°C，湿度 65%` }
      ]
    };
  }
  
  throw new Error(`未知工具: ${name}`);
});

// 启动服务器
async function main() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch(console.error);
```

### package.json

```json
{
  "name": "my-mcp-server",
  "version": "0.1.0",
  "type": "module",
  "main": "dist/index.js",
  "scripts": {
    "build": "tsc",
    "start": "node dist/index.js"
  },
  "dependencies": {
    "@modelcontextprotocol/sdk": "^0.5.0"
  },
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}
```

---

## 第三部分：资源提供

### 提供文件资源

```python
from mcp.types import Resource, TextResourceContents

@server.list_resources()
async def list_resources():
    return [
        Resource(
            uri="file:///config/settings.json",
            name="配置文件",
            description="应用程序配置",
            mimeType="application/json"
        )
    ]

@server.read_resource()
async def read_resource(uri: str):
    if uri == "file:///config/settings.json":
        config = {"theme": "dark", "language": "zh-CN"}
        return TextResourceContents(
            uri=uri,
            mimeType="application/json",
            text=json.dumps(config, ensure_ascii=False)
        )
    raise ValueError(f"未知资源: {uri}")
```

---

## 第四部分：配置与部署

### Claude Desktop 配置

在 `~/Library/Application Support/Claude/claude_desktop_config.json` 中添加：

```json
{
  "mcpServers": {
    "my-server": {
      "command": "python",
      "args": ["-m", "my_server.server"],
      "cwd": "/path/to/my-mcp-server"
    }
  }
}
```

### 调试

```bash
# 安装调试工具
npm install -g @modelcontextprotocol/inspector

# 运行调试器
mcp-inspector python -m my_server.server
```

---

## 最佳实践

1. **输入验证** - 始终验证工具输入参数
2. **错误处理** - 返回用户友好的错误信息
3. **文档完善** - 工具描述应清晰准确
4. **安全考虑** - 避免执行不受信任的代码
5. **日志记录** - 记录关键操作便于调试

---

## 常见模式

### API 集成模式

```python
import httpx

@server.call_tool()
async def call_tool(name: str, arguments: dict):
    if name == "search_products":
        async with httpx.AsyncClient() as client:
            response = await client.get(
                "https://api.example.com/products",
                params={"q": arguments["query"]}
            )
            data = response.json()
            return [TextContent(type="text", text=json.dumps(data, ensure_ascii=False))]
```

### 数据库查询模式

```python
import asyncpg

@server.call_tool()
async def call_tool(name: str, arguments: dict):
    if name == "query_users":
        conn = await asyncpg.connect(DATABASE_URL)
        try:
            rows = await conn.fetch("SELECT * FROM users LIMIT 10")
            return [TextContent(type="text", text=str(rows))]
        finally:
            await conn.close()
```


---

## ⚠️ 强制输出 Token 空间

> **重要规则**：使用此技能时，必须在每次重要输出前检查上下文空间。

### 输出规范

所有对话回复内容都要输出

### 输出格式

```
📊 剩余上下文空间: ~{百分比}%
```

### 告警与自动保存

**当剩余上下文空间 ≤ 30%（即已使用 ≥ 70%）时，必须执行：**

1. **立即暂停当前工作**
2. **保存工作进度**：创建 `.agent/workflows/checkpoint-{timestamp}.md`
3. **通知用户**：
   ```
   ⚠️ 上下文空间即将耗尽 (剩余 ~{百分比}%)
   📋 工作进度已保存至: .agent/workflows/checkpoint-{timestamp}.md
   请检查后决定是否继续或开启新对话
   ```
