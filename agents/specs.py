"""Agent 规格中心"""

from agents.factory import AgentSpec, ToolParam, ToolSpec


AGENT_SPECS = {
    "agent_01": AgentSpec(
        key="agent_01",
        server_name="agent-01",
        description="数据采集 Agent",
        tools=(
            ToolSpec(
                name="fetch_market_data",
                description="获取市场数据",
                params=(ToolParam("symbol", str),),
                response_template="[Agent-01] 已采集 {symbol} 的市场数据: price=100.5, volume=50000",
            ),
            ToolSpec(
                name="fetch_news",
                description="获取新闻数据",
                params=(ToolParam("topic", str),),
                response_template="[Agent-01] 已采集关于 '{topic}' 的最新新闻 3 条",
            ),
        ),
    ),
    "agent_02": AgentSpec(
        key="agent_02",
        server_name="agent-02",
        description="数据清洗 Agent",
        tools=(
            ToolSpec(
                name="clean_data",
                description="清洗原始数据，去除异常值",
                params=(ToolParam("dataset", str),),
                response_template="[Agent-02] 已清洗数据集 '{dataset}': 去除 12 条异常记录",
            ),
            ToolSpec(
                name="normalize_data",
                description="归一化数据",
                params=(ToolParam("dataset", str), ToolParam("method", str, "z-score")),
                response_template="[Agent-02] 已对 '{dataset}' 执行 {method} 归一化",
            ),
        ),
    ),
    "agent_03": AgentSpec(
        key="agent_03",
        server_name="agent-03",
        description="数据分析 Agent",
        tools=(
            ToolSpec(
                name="statistical_analysis",
                description="执行统计分析",
                params=(ToolParam("dataset", str),),
                response_template="[Agent-03] '{dataset}' 统计结果: mean=45.2, std=12.3, median=43.0",
            ),
            ToolSpec(
                name="trend_analysis",
                description="执行趋势分析",
                params=(ToolParam("dataset", str), ToolParam("period", str, "7d")),
                response_template="[Agent-03] '{dataset}' {period} 趋势: 上升 3.2%",
            ),
        ),
    ),
    "agent_04": AgentSpec(
        key="agent_04",
        server_name="agent-04",
        description="数据可视化 Agent",
        tools=(
            ToolSpec(
                name="create_chart",
                description="创建图表",
                params=(ToolParam("data", str), ToolParam("chart_type", str, "line")),
                response_template="[Agent-04] 已生成 {chart_type} 图表: data={data}",
            ),
            ToolSpec(
                name="create_dashboard",
                description="创建数据看板",
                params=(ToolParam("title", str),),
                response_template="[Agent-04] 已创建看板 '{title}' 包含 4 个组件",
            ),
        ),
    ),
    "agent_05": AgentSpec(
        key="agent_05",
        server_name="agent-05",
        description="文本生成 Agent",
        tools=(
            ToolSpec(
                name="write_article",
                description="撰写文章",
                params=(ToolParam("topic", str), ToolParam("word_count", int, 500)),
                response_template="[Agent-05] 已撰写关于 '{topic}' 的文章, 约 {word_count} 字",
            ),
            ToolSpec(
                name="summarize_text",
                description="摘要生成",
                params=(ToolParam("text", str),),
                response_builder=lambda values: f"[Agent-05] 已生成摘要: {values['text'][:50]}...",
            ),
        ),
    ),
    "agent_06": AgentSpec(
        key="agent_06",
        server_name="agent-06",
        description="翻译 Agent",
        tools=(
            ToolSpec(
                name="translate",
                description="翻译文本",
                params=(ToolParam("text", str), ToolParam("target_lang", str, "en")),
                response_builder=lambda values: f"[Agent-06] 已将文本翻译为 {values['target_lang']}: '{values['text'][:30]}...'",
            ),
            ToolSpec(
                name="detect_language",
                description="检测语言",
                params=(ToolParam("text", str),),
                response_template="[Agent-06] 检测结果: 语言=zh-CN, 置信度=0.98",
            ),
        ),
    ),
    "agent_07": AgentSpec(
        key="agent_07",
        server_name="agent-07",
        description="代码生成 Agent",
        tools=(
            ToolSpec(
                name="generate_code",
                description="根据描述生成代码",
                params=(ToolParam("description", str), ToolParam("language", str, "python")),
                response_template="[Agent-07] 已生成 {language} 代码: # {description}",
            ),
            ToolSpec(
                name="review_code",
                description="代码审查",
                params=(ToolParam("code", str),),
                response_template="[Agent-07] 代码审查完成: 发现 0 个严重问题, 2 个建议优化",
            ),
        ),
    ),
    "agent_08": AgentSpec(
        key="agent_08",
        server_name="agent-08",
        description="报告生成 Agent",
        tools=(
            ToolSpec(
                name="generate_report",
                description="生成结构化报告",
                params=(ToolParam("title", str), ToolParam("sections", str, "summary,details,conclusion")),
                response_template="[Agent-08] 已生成报告 '{title}', 包含章节: {sections}",
            ),
            ToolSpec(
                name="export_pdf",
                description="导出 PDF",
                params=(ToolParam("report_id", str),),
                response_template="[Agent-08] 报告 {report_id} 已导出为 PDF",
            ),
        ),
    ),
    "agent_09": AgentSpec(
        key="agent_09",
        server_name="agent-09",
        description="系统监控 Agent",
        tools=(
            ToolSpec(
                name="check_system_health",
                description="检查服务健康状态",
                params=(ToolParam("service", str),),
                response_template="[Agent-09] {service} 状态: healthy, CPU=23%, MEM=45%",
            ),
            ToolSpec(
                name="get_metrics",
                description="获取系统指标",
                params=(ToolParam("service", str), ToolParam("period", str, "1h")),
                response_template="[Agent-09] {service} {period} 指标: QPS=1200, P99=45ms, 错误率=0.01%",
            ),
        ),
    ),
    "agent_10": AgentSpec(
        key="agent_10",
        server_name="agent-10",
        description="日志分析 Agent",
        tools=(
            ToolSpec(
                name="search_logs",
                description="搜索日志",
                params=(ToolParam("query", str), ToolParam("time_range", str, "1h")),
                response_template="[Agent-10] 搜索 '{query}' 在 {time_range} 内: 找到 42 条匹配",
            ),
            ToolSpec(
                name="analyze_errors",
                description="分析错误日志",
                params=(ToolParam("service", str),),
                response_template="[Agent-10] {service} 错误分析: TimeoutError 占 60%, ConnectionError 占 25%",
            ),
        ),
    ),
    "agent_11": AgentSpec(
        key="agent_11",
        server_name="agent-11",
        description="部署管理 Agent",
        tools=(
            ToolSpec(
                name="deploy_service",
                description="部署服务",
                params=(ToolParam("service", str), ToolParam("version", str)),
                response_template="[Agent-11] 已部署 {service}:{version}, 状态: running",
            ),
            ToolSpec(
                name="rollback_service",
                description="回滚服务",
                params=(ToolParam("service", str),),
                response_template="[Agent-11] 已回滚 {service} 到上一个版本",
            ),
        ),
    ),
    "agent_12": AgentSpec(
        key="agent_12",
        server_name="agent-12",
        description="告警管理 Agent",
        tools=(
            ToolSpec(
                name="create_alert",
                description="创建告警规则",
                params=(ToolParam("name", str), ToolParam("condition", str)),
                response_template="[Agent-12] 已创建告警 '{name}': 条件={condition}",
            ),
            ToolSpec(
                name="list_active_alerts",
                description="列出活跃告警",
                params=(),
                response_template="[Agent-12] 活跃告警: [WARNING] disk usage > 80%, [INFO] memory spike",
            ),
            ToolSpec(
                name="acknowledge_alert",
                description="确认告警",
                params=(ToolParam("alert_id", str),),
                response_template="[Agent-12] 已确认告警 {alert_id}",
            ),
        ),
    ),
}
