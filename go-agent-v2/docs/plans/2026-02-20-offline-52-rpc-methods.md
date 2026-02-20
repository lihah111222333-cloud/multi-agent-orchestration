# 52个未触发 RPC 方法下线 实现计划

> **给 Claude:** 必须使用 @执行计划 逐任务实现此计划。

**目标:** 将“今晚日志未调用 + 覆盖率 0%”的 52 个 JSON-RPC 入口先执行“注册层下线”，确保主流程不受影响，并保留快速回滚能力。

**架构:** 采用“两阶段”策略：Phase A 只从 `registerMethods` 下线入口（函数保留，快速回滚）；Phase B 在连续两轮业务采样仍未触发后再物理删除代码。通过统一清单文件驱动注册与测试，避免散落硬编码。

**技术栈:** Go 1.25、Wails v3、JSON-RPC、`go test`、覆盖率脚本 `scripts/ui-coverage.sh`。

---

## 输入证据（本计划基线）

- 运行日志：`/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2-clean/go-agent-v2/.tmp/ui-cover-run.log`
- 当晚合并日志：`/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2-clean/go-agent-v2/.tmp/tonight-merged.log`
- 覆盖率清单：`/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2-clean/go-agent-v2/.tmp/ui-triggered.txt`、`/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2-clean/go-agent-v2/.tmp/ui-untriggered.txt`
- 高置信候选（52）：`/Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2-clean/go-agent-v2/.tmp/handler-candidates-tonight.txt`

---

## 约束与验收

- DRY/YAGNI：只做“方法注册下线”，不做额外架构重写。
- TDD：每批先写失败测试，再改实现，再回归。
- 高频提交：每批 8-15 个方法一个提交。
- 验收标准：
  1) 52 个目标方法默认返回 `method not found`；
  2) 今晚主路径方法仍可调用；
  3) `go test ./internal/apiserver/...` 通过；
  4) 新一轮业务采样无新错误回归。

---

### 任务 1: 建立 52 方法统一清单（单一真相源）

**文件:**
- 创建: `internal/apiserver/methods_offline52_list.go`
- 测试: `internal/apiserver/methods_offline52_list_test.go`

**步骤 1: 写失败的测试（先锁定清单规模和唯一性）**

```go
// 文件: internal/apiserver/methods_offline52_list_test.go
package apiserver

import "testing"

func TestOffline52MethodList_CountAndUnique(t *testing.T) {
	list := offline52MethodList()
	if len(list) != 52 {
		t.Fatalf("offline52 len=%d, want 52", len(list))
	}
	seen := map[string]struct{}{}
	for _, method := range list {
		if _, ok := seen[method]; ok {
			t.Fatalf("duplicate method: %s", method)
		}
		seen[method] = struct{}{}
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestOffline52MethodList_CountAndUnique -v`

预期: FAIL（`offline52MethodList` 未定义）。

**步骤 3: 写最小实现（创建 52 方法清单）**

```go
// 文件: internal/apiserver/methods_offline52_list.go
package apiserver

func offline52MethodList() []string {
	return []string{
		"initialize",
		"thread/resume",
		"thread/fork",
		// ...其余 49 个（按附录 A 完整填充）
		"debug/runtime",
		"debug/gc",
	}
}
```

**步骤 4: 运行测试确认通过**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestOffline52MethodList_CountAndUnique -v`

预期: PASS。

**步骤 5: 提交**

```bash
git add internal/apiserver/methods_offline52_list.go internal/apiserver/methods_offline52_list_test.go
git commit -m "test(apiserver): add offline52 method manifest and uniqueness test"
```

---

### 任务 2: 下线开关（默认下线，可快速回滚）

**文件:**
- 修改: `internal/config/config.go`
- 修改: `internal/apiserver/methods.go`
- 测试: `internal/apiserver/methods_offline52_switch_test.go`

**步骤 1: 写失败的测试（默认下线）**

```go
// 文件: internal/apiserver/methods_offline52_switch_test.go
package apiserver

import (
	"context"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/config"
)

