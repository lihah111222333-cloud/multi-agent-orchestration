# 命令读写判定与 LSP 路由 实现计划

> **给 Claude:** 必须使用 @执行计划 逐任务实现此计划。

**目标:** 实现“编程命令 + 代码文件”优先路由到 LSP；同一 turn 仅提示一次；其余命令保持软路由与可回退。

**架构:** 在 `apiserver` 增加纯函数路由层（意图分类 + 路由决策），避免把策略散落在 `methods_command.go` 与 `server_payload.go`。`command/exec` 路径执行硬路由（可直接返回 LSP 结果），Codex shell 事件路径执行软路由提示（不阻断命令执行）。使用 turn tracker 元信息做“每 turn 一次”去重。

**技术栈:** Go 1.22, JSON-RPC apiserver, internal/lsp ToolHandlers, `go test`（参考 @golang-backend-development）

---

## 约束与边界

- 只处理“读/搜索代码文件”路由到 LSP。
- 写操作（如 `sed -i`, `mv`, `rm`）不做 LSP 路由。
- 复杂 shell 语义（管道、重定向、子命令、复杂正则）不路由，直接走 shell。
- `command/exec` 中移除“每次都在 stdout 注入提示”的行为，避免上下文污染。
- 保留逃生口：`--raw` 参数或 `FORCE_SHELL=1` 环境变量时强制走 shell。
- 当前工作树已有不相关脏改动（`turn_tracker*`）；本计划实现时不要触碰那些文件。

## 路由规则（实现目标）

- 硬路由（LSP 直接执行）: `cat|bat <single-code-file>`；`rg|grep <identifier> <code-path-or-file>`（无复杂 flag/regex）
- 软路由（提示一次后继续 shell）: `head|tail|less|more` 且命中代码文件
- 不路由: 非代码文件、写操作、复杂 shell 语法、LSP 不可用

硬路由判定必须同时满足：
- 命令在硬路由白名单内
- 参数里存在可解析的代码文件/代码目录
- 不包含复杂语法或高风险 flag（示例：`-i`, `--replace`, 管道、重定向）

## 编程命令路由白名单（首版）

- 直接硬路由（意图 100% 明确）: `cat`, `bat`, `rg`, `grep`
- 软路由（提示一次 + 继续 shell）: `head`, `tail`, `less`, `more`
- 强制 shell（即使命中代码文件）: `sed -i`, `mv`, `cp`, `rm`, 含管道/重定向/子命令的组合命令

## 务实建议与兜底策略（必须实现）

- 默认策略必须保守: `scope=code_only` + `mode=mixed`（先软后硬，禁“一刀切”）
- 不追求 100% 替代 shell: 仅替代“读/搜索代码语义”场景
- 任意失败优先回退 shell: 不允许因路由导致任务中断
- 同一 turn 最多提示一次: 降低上下文噪音
- 全链路可观测: 每次路由必须记录 `route_mode`、`route_reason`、`fallback`

## 后端配置项（新增）

- `settings.commandRoute.enabled`（bool）: 是否启用路由
- `settings.commandRoute.scope`（string）: `code_only` | `all`
- `settings.commandRoute.mode`（string）: `soft_only` | `mixed` | `hard_only`
- `settings.commandRoute.oncePerTurnHint`（bool）: 是否每 turn 仅提示一次
- `settings.commandRoute.forceShellFlags`（[]string）: 例如 `--raw`
- `settings.commandRoute.fallbackWhenLspUnavailable`（bool）: LSP 不可用是否自动回退 shell
- `settings.commandRoute.codeExtensions`（[]string）: 代码扩展白名单

---

### 任务 0: 落地后端可配置策略（默认务实模式）

**文件:**
- 修改: `internal/apiserver/methods_config.go`
- 创建: `internal/apiserver/command_route_config.go`
- 创建: `internal/apiserver/command_route_config_test.go`
- 测试: `internal/apiserver/command_route_config_test.go`

