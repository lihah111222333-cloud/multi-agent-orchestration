// methods.go — JSON-RPC 方法注册与初始化入口。
//
// 说明: 具体方法实现已拆分到 methods_*.go，本文件仅保留公共常量与注册逻辑。
package apiserver

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

var codexThreadIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

const (
	defaultLSPUsagePromptHint = "已注入 LSP/Playwright/json-render/code_run 工具。使用规则：\n" +
		"1. 分析/修改源码前，建议优先使用 lsp_open_file 打开目标文件（非强制，可按需直接使用其他 LSP 工具）\n" +
		"2. 凡是源代码的分析、定位、修改与解释，须优先调用下述工具增强代码理解能力\n" +
		"3. 未使用工具前，不得基于猜测给出结论\n" +
		"4. 当需要结构化展示步骤、指标、图表等内容时，请使用 json-render 代码块\n\n" +
		"## LSP 工具指南\n\n" +
		"### 高频工具（日常首选）\n" +
		"- lsp_open_file — 打开目标文件（推荐，非强制前置）\n" +
		"- lsp_hover — 查看符号类型和文档\n" +
		"- lsp_definition — 跳转到定义\n" +
		"- lsp_references — 查找所有引用\n" +
		"- lsp_diagnostics — 获取错误和警告\n" +
		"- lsp_document_symbol — 查看文件结构大纲\n\n" +
		"### 中频工具（按需使用）\n" +
		"- lsp_completion — 代码补全建议\n" +
		"- lsp_rename — 安全重命名符号\n" +
		"- lsp_workspace_symbol — 跨文件搜索符号\n" +
		"- lsp_implementation — 查找接口实现\n" +
		"- lsp_type_definition — 跳转到类型定义\n" +
		"- lsp_did_change — 同步编辑内容到 LSP\n\n" +
		"### 深度分析工具（复杂任务时使用）\n" +
		"- lsp_call_hierarchy — 分析调用链\n" +
		"- lsp_type_hierarchy — 分析类型继承关系\n" +
		"- lsp_code_action — 获取修复建议\n" +
		"- lsp_signature_help — 查看函数签名\n" +
		"- lsp_format — 格式化代码\n" +
		"- lsp_semantic_tokens — 语义级代码理解\n" +
		"- lsp_folding_range — 代码折叠区域\n\n" +
		"## Generative UI (json-render)\n\n" +
		"当需要展示结构化数据 (如 Dashboard、指标、表格、步骤、图表) 时，在回复中使用 json-render 代码块输出 UI spec:\n\n" +
		"```json-render\n" +
		"{\n" +
		"  \"root\": \"element-id\",\n" +
		"  \"elements\": {\n" +
		"    \"element-id\": {\n" +
		"      \"type\": \"ComponentType\",\n" +
		"      \"props\": { ... },\n" +
		"      \"children\": [\"child-id\"]\n" +
		"    }\n" +
		"  }\n" +
		"}\n" +
		"```\n\n" +
		"可用组件:\n" +
		"布局: Card, Stack, Tabs, Accordion\n" +
		"数据展示: Heading, Metric, Stat, Table, List, Badge, Progress, Timeline\n" +
		"图表: Chart (ECharts option)\n" +
		"反馈: Alert\n" +
		"代码: CodeBlock\n" +
		"交互: Button\n" +
		"媒体: Separator, Image, Link\n\n" +
		"## Playwright 浏览器自动化\n\n" +
		"当需要访问网页、截图、提取页面内容、填写表单测试或执行浏览器操作时，优先使用 shell 调用 npx playwright 命令行工具。\n" +
		"常用操作:\n" +
		"- 截图: npx playwright screenshot <url> <output.png>\n" +
		"- 生成代码: npx playwright codegen <url>\n" +
		"- 复杂场景可编写 Node.js 脚本使用 playwright API（导航、点击、填表、提取文本、执行 JS）\n" +
		"使用完毕后确保关闭浏览器释放资源。\n\n" +
		"## 代码执行工具\n\n" +
		"1. 运行代码片段: code_run (mode=run) — 支持 Go, JavaScript, TypeScript\n" +
		"  - Go 代码默认 auto_wrap=true, 自动补全 package main 和 imports\n" +
		"  - JS/TS 代码直接执行, 无需额外配置\n" +
		"2. 执行测试: code_run_test — go test -v -run ^TestFunc$ [package]\n" +
		"3. 项目命令: code_run (mode=project_cmd) — 执行 shell 命令 (仅高风险命令需要用户审批)\n" +
		"安全约束: 输出上限 512KB, 默认超时 30s, 代码在临时目录隔离执行。\n" +
		"优先使用 code_run 验证代码逻辑, 使用 code_run_test 验证测试结果。"
	prefKeyLSPUsagePromptHint = "settings.lspUsagePromptHint"
	maxLSPUsagePromptHintLen  = 16000
)