func TestOffline52_DefaultDisabled_ReturnsMethodNotFound(t *testing.T) {
	srv := New(Deps{Config: &config.Config{DisableOffline52Methods: true}})
	resp := srv.dispatchRequest(context.Background(), 1, "thread/resume", nil)
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("want method not found, got %+v", resp)
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestOffline52_DefaultDisabled_ReturnsMethodNotFound -v`

预期: FAIL（配置字段/下线逻辑尚未实现）。

**步骤 3: 写最小实现（配置 + 注册后删除）**

```go
// 文件: internal/config/config.go
DisableOffline52Methods bool `env:"DISABLE_OFFLINE_52_METHODS" default:"true"`

// 文件: internal/apiserver/methods.go
func (s *Server) registerMethods() {
	// 原有注册逻辑...
	if s.cfg == nil || s.cfg.DisableOffline52Methods {
		for _, method := range offline52MethodList() {
			delete(s.methods, method)
		}
	}
}
```

**步骤 4: 运行测试确认通过**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestOffline52_DefaultDisabled_ReturnsMethodNotFound -v`

预期: PASS。

**步骤 5: 提交**

```bash
git add internal/config/config.go internal/apiserver/methods.go internal/apiserver/methods_offline52_switch_test.go
git commit -m "feat(apiserver): add offline52 registration gate with default disable"
```

---

### 任务 3: 回滚能力测试（开关关闭时恢复旧入口）

**文件:**
- 修改: `internal/apiserver/methods_offline52_switch_test.go`

**步骤 1: 写失败的测试（关闭下线开关时可注册）**

```go
func TestOffline52_RollbackSwitch_ReEnableMethods(t *testing.T) {
	srv := New(Deps{Config: &config.Config{DisableOffline52Methods: false}})
	if _, ok := srv.methods["thread/resume"]; !ok {
		t.Fatal("thread/resume should be registered when switch is off")
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestOffline52_RollbackSwitch_ReEnableMethods -v`

预期: 若开关语义实现不一致则 FAIL。

**步骤 3: 写最小实现（修正开关语义）**

```go
// 确认语义: DisableOffline52Methods=true => 下线；false => 保留
```

**步骤 4: 运行测试确认通过**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestOffline52_ -v`

预期: PASS。

**步骤 5: 提交**

```bash
git add internal/apiserver/methods_offline52_switch_test.go internal/apiserver/methods.go
git commit -m "test(apiserver): verify offline52 rollback switch semantics"
```

---

### 任务 4: 主流程守护测试（今晚调用的 21 方法必须保留）

**文件:**
- 创建: `internal/apiserver/methods_mainpath_guard_test.go`

**步骤 1: 写失败的测试（主流程方法存在性）**

```go
// 文件: internal/apiserver/methods_mainpath_guard_test.go
package apiserver

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/config"
)