**步骤 1: 写失败的测试**

```go
func TestCommandRouteConfig_DefaultPragmatic(t *testing.T) {
	cfg := defaultCommandRouteConfig()
	if !cfg.Enabled {
		t.Fatal("expected enabled by default")
	}
	if cfg.Scope != "code_only" || cfg.Mode != "mixed" {
		t.Fatalf("unexpected default: %+v", cfg)
	}
	if !cfg.OncePerTurnHint || !cfg.FallbackWhenLSPUnavailable {
		t.Fatalf("default fallback/hint must be enabled: %+v", cfg)
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/apiserver -run TestCommandRouteConfig_DefaultPragmatic -count=1`
预期: FAIL，提示 `undefined: defaultCommandRouteConfig`

**步骤 3: 写最小实现**

```go
type CommandRouteConfig struct {
	Enabled                    bool
	Scope                      string
	Mode                       string
	OncePerTurnHint            bool
	ForceShellFlags            []string
	FallbackWhenLSPUnavailable bool
	CodeExtensions             []string
}

func defaultCommandRouteConfig() CommandRouteConfig {
	return CommandRouteConfig{
		Enabled:                    true,
		Scope:                      "code_only",
		Mode:                       "mixed",
		OncePerTurnHint:            true,
		ForceShellFlags:            []string{"--raw"},
		FallbackWhenLSPUnavailable: true,
		CodeExtensions:             defaultCodeExtensions(),
	}
}
```

并在配置读取路径补齐容错：
- 配置缺失时回落到 `defaultCommandRouteConfig`
- 非法值（如 `mode=foo`）自动纠正并打 WARN 日志

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/apiserver -run TestCommandRouteConfig_ -count=1`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/command_route_config.go internal/apiserver/command_route_config_test.go internal/apiserver/methods_config.go
git commit -m "feat(apiserver): add configurable command-route policy with pragmatic defaults"
```

### 任务 1: 建立命令意图分类与路由决策纯函数

**文件:**
- 创建: `internal/apiserver/command_route_policy.go`
- 创建: `internal/apiserver/command_route_policy_test.go`
- 测试: `internal/apiserver/command_route_policy_test.go`

**步骤 1: 写失败的测试**

```go
func TestClassifyCommandIntentFromArgv(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want routeDecision
	}{
		{"cat_go_file_hard", []string{"cat", "internal/apiserver/server.go"}, routeHardLSP},
		{"grep_readme_shell", []string{"grep", "TODO", "README.md"}, routeShell},
		{"sed_write_shell", []string{"sed", "-i", "s/a/b/", "main.go"}, routeShell},
		{"rg_identifier_hard", []string{"rg", "Server", "internal/apiserver"}, routeHardLSP},
		{"rg_regex_shell", []string{"rg", "^func\\s+", "internal/apiserver"}, routeShell},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := classifyCommandIntentFromArgv(tt.argv)
			if got := decideCommandRoute(intent); got != tt.want {
				t.Fatalf("route=%v want=%v intent=%+v", got, tt.want, intent)
			}
		})
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/apiserver -run TestClassifyCommandIntentFromArgv -count=1`
预期: FAIL，提示 `undefined: classifyCommandIntentFromArgv` / `undefined: decideCommandRoute`

**步骤 3: 写最小实现**

```go
type routeDecision string

const (
	routeShell    routeDecision = "shell"
	routeSoftHint routeDecision = "soft_hint"
	routeHardLSP  routeDecision = "hard_lsp"
)

type commandIntent struct {
	Base          string
	Args          []string
	ReadsCode     bool
	WritesCode    bool
	ComplexShell  bool
	ComplexSearch bool
}

func decideCommandRoute(in commandIntent) routeDecision {
	if in.WritesCode || in.ComplexShell {
		return routeShell
	}
	if in.ReadsCode && !in.ComplexSearch {
		switch in.Base {
		case "cat", "bat", "rg", "grep":
			return routeHardLSP
			case "head", "tail", "less", "more":
			return routeSoftHint
		}
		return routeSoftHint
	}
	return routeShell
}
```

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/apiserver -run TestClassifyCommandIntentFromArgv -count=1`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/command_route_policy.go internal/apiserver/command_route_policy_test.go
git commit -m "feat(apiserver): add command intent classifier and route policy"
```

