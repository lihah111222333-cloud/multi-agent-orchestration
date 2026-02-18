# UI 状态下沉二期（Projects / Dashboard / Runtime Sync）实现计划

> **给 Claude:** 必须使用 @执行计划 逐任务实现此计划。

**目标:** 将前端剩余业务状态（项目列表、Dashboard 聚合、运行时同步节流）进一步下沉到 Go，JS 仅保留纯 UI 暂态。

**架构:** 在 `internal/apiserver` 增加 `ui/projects/*` 与 `ui/dashboard/get`，统一由 Go 负责状态归一化与持久化。前端 `projects.js`/`app.js`/`threads.js` 去掉业务状态机与事件节流，只消费后端快照与后端变更信号。整个迁移按 TDD 小步推进，逐任务可回滚。

**技术栈:** Go 1.22+, JSON-RPC (apiserver), PostgreSQL `ui_preferences`, Vue 3 ESM, Node `--test`

---

**执行技能链:** `@执行计划` + `@后端` + `@TDD` + `@完成前验证`

## 任务 1: 新增 Go 项目状态 API（`ui/projects/*`）

**文件:**
- 创建: `internal/apiserver/methods_ui_projects.go`
- 创建: `internal/apiserver/methods_ui_projects_test.go`
- 修改: `internal/apiserver/methods.go`

**步骤 1: 写失败的测试**

```go
// internal/apiserver/methods_ui_projects_test.go
package apiserver

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestUIProjects_CRUD(t *testing.T) {
	srv := &Server{prefManager: uistate.NewPreferenceManager(nil)}
	ctx := context.Background()

	_, err := srv.uiProjectsAdd(ctx, uiProjectsAddParams{Path: "/tmp/demo/"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = srv.uiProjectsAdd(ctx, uiProjectsAddParams{Path: "/tmp/demo"})
	if err != nil {
		t.Fatalf("add dedup: %v", err)
	}

	raw, err := srv.uiProjectsGet(ctx, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp := raw.(map[string]any)
	if got := resp["active"]; got != "/tmp/demo" {
		t.Fatalf("active=%v", got)
	}
	if !reflect.DeepEqual(resp["projects"], []string{"/tmp/demo"}) {
		t.Fatalf("projects=%#v", resp["projects"])
	}

	_, err = srv.uiProjectsRemove(ctx, uiProjectsRemoveParams{Path: "/tmp/demo"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	raw, _ = srv.uiProjectsGet(ctx, json.RawMessage(`{}`))
	resp = raw.(map[string]any)
	if got := resp["active"]; got != "." {
		t.Fatalf("active after remove=%v", got)
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/apiserver -run TestUIProjects_CRUD -v`
预期: FAIL（`uiProjectsAdd` / `uiProjectsGet` / `uiProjectsRemove` 未定义）

**步骤 3: 写最小实现**