func TestMainPathMethods_StillRegisteredWhenOffline52Enabled(t *testing.T) {
	srv := New(Deps{Config: &config.Config{DisableOffline52Methods: true}})
	mainPath := []string{
		"thread/start", "thread/list", "thread/messages", "thread/name/set",
		"turn/start", "turn/interrupt",
		"ui/state/get", "ui/dashboard/get", "ui/preferences/get", "ui/preferences/set",
		"ui/projects/get", "ui/projects/setActive", "ui/copyText", "ui/selectProjectDirs",
		"skills/local/read", "skills/local/importDir", "skills/config/read", "skills/config/write", "skills/match/preview",
		"config/lspPromptHint/read", "config/lspPromptHint/write",
	}
	for _, method := range mainPath {
		if _, ok := srv.methods[method]; !ok {
			t.Fatalf("main path method missing: %s", method)
		}
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestMainPathMethods_StillRegisteredWhenOffline52Enabled -v`

预期: 若误下线主路径方法则 FAIL。

**步骤 3: 写最小实现（修正清单或注册删除逻辑）**

```go
// 仅调整 offline52MethodList，不修改主路径逻辑
```

**步骤 4: 运行测试确认通过**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestMainPathMethods_StillRegisteredWhenOffline52Enabled -v`

预期: PASS。

**步骤 5: 提交**

```bash
git add internal/apiserver/methods_mainpath_guard_test.go internal/apiserver/methods_offline52_list.go
git commit -m "test(apiserver): guard tonight main-path methods during offline52 rollout"
```

---

### 任务 5: 52 方法统一断言（批量 method-not-found）

**文件:**
- 创建: `internal/apiserver/methods_offline52_invoke_test.go`

**步骤 1: 写失败的测试（批量调用）**

```go
// 文件: internal/apiserver/methods_offline52_invoke_test.go
package apiserver

import (
	"context"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/config"
)

func TestOffline52_Invoke_ReturnMethodNotFound(t *testing.T) {
	srv := New(Deps{Config: &config.Config{DisableOffline52Methods: true}})
	for _, method := range offline52MethodList() {
		resp := srv.dispatchRequest(context.Background(), 1, method, nil)
		if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
			t.Fatalf("method=%s want CodeMethodNotFound got %+v", method, resp)
		}
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestOffline52_Invoke_ReturnMethodNotFound -v`

预期: FAIL（若清单或删除逻辑不完整）。

**步骤 3: 写最小实现（补齐漏项）**

```go
// 在 offline52MethodList 补齐漏下线方法
```

**步骤 4: 运行测试确认通过**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver -run TestOffline52_Invoke_ReturnMethodNotFound -v`

预期: PASS。

**步骤 5: 提交**

```bash
git add internal/apiserver/methods_offline52_invoke_test.go internal/apiserver/methods_offline52_list.go
git commit -m "test(apiserver): assert all offline52 methods return method-not-found"
```

---

### 任务 6: 全量回归 + 业务采样复验

**文件:**
- 修改: `README.md`（补下线开关说明）
- 修改: `.env.example`（增加 `DISABLE_OFFLINE_52_METHODS`）

**步骤 1: 写文档/配置更新（先写后测）**

```bash
# README 增加：
# - DISABLE_OFFLINE_52_METHODS=1 默认下线
# - 回滚：DISABLE_OFFLINE_52_METHODS=0
```

**步骤 2: 运行测试确认通过**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver/... -count=1`

预期: PASS。

**步骤 3: 运行一次采样脚本（人工业务流）**

运行:

```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2-clean/go-agent-v2
TARGET=agent-terminal ./scripts/ui-coverage.sh build
TARGET=agent-terminal ./scripts/ui-coverage.sh run --debug
# 手工跑业务后退出
TARGET=agent-terminal ./scripts/ui-coverage.sh report
```

预期:
- 主路径 21 方法仍在日志出现；
- 52 方法仍未出现；
- 无新增 P0 错误。

**步骤 4: 提交**

```bash
git add README.md .env.example
git commit -m "docs: add offline52 switch and rollback instructions"
```

---

### 任务 7: Phase B（延迟执行）— 物理删除 52 处理函数

> 触发条件：连续两轮采样（不同业务时段）仍满足“入口未调用 + 函数 0% + 主流程测试全绿”。

**文件:**
- 修改: `internal/apiserver/methods.go`
- 修改: `internal/apiserver/workspace_methods.go`
- 修改: `internal/apiserver/methods_ui_projects.go`
- 测试: `internal/apiserver/*_test.go`

**步骤 1: 写失败的编译/测试期望（保留接口不再引用被删函数）**

```go
// 删除函数前，先确保 registerMethods 不再引用这些 handler
```

**步骤 2: 分批删除（每批 <= 15 个函数）**

```go
// 仅删除已下线且未被其他路径调用的函数
```

**步骤 3: 运行回归**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test ./internal/apiserver/... -count=1`

预期: PASS。

**步骤 4: 提交（每批一次）**

```bash
git commit -m "refactor(apiserver): delete offline52 dead handlers batch-1"
# batch-2 / batch-3 ...
```

---

## 附录 A：52 个下线目标方法（今晚高置信）

1. initialize
2. thread/resume
3. thread/fork
4. thread/compact/start
5. thread/rollback
6. thread/loaded/list
7. thread/read
8. thread/resolve
9. thread/backgroundTerminals/clean
10. turn/steer
11. turn/forceComplete
12. review/start
13. fuzzyFileSearch
14. skills/list
15. skills/remote/read
16. skills/remote/write
17. app/list
18. model/list
19. collaborationMode/list
20. experimentalFeature/list
21. config/read
22. config/value/write
23. config/batchWrite
24. configRequirements/read
25. account/login/start
26. account/login/cancel
27. account/logout
28. account/read
29. account/rateLimits/read
30. config/mcpServer/reload
31. mcpServerStatus/list
32. lsp_diagnostics_query
33. command/exec
34. thread/undo
35. thread/model/set
36. thread/personality/set
37. thread/approvals/set
38. thread/mcp/list
39. thread/skills/list
40. thread/debugMemory
41. log/list
42. log/filters
43. workspace/run/create
44. workspace/run/get
45. workspace/run/list
46. workspace/run/merge
47. workspace/run/abort
48. ui/preferences/getAll
49. ui/projects/add
50. ui/projects/remove
51. debug/runtime
52. debug/gc

---

## 附录 B：执行时技能约束

- 逐任务执行必须使用 `@执行计划`。
- 遇到测试不稳定或结果异常，使用 `@系统性调试`。
- 如需拆分并发任务，使用 `@子代理开发`。