---

### 任务 2: 实现每 turn 一次提示跟踪器

**文件:**
- 创建: `internal/apiserver/read_hint_tracker.go`
- 创建: `internal/apiserver/read_hint_tracker_test.go`
- 修改: `internal/apiserver/server.go:126-176`
- 测试: `internal/apiserver/read_hint_tracker_test.go`

**步骤 1: 写失败的测试**

```go
func TestReadHintTracker_OncePerTurn(t *testing.T) {
	s := &Server{readHintShownByTurn: map[string]struct{}{}, activeTurns: map[string]*trackedTurn{}}
	s.activeTurns["thread-1"] = &trackedTurn{ID: "turn-a", ThreadID: "thread-1"}

	if !s.shouldEmitReadHintOnce("thread-1") {
		t.Fatal("first emit should be true")
	}
	if s.shouldEmitReadHintOnce("thread-1") {
		t.Fatal("second emit in same turn should be false")
	}

	s.activeTurns["thread-1"] = &trackedTurn{ID: "turn-b", ThreadID: "thread-1"}
	if !s.shouldEmitReadHintOnce("thread-1") {
		t.Fatal("new turn should emit again")
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/apiserver -run TestReadHintTracker_OncePerTurn -count=1`
预期: FAIL，提示 `undefined: shouldEmitReadHintOnce`

**步骤 3: 写最小实现**

```go
func (s *Server) shouldEmitReadHintOnce(threadID string) bool {
	key := s.currentReadHintTurnKey(threadID)
	if key == "" {
		return false
	}
	s.readHintMu.Lock()
	defer s.readHintMu.Unlock()
	if _, ok := s.readHintShownByTurn[key]; ok {
		return false
	}
	s.readHintShownByTurn[key] = struct{}{}
	return true
}

func (s *Server) currentReadHintTurnKey(threadID string) string {
	turnID, _, _, ok := s.peekTrackedTurnMeta(threadID)
	if !ok || strings.TrimSpace(turnID) == "" {
		return strings.TrimSpace(threadID) + "#no-turn"
	}
	return strings.TrimSpace(threadID) + "#" + strings.TrimSpace(turnID)
}
```

并在 `Server` 结构体和 `New()` 初始化中补齐：

```go
readHintMu         sync.Mutex
readHintShownByTurn map[string]struct{}
```

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/apiserver -run TestReadHintTracker_ -count=1`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/server.go internal/apiserver/read_hint_tracker.go internal/apiserver/read_hint_tracker_test.go
git commit -m "feat(apiserver): track read-command hint once per turn"
```

---

### 任务 3: 在 Codex shell 事件路径接入软路由与去重提示

**文件:**
- 修改: `internal/apiserver/server_payload.go:753-860`
- 创建: `internal/apiserver/server_payload_read_route_test.go`
- 测试: `internal/apiserver/server_payload_read_route_test.go`

**步骤 1: 写失败的测试**