```go
// internal/apiserver/methods_ui_projects.go
package apiserver

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

const (
	prefProjectsList   = "projects.list"
	prefProjectsActive = "projects.active"
)

var windowsDriveOnly = regexp.MustCompile(`^[a-zA-Z]:[\\/]?$`)

type uiProjectsAddParams struct { Path string `json:"path"` }
type uiProjectsRemoveParams struct { Path string `json:"path"` }
type uiProjectsSetActiveParams struct { Path string `json:"path"` }

func normalizeProjectPath(path string) string {
	v := strings.TrimSpace(path)
	if v == "" {
		return ""
	}
	if v != "/" && !windowsDriveOnly.MatchString(v) {
		v = strings.TrimRight(v, "\\/")
	}
	return v
}

func (s *Server) readProjectsState(ctx context.Context) ([]string, string, error) {
	if s.prefManager == nil {
		return []string{}, ".", nil
	}
	prefs, err := s.prefManager.GetAll(ctx)
	if err != nil {
		return nil, "", err
	}
	projects := []string{}
	if arr, ok := prefs[prefProjectsList].([]any); ok {
		for _, item := range arr {
			p := normalizeProjectPath(asString(item))
			if p == "" || p == "." || slices.Contains(projects, p) {
				continue
			}
			projects = append(projects, p)
		}
	}
	active := normalizeProjectPath(asString(prefs[prefProjectsActive]))
	if active == "" {
		active = "."
	}
	if active != "." && !slices.Contains(projects, active) {
		active = "."
	}
	return projects, active, nil
}

func (s *Server) writeProjectsState(ctx context.Context, projects []string, active string) error {
	if s.prefManager == nil {
		return nil
	}
	if err := s.prefManager.Set(ctx, prefProjectsList, projects); err != nil { return err }
	if err := s.prefManager.Set(ctx, prefProjectsActive, active); err != nil { return err }
	return nil
}

func (s *Server) uiProjectsGet(ctx context.Context, _ json.RawMessage) (any, error) {
	projects, active, err := s.readProjectsState(ctx)
	if err != nil { return nil, err }
	return map[string]any{"projects": projects, "active": active}, nil
}

func (s *Server) uiProjectsAdd(ctx context.Context, p uiProjectsAddParams) (any, error) {
	projects, _, err := s.readProjectsState(ctx)
	if err != nil { return nil, err }
	next := normalizeProjectPath(p.Path)
	if next == "" || next == "." {
		return map[string]any{"projects": projects, "active": "."}, nil
	}
	if !slices.Contains(projects, next) {
		projects = append(projects, next)
	}
	if err := s.writeProjectsState(ctx, projects, next); err != nil { return nil, err }
	return map[string]any{"projects": projects, "active": next}, nil
}

func (s *Server) uiProjectsRemove(ctx context.Context, p uiProjectsRemoveParams) (any, error) {
	projects, active, err := s.readProjectsState(ctx)
	if err != nil { return nil, err }
	target := normalizeProjectPath(p.Path)
	filtered := make([]string, 0, len(projects))
	for _, item := range projects {
		if item != target {
			filtered = append(filtered, item)
		}
	}
	if active == target { active = "." }
	if err := s.writeProjectsState(ctx, filtered, active); err != nil { return nil, err }
	return map[string]any{"projects": filtered, "active": active}, nil
}

func (s *Server) uiProjectsSetActive(ctx context.Context, p uiProjectsSetActiveParams) (any, error) {
	projects, _, err := s.readProjectsState(ctx)
	if err != nil { return nil, err }
	next := normalizeProjectPath(p.Path)
	if next == "" || (next != "." && !slices.Contains(projects, next)) {
		next = "."
	}
	if err := s.writeProjectsState(ctx, projects, next); err != nil { return nil, err }
	return map[string]any{"projects": projects, "active": next}, nil
}
```

并在 `internal/apiserver/methods.go` 注册:

```go
s.methods["ui/projects/get"] = s.uiProjectsGet
s.methods["ui/projects/add"] = typedHandler(s.uiProjectsAdd)
s.methods["ui/projects/remove"] = typedHandler(s.uiProjectsRemove)
s.methods["ui/projects/setActive"] = typedHandler(s.uiProjectsSetActive)
```

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/apiserver -run TestUIProjects_CRUD -v`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/methods.go internal/apiserver/methods_ui_projects.go internal/apiserver/methods_ui_projects_test.go
git commit -m "feat(ui): add backend-owned projects state rpc"
```

---

## 任务 2: 前端项目状态改为后端权威源

**文件:**
- 修改: `cmd/agent-terminal/frontend/vue-app/stores/projects.js`
- 创建: `cmd/agent-terminal/frontend/vue-app/stores/__tests__/projects.backend-owned.test.mjs`

**步骤 1: 写失败的测试**

```js
// stores/__tests__/projects.backend-owned.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { useProjectStore } from '../projects.js';

let calls = [];
globalThis.__mockCallAPI = async (method, params) => {
  calls.push({ method, params });
  if (method === 'ui/projects/get') return { projects: ['/repo/a'], active: '/repo/a' };
  if (method === 'ui/projects/add') return { projects: ['/repo/a', '/repo/b'], active: '/repo/b' };
  return { projects: ['/repo/a'], active: '/repo/a' };
};

test('project store reads from backend-owned ui/projects/get', async () => {
  const store = useProjectStore();
  await store.reloadProjects();
  assert.equal(store.state.active, '/repo/a');
  assert.deepEqual(store.state.projects, ['/repo/a']);
});

test('addProject delegates to ui/projects/add', async () => {
  calls = [];
  const store = useProjectStore();
  await store.addProject('/repo/b');
  assert.equal(calls[0].method, 'ui/projects/add');
  assert.equal(store.state.active, '/repo/b');
});
```

**步骤 2: 运行测试确认失败**

运行: `node --test cmd/agent-terminal/frontend/vue-app/stores/__tests__/projects.backend-owned.test.mjs`
预期: FAIL（`reloadProjects` 不存在；`addProject` 未委托 Go）

