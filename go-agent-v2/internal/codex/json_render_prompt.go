// json_render_prompt.go — json-render Generative UI 系统提示词。
//
// 此 prompt 段落描述可用的 UI 组件, 注入 AI 的 system instructions,
// 使 AI 在需要结构化展示时输出 json-render spec。
package codex

// JsonRenderCatalogPrompt 描述可用的 json-render UI 组件。
//
// 注入 thread/start 的 instructions 中,
// 让 AI 知道可以输出 ```json-render 代码块。
const JsonRenderCatalogPrompt = `## Generative UI (json-render)

当需要展示结构化数据 (如 Dashboard、指标、表格、步骤、图表) 时,
在回复中使用 ` + "`" + `json-render` + "`" + ` 代码块输出 UI spec:

` + "```json-render" + `
{
  "root": "element-id",
  "elements": {
    "element-id": {
      "type": "ComponentType",
      "props": { ... },
      "children": ["child-id"]
    }
  }
}
` + "```" + `

### 可用组件

**布局:**
- **Card** — 卡片容器 (props: title?, description?; 支持 children)
- **Stack** — 垂直/水平布局 (props: direction?=row|column, gap?; 支持 children)
- **Tabs** — 标签页 (props: tabs=[{key,label}], defaultTab?; 支持 children)
- **Accordion** — 折叠面板 (props: title, open?; 支持 children)

**数据展示:**
- **Heading** — 标题 (props: text, level?=1|2|3|4)
- **Metric** — 指标 (props: label, value, format?=currency|percent|number)
- **Stat** — 指标+趋势 (props: label, value, change?, trend?=up|down)
- **Table** — 表格 (props: columns=[{header,align?}], rows=string[][])
- **List** — 列表 (props: items=string[], ordered?)
- **Badge** — 标签 (props: text, variant?=default|primary|success|warning|error)
- **Progress** — 进度条 (props: value, max?=100, label?)
- **Timeline** — 时间线 (props: items=[{title,description?,time?,status?=done|active|pending}])

**图表:**
- **Chart** — 交互式图表 (props: option=ECharts配置对象, width?="100%", height?="300px", theme?=dark)
  option 遵循 ECharts 标准格式, 至少包含 series, 常用图表类型: line/bar/pie/scatter/radar
  示例 option: {"xAxis":{"type":"category","data":["周一","周二","周三"]},"yAxis":{"type":"value"},"series":[{"type":"line","data":[120,200,150]}]}

**反馈:** **Alert** — 提示框 (props: message, variant?=info|warning|error|success, title?)
**代码:** **CodeBlock** — 代码块 (props: code, language?)
**交互:** **Button** — 按钮 (props: label, variant?=default|primary|danger, action?)
**媒体:** **Separator** — 分隔线 (props: label?), **Image** — 图片 (props: src, alt?, width?, caption?), **Link** — 链接 (props: href, text?)

普通对话直接用 markdown, 不需要 json-render。仅在需要结构化 UI 展示时使用。`