```go
func TestEnrichReadCommandPayload_SoftRouteOncePerTurn(t *testing.T) {
	s := &Server{
		readHintShownByTurn: map[string]struct{}{},
		activeTurns: map[string]*trackedTurn{
			"thread-1": {ID: "turn-1", ThreadID: "thread-1"},
		},
		toolCallCount: make(map[string]int64),
	}
	p := map[string]any{"threadId": "thread-1", "command": "cat internal/apiserver/server.go"}

	s.enrichReadCommandPayload(agentcore.EventExecCommandBegin, "item/started", p)
	if _, ok := p["lspHint"]; !ok {
		t.Fatal("first read command should carry lspHint")
	}

	p2 := map[string]any{"threadId": "thread-1", "command": "cat internal/apiserver/methods.go"}
	s.enrichReadCommandPayload(agentcore.EventExecCommandBegin, "item/started", p2)
	if _, ok := p2["lspHint"]; ok {
		t.Fatal("second read command in same turn must not repeat lspHint")
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/apiserver -run TestEnrichReadCommandPayload_SoftRouteOncePerTurn -count=1`
预期: FAIL，签名不匹配或字段缺失

**步骤 3: 写最小实现**

```go
func (s *Server) enrichReadCommandPayload(eventType, method string, payload map[string]any) {
	if payload == nil {
		return
	}
	if eventType != agentcore.EventExecCommandBegin || !strings.EqualFold(method, "item/started") {
		return
	}
	cmd := extractCommandBaseName(payload)
	intent := classifyCommandIntentFromPayload(payload)
	decision := decideCommandRoute(intent)
	payload["commandRoute"] = string(decision)
	if decision == routeShell {
		return
	}
	payload["isReadCommand"] = true
	if decision == routeHardLSP {
		payload["lspRoute"] = "hard"
	} else {
		payload["lspRoute"] = "soft"
	}
	threadID, _ := payload["threadId"].(string)
	if s.shouldEmitReadHintOnce(threadID) {
		payload["lspHint"] = lspPreferenceHint
	}
	s.IncrementToolCall("shell_read:" + cmd)
}
```

同时扩展 `extractCommandBaseName`，支持 `args/arguments/item/process` 嵌套来源。

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/apiserver -run 'TestEnrichReadCommandPayload_|TestExtractCommandBaseName' -count=1`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/server_payload.go internal/apiserver/server_payload_read_route_test.go
git commit -m "feat(apiserver): apply soft-route and once-per-turn hint for codex shell read commands"
```

---

### 任务 4: 在 command/exec 路径接入硬路由与软提示（无上下文污染）

**文件:**
- 修改: `internal/apiserver/methods_command.go:18-166`
- 创建: `internal/apiserver/command_exec_routing.go`
- 创建: `internal/apiserver/command_exec_routing_test.go`
- 测试: `internal/apiserver/command_exec_routing_test.go`

**步骤 1: 写失败的测试**

```go
func TestMaybeRouteCommandToLSP_CatCodeFile(t *testing.T) {
	intent := classifyCommandIntentFromArgv([]string{"cat", "internal/apiserver/server.go"})
	res := maybeRouteCommandToLSP(routeInput{
		Intent: intent,
		ThreadID: "thread-1",
		ForceShell: false,
		WorkspaceRoot: ".",
	})
	if !res.Routed || res.Mode != routeHardLSP {
		t.Fatalf("expected hard route, got %+v", res)
	}
}

func TestMaybeRouteCommandToLSP_FallbackWhenLSPUnavailable(t *testing.T) {
	intent := classifyCommandIntentFromArgv([]string{"cat", "internal/apiserver/server.go"})
	res := maybeRouteCommandToLSP(routeInput{
		Intent: intent,
		ThreadID: "thread-1",
		ForceShell: false,
		WorkspaceRoot: ".",
		LSPAvailable: false,
	})
	if res.Routed {
		t.Fatalf("expected fallback to shell, got %+v", res)
	}
	if res.Mode != routeShell {
		t.Fatalf("expected routeShell fallback, got %+v", res)
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/apiserver -run TestMaybeRouteCommandToLSP_CatCodeFile -count=1`
预期: FAIL，`undefined: maybeRouteCommandToLSP`

**步骤 3: 写最小实现**