// registerMethods 注册所有 JSON-RPC 方法 (完整对标 APP-SERVER-PROTOCOL.md)。
func (s *Server) registerMethods() {
	noop := noopHandler()

	// § 1. 初始化
	s.methods["initialize"] = s.initialize
	s.methods["initialized"] = noop

	// § 2. 线程生命周期 (12 methods)
	s.methods["thread/start"] = typedHandler(s.threadStartTyped)
	s.methods["thread/resume"] = typedHandler(s.threadResumeTyped)
	s.methods["thread/fork"] = typedHandler(s.threadForkTyped)
	s.methods["thread/archive"] = typedHandler(s.threadArchiveTyped)
	s.methods["thread/unarchive"] = typedHandler(s.threadUnarchiveTyped)
	s.methods["thread/name/set"] = typedHandler(s.threadNameSetTyped)
	s.methods["thread/compact/start"] = s.threadCompact
	s.methods["thread/rollback"] = typedHandler(s.threadRollbackTyped)
	s.methods["thread/list"] = s.threadList
	s.methods["thread/loaded/list"] = s.threadLoadedList
	s.methods["thread/read"] = typedHandler(s.threadReadTyped)
	s.methods["thread/resolve"] = typedHandler(s.threadResolveTyped)
	s.methods["thread/messages"] = typedHandler(s.threadMessagesTyped)
	s.methods["thread/backgroundTerminals/clean"] = s.threadBgTerminalsClean

	// § 3. 对话控制 (4 methods)
	s.methods["turn/start"] = typedHandler(s.turnStartTyped)
	s.methods["turn/steer"] = typedHandler(s.turnSteerTyped)
	s.methods["turn/interrupt"] = s.turnInterrupt
	s.methods["turn/forceComplete"] = s.turnForceComplete
	s.methods["review/start"] = typedHandler(s.reviewStartTyped)

	// § 4. 文件搜索 (4 methods)
	s.methods["fuzzyFileSearch"] = typedHandler(s.fuzzyFileSearchTyped)
	s.methods["fuzzyFileSearch/sessionStart"] = noop
	s.methods["fuzzyFileSearch/sessionUpdate"] = noop
	s.methods["fuzzyFileSearch/sessionStop"] = noop

	// § 5. Skills / Apps (5 methods)
	s.methods["skills/list"] = s.skillsList
	s.methods["skills/local/read"] = typedHandler(s.skillsLocalReadTyped)
	s.methods["skills/local/importDir"] = typedHandler(s.skillsLocalImportDirTyped)
	s.methods["skills/local/delete"] = typedHandler(s.skillsLocalDeleteTyped)
	s.methods["skills/remote/read"] = typedHandler(s.skillsRemoteReadTyped)
	s.methods["skills/remote/write"] = typedHandler(s.skillsRemoteWriteTyped)
	s.methods["skills/config/read"] = typedHandler(s.skillsConfigReadTyped)
	s.methods["skills/config/write"] = typedHandler(s.skillsConfigWriteTyped)
	s.methods["skills/summary/write"] = typedHandler(s.skillsSummaryWriteTyped)
	s.methods["skills/match/preview"] = typedHandler(s.skillsMatchPreviewTyped)
	s.methods["app/list"] = s.appList

	// § 6. 模型 / 配置 (7 methods)
	s.methods["model/list"] = s.modelList
	s.methods["collaborationMode/list"] = s.collaborationModeList
	s.methods["experimentalFeature/list"] = s.experimentalFeatureList
	s.methods["config/read"] = s.configRead
	s.methods["config/value/write"] = typedHandler(s.configValueWriteTyped)
	s.methods["config/batchWrite"] = typedHandler(s.configBatchWriteTyped)
	s.methods["config/lspPromptHint/read"] = s.configLSPPromptHintRead
	s.methods["config/lspPromptHint/write"] = typedHandler(s.configLSPPromptHintWriteTyped)
	s.methods["configRequirements/read"] = s.configRequirementsRead

	// § 7. 账号 (5 methods)
	s.methods["account/login/start"] = typedHandler(s.accountLoginStartTyped)
	s.methods["account/login/cancel"] = s.accountLoginCancel
	s.methods["account/logout"] = s.accountLogout
	s.methods["account/read"] = s.accountRead
	s.methods["account/rateLimits/read"] = s.accountRateLimitsRead

	// § 8. MCP (3 methods)
	s.methods["mcpServer/oauth/login"] = noop
	s.methods["config/mcpServer/reload"] = s.mcpServerReload
	s.methods["mcpServerStatus/list"] = s.mcpServerStatusList
	s.methods["lsp_diagnostics_query"] = typedHandler(s.lspDiagnosticsQueryTyped)

	// § 9. 命令执行 / 其他 (2 methods)
	s.methods["command/exec"] = typedHandler(s.commandExecTyped)
	s.methods["feedback/upload"] = noop

	// § 10. 斜杠命令 (SOCKS 独有, JSON-RPC 化)
	s.methods["thread/undo"] = s.threadUndo
	s.methods["thread/model/set"] = s.threadModelSet
	s.methods["thread/personality/set"] = s.threadPersonality
	s.methods["thread/approvals/set"] = s.threadApprovals
	s.methods["thread/mcp/list"] = s.threadMCPList
	s.methods["thread/skills/list"] = s.threadSkillsList
	s.methods["thread/debugMemory"] = s.threadDebugMemory

	// § 11. 系统日志查询 (2 methods)
	s.methods["log/list"] = typedHandler(s.logListTyped)
	s.methods["log/filters"] = s.logFilters

	// § 12. Dashboard 数据查询 (12 methods, 替代 Wails Dashboard 绑定)
	s.registerDashboardMethods()

	// § 13. Workspace Run (双通道编排: 虚拟目录 + PG 状态)
	s.methods["workspace/run/create"] = s.workspaceRunCreate
	s.methods["workspace/run/get"] = s.workspaceRunGet
	s.methods["workspace/run/list"] = s.workspaceRunList
	s.methods["workspace/run/merge"] = s.workspaceRunMerge
	s.methods["workspace/run/abort"] = s.workspaceRunAbort

	// § 14. UI State (UI 偏好持久化)
	s.methods["ui/preferences/get"] = typedHandler(s.uiPreferencesGet)
	s.methods["ui/preferences/set"] = typedHandler(s.uiPreferencesSet)
	s.methods["ui/preferences/getAll"] = s.uiPreferencesGetAll
	s.methods["ui/projects/get"] = s.uiProjectsGet
	s.methods["ui/projects/add"] = typedHandler(s.uiProjectsAdd)
	s.methods["ui/projects/remove"] = typedHandler(s.uiProjectsRemove)
	s.methods["ui/projects/setActive"] = typedHandler(s.uiProjectsSetActive)
	s.methods["ui/code/open"] = typedHandler(s.uiCodeOpenTyped)
	s.methods["ui/dashboard/get"] = typedHandler(s.uiDashboardGet)
	s.methods["ui/state/get"] = s.uiStateGet

	// § 15. Debug (运行时诊断)
	s.methods["debug/runtime"] = s.debugRuntime
	s.methods["debug/gc"] = s.debugForceGC

	// § 16. 前端兼容 Stub (返回空数据, 防止前端 "unregistered method" 报错)
	//
	// 这些方法对应原 Codex Electron 前端的查询接口, 当前 Go 后端尚未实现。
	// 注册空响应使前端能正常渲染, 后续按需逐步实现。
	s.methods["workspace-root-options"] = stubHandler(map[string]any{"roots": []any{}, "labels": map[string]any{}})
	s.methods["codex-home"] = stubHandler(map[string]any{"codexHome": ""})
	s.methods["git-origins"] = stubHandler(map[string]any{"origins": []any{}})
	s.methods["mcp-servers"] = stubHandler(map[string]any{"servers": []any{}})
	s.methods["platform-info"] = stubHandler(map[string]any{"platform": "darwin", "arch": "arm64"})
	s.methods["open-in-targets"] = stubHandler(map[string]any{"targets": []any{}})
	s.methods["codex-agents-md"] = stubHandler(map[string]any{})
	s.methods["local-environments/list"] = stubHandler(map[string]any{"environments": []any{}})
	s.methods["worktrees/list"] = stubHandler(map[string]any{"worktrees": []any{}})
	s.methods["tasks/list"] = stubHandler([]any{})
	s.methods["tasks/get"] = stubHandler(map[string]any{})
	s.methods["inbox-items"] = stubHandler(map[string]any{"items": []any{}})
	s.methods["inbox-items/get"] = stubHandler(map[string]any{})
	s.methods["pending-automation-runs"] = stubHandler(map[string]any{"runs": []any{}})
	s.methods["mcp/status"] = stubHandler(map[string]any{})
	s.methods["config/read-all"] = stubHandler(map[string]any{})
	s.methods["diff/get"] = stubHandler(map[string]any{})

	if s.cfg != nil && s.cfg.DisableOffline52Methods {
		for _, method := range offline52MethodList() {
			delete(s.methods, method)
		}
	}
}

// ========================================
// 初始化
// ========================================

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	ClientInfo      any    `json:"clientInfo,omitempty"`
	Capabilities    any    `json:"capabilities,omitempty"`
}

func (s *Server) initialize(_ context.Context, params json.RawMessage) (any, error) {
	var p initializeParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			logger.Debug("initialize: unmarshal params", logger.FieldError, err)
		}
	}
	return map[string]any{
		"protocolVersion": "2.0",
		"serverInfo": map[string]string{
			"name":    "codex-go-app-server",
			"version": "0.1.0",
		},
		"capabilities": map[string]bool{
			"threads":    true,
			"turns":      true,
			"fileSearch": true,
			"skills":     true,
			"exec":       true,
		},
	}, nil
}