**步骤 3: 写最小实现**

```js
// stores/projects.js（核心改造）
async function reloadProjects() {
  const res = await callAPI('ui/projects/get', {});
  state.projects = Array.isArray(res?.projects) ? res.projects : [];
  state.active = (res?.active || '.').toString() || '.';
}

async function addProject(path) {
  const normalized = normalizePath(path);
  if (!normalized || normalized === '.') return false;
  const res = await callAPI('ui/projects/add', { path: normalized });
  state.projects = Array.isArray(res?.projects) ? res.projects : [];
  state.active = (res?.active || '.').toString() || '.';
  return true;
}

async function removeProject(path) {
  const res = await callAPI('ui/projects/remove', { path: normalizePath(path) });
  state.projects = Array.isArray(res?.projects) ? res.projects : [];
  state.active = (res?.active || '.').toString() || '.';
}

async function setActive(path) {
  const res = await callAPI('ui/projects/setActive', { path: normalizePath(path) || '.' });
  state.projects = Array.isArray(res?.projects) ? res.projects : [];
  state.active = (res?.active || '.').toString() || '.';
}
```

> 保留前端本地状态仅限 `showModal` / `modalPath` / `browsing`。

**步骤 4: 运行测试确认通过**

运行:
- `node --test cmd/agent-terminal/frontend/vue-app/stores/__tests__/projects.backend-owned.test.mjs`
- `find cmd/agent-terminal/frontend/vue-app -name '*.test.mjs' -print0 | xargs -0 node --test`

预期: PASS

**步骤 5: 提交**

```bash
git add cmd/agent-terminal/frontend/vue-app/stores/projects.js cmd/agent-terminal/frontend/vue-app/stores/__tests__/projects.backend-owned.test.mjs
git commit -m "refactor(ui): move projects business state to backend"
```

---

## 任务 3: 新增 Dashboard 聚合 API（`ui/dashboard/get`）

**文件:**
- 创建: `internal/apiserver/methods_ui_dashboard.go`
- 创建: `internal/apiserver/methods_ui_dashboard_test.go`
- 修改: `internal/apiserver/methods.go`

**步骤 1: 写失败的测试**

```go
// internal/apiserver/methods_ui_dashboard_test.go
package apiserver

import (
	"context"
	"testing"
)

func TestUIDashboardGet_ReturnsStableShape(t *testing.T) {
	srv := &Server{}
	ctx := context.Background()

	raw, err := srv.uiDashboardGet(ctx, uiDashboardGetParams{Page: "tasks", TasksSubTab: "acks"})
	if err != nil {
		t.Fatalf("uiDashboardGet: %v", err)
	}
	resp := raw.(map[string]any)
	if _, ok := resp["taskAcks"]; !ok {
		t.Fatalf("missing taskAcks")
	}
	if _, ok := resp["taskTraces"]; !ok {
		t.Fatalf("missing taskTraces")
	}
}
```

**步骤 2: 运行测试确认失败**

运行: `go test ./internal/apiserver -run TestUIDashboardGet_ReturnsStableShape -v`
预期: FAIL（`uiDashboardGet` 未定义）

**步骤 3: 写最小实现**

```go
// internal/apiserver/methods_ui_dashboard.go
package apiserver

import "context"

type uiDashboardGetParams struct {
	Page       string `json:"page"`
	TasksSubTab string `json:"tasksSubTab"`
}

func (s *Server) uiDashboardGet(ctx context.Context, p uiDashboardGetParams) (any, error) {
	result := map[string]any{}
	switch p.Page {
	case "agents":
		out, _ := s.dashAgentStatus(ctx, nil)
		mergeMap(result, out)
	case "dags":
		out, _ := s.dashDAGs(ctx, nil)
		mergeMap(result, out)
	case "tasks":
		acks, _ := s.dashTaskAcks(ctx, nil)
		traces, _ := s.dashTaskTraces(ctx, nil)
		mergeMap(result, acks)
		mergeMap(result, traces)
	case "skills":
		out, _ := s.dashSkills(ctx, nil)
		mergeMap(result, out)
	case "commands":
		cards, _ := s.dashCommandCards(ctx, nil)
		prompts, _ := s.dashPrompts(ctx, nil)
		mergeMap(result, cards)
		mergeMap(result, prompts)
	case "memory":
		out, _ := s.dashSharedFiles(ctx, nil)
		mergeMap(result, out)
	default:
		result["agents"] = []any{}
	}
	return result, nil
}

func mergeMap(dst map[string]any, src any) {
	m, ok := src.(map[string]any)
	if !ok { return }
	for k, v := range m { dst[k] = v }
}
```