```go
type routeInput struct {
	Intent       commandIntent
	ThreadID     string
	ForceShell   bool
	WorkspaceRoot string
	LSPAvailable bool
	LSP tools.LSPProvider
}

type routeResult struct {
	Routed bool
	Mode   routeDecision
	Stdout string
	Reason string
}

func maybeRouteCommandToLSP(in routeInput) routeResult {
	if in.ForceShell {
		return routeResult{Routed: false, Mode: routeShell, Reason: "force_shell"}
	}
	mode := decideCommandRoute(in.Intent)
	if mode != routeHardLSP {
		return routeResult{Routed: false, Mode: mode, Reason: "policy_non_hard"}
	}
	if !in.LSPAvailable || in.LSP == nil {
		return routeResult{Routed: false, Mode: routeShell, Reason: "lsp_unavailable"}
	}
	// Cat/Bat: 直接路由到 lsp_document_symbol (必要时扩展为 lsp_open_file + lsp_document_symbol)
	// Rg/Grep identifier: 直接路由到 lsp_workspace_symbol (必要时扩展为 lsp_references)
	out := routeReadCommandToLSP(in.Intent, in.LSP)
	return routeResult{
		Routed: true,
		Mode: routeHardLSP,
		Stdout: out,
		Reason: "hard_routed_to_lsp",
	}
}
```

并在 `commandExecTyped` 中替换旧逻辑：
- 删除每次都 `outStr = lspPreferenceHint + outStr` 的分支。
- 先做路由决策；硬路由直接返回 LSP 结果。
- 软路由仅在 `threadId` 存在且 `shouldEmitReadHintOnce(threadId)` 为 true 时提示一次。
- `threadId` 为空时，降级为“仅软路由提示，不做 once-per-turn 去重”，并记录 `route_reason=missing_thread_id`。

`commandExecParams` 增加可选字段：

```go
ThreadID string `json:"threadId,omitempty"`
```

同时更新调用方契约（客户端传参）：
- `docs/plans/2026-02-20-offline-52-rpc-methods.md` 中 `command/exec` 的 params 示例新增 `threadId`（可选）
- `internal/apiserver/methods_offline52_list.go` 若有参数 schema 映射，补充 `threadId` 字段说明

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/apiserver -run 'TestMaybeRouteCommandToLSP_|TestClassifyCommandIntentFromArgv' -count=1`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/methods_command.go internal/apiserver/command_exec_routing.go internal/apiserver/command_exec_routing_test.go
git commit -m "feat(apiserver): add hard/soft routing for command exec without repeated stdout hint"
```

---

### 任务 5: UI 适配（路由状态可见且不污染）

**文件:**
- 修改: `internal/uistate/runtime_types.go`
- 修改: `internal/uistate/runtime_event_handlers.go`
- 修改: `internal/uistate/runtime_timeline.go`
- 修改: `internal/uistate/event_normalizer.go`
- 创建: `internal/uistate/command_route_ui_test.go`
- 测试: `internal/uistate/command_route_ui_test.go`

**步骤 1: 写失败的测试**

```go
func TestResolveEventFields_CommandRouteMeta(t *testing.T) {
	payload := map[string]any{
		"command":      "cat internal/apiserver/server.go",
		"commandRoute": "hard_lsp",
		"routeReason":  "hard_cat_single_code_file",
		"routeFallback": false,
	}
	fields := resolveEventFields(NormalizedEvent{}, payload)
	if fields.commandRoute != "hard_lsp" {
		t.Fatalf("want hard_lsp, got %q", fields.commandRoute)
	}
	if fields.routeReason == "" {
		t.Fatal("routeReason should be present")
	}
	if fields.routeFallback {
		t.Fatal("routeFallback should be false")
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/uistate -run TestResolveEventFields_CommandRouteMeta -count=1`
预期: FAIL，提示 `resolvedFields` 不包含路由字段

**步骤 3: 写最小实现**

