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
	defaultLSPUsagePromptHint = "已注入LSP工具。凡是源代码的分析、定位、修改与解释，必须调用lsp_open_file、lsp_hover、lsp_diagnostics；未使用上述工具前，不得基于猜测给出结论"
	prefKeyLSPUsagePromptHint = "settings.lspUsagePromptHint"
	maxLSPUsagePromptHintLen  = 4000

	prefKeyJsonRenderPrompt = "settings.jsonRenderPrompt"
	maxJsonRenderPromptLen  = 8000

	defaultBrowserPrompt = "已注入 Playwright 浏览器自动化能力。" +
		"当需要访问网页、截图、提取页面内容、填写表单或执行浏览器操作时，" +
		"请使用 shell 调用 npx playwright 命令行工具。" +
		"常用操作:\n" +
		"- 截图: npx playwright screenshot <url> <output.png>\n" +
		"- 生成代码: npx playwright codegen <url>\n" +
		"- 也可编写 Node.js 脚本使用 playwright API 进行复杂操作（导航、点击、填表、提取文本、执行 JS 等）\n" +
		"- 脚本示例: const { chromium } = require('playwright'); const browser = await chromium.launch(); const page = await browser.newPage(); await page.goto('url'); ...\n" +
		"优先使用 playwright CLI，复杂场景再写脚本。使用完毕后确保关闭浏览器释放资源。"
	prefKeyBrowserPrompt = "settings.browserPrompt"
	maxBrowserPromptLen  = 4000
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
	s.methods["config/jsonRenderPrompt/read"] = s.configJsonRenderPromptRead
	s.methods["config/jsonRenderPrompt/write"] = typedHandler(s.configJsonRenderPromptWriteTyped)
	s.methods["config/browserPrompt/read"] = s.configBrowserPromptRead
	s.methods["config/browserPrompt/write"] = typedHandler(s.configBrowserPromptWriteTyped)
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