并在 `internal/apiserver/methods.go` 注册:

```go
s.methods["ui/dashboard/get"] = typedHandler(s.uiDashboardGet)
```

**步骤 4: 运行测试确认通过**

运行: `go test ./internal/apiserver -run TestUIDashboardGet_ReturnsStableShape -v`
预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/methods.go internal/apiserver/methods_ui_dashboard.go internal/apiserver/methods_ui_dashboard_test.go
git commit -m "feat(ui): add backend dashboard aggregate rpc"
```

---

## 任务 4: 前端 Dashboard 逻辑改为后端聚合消费

**文件:**
- 修改: `cmd/agent-terminal/frontend/vue-app/app.js`
- 创建: `cmd/agent-terminal/frontend/vue-app/pages/__tests__/app.dashboard-fetch.test.mjs`

**步骤 1: 写失败的测试**

```js
// pages/__tests__/app.dashboard-fetch.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { AppRoot } from '../../app.js';

test('dashboard page uses ui/dashboard/get instead of dashboard/* fan-out', () => {
  const src = AppRoot.setup.toString();
  assert.equal(src.includes('ui/dashboard/get'), true);
  assert.equal(src.includes('dashboard/agentStatus'), false);
  assert.equal(src.includes('dashboard/dags'), false);
});
```

**步骤 2: 运行测试确认失败**

运行: `node --test cmd/agent-terminal/frontend/vue-app/pages/__tests__/app.dashboard-fetch.test.mjs`
预期: FAIL（仍存在 `dashboard/*` fan-out）

**步骤 3: 写最小实现**

```js
// app.js 中 refreshDashboardByPage 改造
async function refreshDashboardByPage(targetPage) {
  if (targetPage === 'chat' || targetPage === 'settings') return;
  const res = await callAPI('ui/dashboard/get', {
    page: targetPage,
    tasksSubTab: tasksSubTab.value,
  });
  dashboard.agents = res?.agents || [];
  dashboard.dags = res?.dags || [];
  dashboard.taskAcks = res?.acks || [];
  dashboard.taskTraces = res?.traces || [];
  dashboard.skills = res?.skills || [];
  dashboard.commandCards = res?.cards || [];
  dashboard.prompts = res?.prompts || [];
  dashboard.memory = res?.files || [];
}
```

**步骤 4: 运行测试确认通过**

运行:
- `node --test cmd/agent-terminal/frontend/vue-app/pages/__tests__/app.dashboard-fetch.test.mjs`
- `find cmd/agent-terminal/frontend/vue-app -name '*.test.mjs' -print0 | xargs -0 node --test`

预期: PASS

**步骤 5: 提交**

```bash
git add cmd/agent-terminal/frontend/vue-app/app.js cmd/agent-terminal/frontend/vue-app/pages/__tests__/app.dashboard-fetch.test.mjs
git commit -m "refactor(ui): consume backend dashboard aggregate"
```

---

## 任务 5: 运行时同步信号下沉到 Go，删除前端节流状态机

**文件:**
- 修改: `internal/apiserver/server.go`
- 修改: `cmd/agent-terminal/app.go`
- 修改: `cmd/agent-terminal/frontend/vue-app/stores/threads.js`
- 删除: `cmd/agent-terminal/frontend/vue-app/stores/thread-runtime-sync-policy.js`
- 删除: `cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-runtime-sync-policy.test.mjs`
- 创建: `cmd/agent-terminal/frontend/vue-app/stores/__tests__/threads.runtime-sync-event.test.mjs`

**步骤 1: 写失败的测试**

```js
// stores/__tests__/threads.runtime-sync-event.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { useThreadStore } from '../threads.js';

test('handleBridgeEvent only syncs on ui/state/changed', async () => {
  const store = useThreadStore();
  let called = 0;
  store.__test_syncRuntimeState = async () => { called += 1; };

  store.handleBridgeEvent({ type: 'thread/updated' });
  store.handleBridgeEvent({ type: 'ui/state/changed' });

  assert.equal(called, 1);
});
```

**步骤 2: 运行测试确认失败**

运行: `node --test cmd/agent-terminal/frontend/vue-app/stores/__tests__/threads.runtime-sync-event.test.mjs`
预期: FAIL（当前 `handleBridgeEvent` 对多种事件触发同步）

**步骤 3: 写最小实现**

```go
// internal/apiserver/server.go (Notify 内)
func shouldEmitUIStateChanged(method string) bool {
	switch method {
	case "agent_message_delta", "agent_message_completed", "turn_complete", "turn_started",
		"workspace/run/created", "workspace/run/aborted", "workspace/run/merged":
		return true
	default:
		return false
	}
}
```

```go
// Notify 尾部追加二次通知（避免递归，走独立 broadcast helper）
if shouldEmitUIStateChanged(method) {
	s.broadcastNotification("ui/state/changed", map[string]any{"source": method})
}
```

```js
// stores/threads.js: 删除 scheduleRuntimeSync 与策略依赖
function handleBridgeEvent(evt) {
  const eventType = (evt?.type || evt?.method || '').toString();
  if (eventType !== 'ui/state/changed') return;
  syncRuntimeState().catch((error) => {
    logWarn('thread', 'state.sync.failed', { error, by_event: eventType });
  });
}

function handleAgentEvent() {
  // no-op: runtime sync is driven by backend ui/state/changed
}
```

并在 `cmd/agent-terminal/app.go` 保持 `bridge-event` 转发，不再依赖 `agent-event` 触发同步。

**步骤 4: 运行测试确认通过**

运行:
- `node --test cmd/agent-terminal/frontend/vue-app/stores/__tests__/threads.runtime-sync-event.test.mjs`
- `find cmd/agent-terminal/frontend/vue-app -name '*.test.mjs' -print0 | xargs -0 node --test`
- `go test ./internal/apiserver -run TestUIState -v`

预期: PASS

**步骤 5: 提交**

```bash
git add internal/apiserver/server.go cmd/agent-terminal/app.go cmd/agent-terminal/frontend/vue-app/stores/threads.js cmd/agent-terminal/frontend/vue-app/stores/__tests__/threads.runtime-sync-event.test.mjs
git rm cmd/agent-terminal/frontend/vue-app/stores/thread-runtime-sync-policy.js cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-runtime-sync-policy.test.mjs
git commit -m "refactor(ui): backend-drive runtime sync signal"
```

---

## 任务 6: 文档与完成前验证（收口）

**文件:**
- 修改: `docs/plans/2026-02-18-ui-stateless-migration.md`
- 修改: `cmd/agent-terminal/frontend/vue-app/stores/thread-state-whitelist.js`

**步骤 1: 写失败的验证用例（白名单/约束）**

```js
// stores/__tests__/thread-state-whitelist.test.mjs 新增断言
assert.equal(THREAD_STORE_UI_LOCAL_STATE_WHITELIST.includes('projects'), false);
assert.equal(THREAD_STORE_UI_LOCAL_STATE_WHITELIST.includes('dashboard'), false);
```

**步骤 2: 运行验证确认失败**

运行: `node --test cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-state-whitelist.test.mjs`
预期: FAIL（若误把业务态残留在 JS root state）

**步骤 3: 更新文档与白名单**

```md
# docs/plans/2026-02-18-ui-stateless-migration.md
- Phase 2 完成标准：
  - JS 不再维护 projects/dashboard 业务状态
  - Runtime 同步由 Go `ui/state/changed` 驱动
```

```js
// thread-state-whitelist.js
const UI_LOCAL_STATE_KEYS = Object.freeze([
  'activeThreadId',
  'activeCmdThreadId',
  'mainAgentId',
  'viewPrefs',
  'loadingThreads',
  'sending',
]);
```

**步骤 4: 运行全量完成前验证**

运行:
- `go test ./internal/apiserver ./internal/uistate -v`
- `find cmd/agent-terminal/frontend/vue-app -name '*.test.mjs' -print0 | xargs -0 node --test`
- `go test ./cmd/agent-terminal/... -v`

预期: 全部 PASS

**步骤 5: 提交**

```bash
git add docs/plans/2026-02-18-ui-stateless-migration.md cmd/agent-terminal/frontend/vue-app/stores/thread-state-whitelist.js cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-state-whitelist.test.mjs
git commit -m "docs: finalize phase2 state-downshift acceptance"
```

---

计划完成并保存到 `docs/plans/2026-02-18-ui-go-state-downshift-phase2.md`。两种执行选项：

**1. 子代理驱动（本会话）(Subagent-Driven)** - 每任务派遣新子代理，任务间审查，快速迭代

**2. 并行会话（单独）(Parallel Session)** - 新会话用 @执行计划，分批执行带检查点

选哪个？