```go
type TimelineItem struct {
	Kind          string `json:"kind"`
	Command       string `json:"command,omitempty"`
	CommandRoute  string `json:"commandRoute,omitempty"`
	RouteReason   string `json:"routeReason,omitempty"`
	RouteFallback bool   `json:"routeFallback,omitempty"`
	HintShown     bool   `json:"hintShown,omitempty"`
}
```

并在事件处理中补齐：
- 从 payload 解析 `commandRoute`、`routeReason`、`routeFallback`
- 仅展示结构化字段，不把提示文本重复注入到 command output
- `hintShown` 仅用于 UI 标记，不额外写入用户消息/助手消息

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/uistate -run 'TestResolveEventFields_CommandRouteMeta|TestCommandTimeline_RouteBadge' -count=1`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/uistate/runtime_types.go internal/uistate/runtime_event_handlers.go internal/uistate/runtime_timeline.go internal/uistate/event_normalizer.go internal/uistate/command_route_ui_test.go
git commit -m "feat(uistate): surface command route metadata without extra hint noise"
```

---

### 任务 6: 严格验证路由行为与兜底正确性

**文件:**
- 创建: `internal/apiserver/command_route_integration_test.go`
- 创建: `internal/apiserver/command_route_observability_test.go`
- 修改: `internal/apiserver/server_payload_read_route_test.go`
- 修改: `internal/apiserver/command_exec_routing_test.go`
- 测试: `internal/apiserver/*command*test.go`

**步骤 1: 写失败的矩阵测试**

```go
func TestCommandRouteMatrix(t *testing.T) {
	cases := []struct {
		name   string
		argv   []string
		lspUp  bool
		want   routeDecision
		fbWant bool
	}{
		{"cat_code_hard", []string{"cat", "internal/apiserver/server.go"}, true, routeHardLSP, false},
		{"head_code_soft", []string{"head", "internal/apiserver/server.go"}, true, routeSoftHint, false},
		{"sed_write_shell", []string{"sed", "-i", "s/a/b/", "main.go"}, true, routeShell, false},
		{"cat_code_lsp_down", []string{"cat", "internal/apiserver/server.go"}, false, routeShell, true},
		{"cat_readme_shell", []string{"cat", "README.md"}, true, routeShell, false},
	}
	_ = cases
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/apiserver -run 'TestCommandRouteMatrix|TestRouteObservability_' -count=1`
预期: FAIL，缺少完整 `route_reason` / `fallback` 断言链路

**步骤 3: 写最小实现**

实现要求：
- 每条命令路径都落 `route_mode`、`route_reason`、`route_fallback`
- 当 `fallbackWhenLspUnavailable=true` 且 LSP 不可用时，必须回退 shell 且 `route_fallback=true`
- `threadId` 缺失时标记 `route_reason=missing_thread_id`，但不能中断命令执行
- `IncrementToolCall` 增加路由统计键：`route:hard_lsp`、`route:soft_hint`、`route:shell`、`route:fallback`

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/apiserver -run 'TestCommandRouteMatrix|TestMaybeRouteCommandToLSP_|TestEnrichReadCommandPayload_|TestRouteObservability_' -count=1`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/command_route_integration_test.go internal/apiserver/command_route_observability_test.go internal/apiserver/server_payload_read_route_test.go internal/apiserver/command_exec_routing_test.go
git commit -m "test(apiserver): enforce command-route matrix, fallback and observability"
```

---

### 任务 7: 文档与发布门禁更新

**文件:**
- 修改: `docs/plans/2026-02-24-command-file-intent-lsp-routing.md`
- 修改: `docs/plans/2026-02-20-offline-52-rpc-methods.md`
- 创建: `docs/command-route-policy.md`
- 测试: `internal/apiserver/*.go`

**步骤 1: 先写文档草案（失败态：信息不全）**

`docs/command-route-policy.md` 至少包含：
- 配置项与默认值（`code_only + mixed + oncePerTurnHint + fallbackWhenLspUnavailable`）
- 路由矩阵（硬路由/软路由/不路由）
- 兜底语义（LSP down、missing threadId、复杂 shell）
- UI 展示字段说明（`commandRoute`、`routeReason`、`routeFallback`、`hintShown`）
- 回滚开关说明（`enabled=false`、`mode=soft_only`）

**步骤 2: 运行文档关联检查**

运行: `rg -n "commandRoute|routeReason|routeFallback|threadId" docs internal/apiserver internal/uistate`
预期: 文档字段与代码字段一致，无拼写漂移

**步骤 3: 补齐实现缺口并同步文档**

```go
// 文档-代码一致性约束（示意）
const (
	RouteModeHard = "hard_lsp"
	RouteModeSoft = "soft_hint"
	RouteModeShell = "shell"
)
```

确保命名与文档保持一致，避免 UI/API/日志三套命名。

**步骤 4: 全量验证**

运行: `go test ./internal/apiserver ./internal/uistate ./internal/lsp -count=1`
预期: PASS

运行: `go test ./... -count=1`
预期: PASS（若存在历史失败，记录并标注非本次改动）

**步骤 5: 提交**

```bash
git add docs/command-route-policy.md docs/plans/2026-02-24-command-file-intent-lsp-routing.md docs/plans/2026-02-20-offline-52-rpc-methods.md internal/apiserver internal/uistate
git commit -m "docs(apiserver): add command-route policy, verification gates and ui contract"
```

---

## 验收清单

- 同一 `threadId + turnId` 下，`lspHint` 最多出现一次。
- 仅“编程命令 + 代码文件/目录”可进入路由决策；非代码语义保持 shell。
- 硬路由、软路由、回退 shell 三种路径都能输出结构化元信息。
- `command/exec` 不再默认把提示文本前置注入 stdout。
- UI 可以展示路由状态（`commandRoute`/`routeReason`/`routeFallback`），且不会重复污染命令输出。
- 后端配置可关闭或降级策略（`enabled`、`mode`、`scope`）并即时生效。

## 严格验证门禁（发布前必须全部通过）

1. 单元门禁: `go test ./internal/apiserver -run 'TestCommandRouteConfig_|TestClassifyCommandIntentFromArgv|TestMaybeRouteCommandToLSP_|TestEnrichReadCommandPayload_|TestCommandRouteMatrix|TestRouteObservability_' -count=1`
2. UI 门禁: `go test ./internal/uistate -run 'TestResolveEventFields_CommandRouteMeta|TestCommandTimeline_RouteBadge' -count=1`
3. 组件门禁: `go test ./internal/apiserver ./internal/uistate ./internal/lsp -count=1`
4. 仓库门禁: `go test ./... -count=1`
5. 人工门禁: 手工触发 6 类命令（hard/soft/write/non-code/complex/lsp-down），逐条确认 UI 与日志里的 `route_mode`、`route_reason`、`route_fallback` 与预期一致

## 风险与缓解

- 风险: `grep/rg` 语义过于复杂导致误路由。
- 缓解: 仅允许 identifier + 代码目标的硬路由，其余回落 soft/shell。

- 风险: turn 边界异常导致提示不重置。
- 缓解: 基于 `peekTrackedTurnMeta` 去重，并为缺失 turnId 提供降级逻辑。

- 风险: 关闭 shell 后覆盖不全导致任务失败。
- 缓解: 保持 `fallbackWhenLspUnavailable=true`，并提供 `enabled=false` 与 `mode=soft_only` 快速回滚。

## 执行交接

计划完成并保存到 `docs/plans/2026-02-24-command-file-intent-lsp-routing.md`。两种执行选项：

**1. 子代理驱动（本会话）** - 每任务派遣新子代理，任务间审查，快速迭代

**2. 并行会话（单独）** - 新会话用 @执行计划，分批执行带检查点

选哪个？
