package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

// TestE2E_FullOrchestrationCapability_MasterAnd4Workers
//
// 覆盖能力:
//  1. 定义主 Agent (thread/start)
//  2. 唤起 4 个子 Agent (orchestration_launch_agent)
//  3. 4 个子 Agent 协作编辑同一个代码文件(不同占位符)
//  4. DAG 创建/读取/节点状态更新 (task_create_dag/task_get_dag/task_update_node)
//  5. 命令卡能力 (command_get/command_list)
//  6. 提示词模板能力 (prompt_get/prompt_list)
//  7. skills 能力 (skills/config/write + skills/list + per-session skills)
//
// 运行:
//
//	RUN_FULL_ORCH_E2E=1 go test -v -run TestE2E_FullOrchestrationCapability_MasterAnd4Workers ./internal/apiserver
func TestE2E_FullOrchestrationCapability_MasterAnd4Workers(t *testing.T) {
	if os.Getenv("RUN_FULL_ORCH_E2E") != "1" {
		t.Skip("set RUN_FULL_ORCH_E2E=1 to run full orchestration capability test")
	}

	env := setupDashTestServer(t)
	defer env.pool.Close()
	defer env.srv.mgr.StopAll()

	ctx := context.Background()
	if err := database.Migrate(ctx, env.pool, "../../migrations"); err != nil {
		t.Fatalf("migrate DB: %v", err)
	}

	rootDir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	targetFile := filepath.Join(t.TempDir(), "orchestration_capability_target.go")
	initialCode := `package capability

type CapabilityTarget struct {
	Master string
	Agent1 string
	Agent2 string
	Agent3 string
	Agent4 string
}

var Target = CapabilityTarget{
	Master: "MASTER_BOOTSTRAP_PENDING",
	Agent1: "AGENT_1_PENDING",
	Agent2: "AGENT_2_PENDING",
	Agent3: "AGENT_3_PENDING",
	Agent4: "AGENT_4_PENDING",
}
`
	if err := os.WriteFile(targetFile, []byte(initialCode), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	t.Logf("target file: %s", targetFile)

	stamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	cardKey := "e2e.orch.full.card." + stamp
	promptKey := "e2e.orch.full.prompt." + stamp
	dagKey := "e2e.orch.full.dag." + stamp
	skillName := "e2e-orch-full-skill-" + stamp

	cardArgsSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patch": map[string]any{"type": "string"},
		},
		"required": []string{"patch"},
	}
	cardArgsSchemaJSON, _ := json.Marshal(cardArgsSchema)
	_, err = env.pool.Exec(ctx, `
		INSERT INTO command_cards (
			card_key, title, description, command_template, args_schema,
			risk_level, enabled, created_by, updated_by, updated_at
		) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, NOW())
		ON CONFLICT (card_key) DO UPDATE SET
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			command_template = EXCLUDED.command_template,
			args_schema = EXCLUDED.args_schema,
			risk_level = EXCLUDED.risk_level,
			enabled = EXCLUDED.enabled,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
	`, cardKey,
		"E2E Orchestration Patch Card",
		"Patch one placeholder token in target code file",
		"apply_patch <<'PATCH'\n{{patch}}\nPATCH",
		string(cardArgsSchemaJSON),
		"low", true, "e2e-test", "e2e-test",
	)
	if err != nil {
		t.Fatalf("insert command card: %v", err)
	}

	promptVarsJSON, _ := json.Marshal([]string{"target_file", "from", "to"})
	promptTagsJSON, _ := json.Marshal([]string{"e2e", "orchestration"})
	_, err = env.pool.Exec(ctx, `
		INSERT INTO prompt_templates (
			prompt_key, title, agent_key, tool_name, prompt_text,
			variables, tags, description, enabled, created_by, updated_by, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, NOW())
		ON CONFLICT (prompt_key) DO UPDATE SET
			title = EXCLUDED.title,
			agent_key = EXCLUDED.agent_key,
			tool_name = EXCLUDED.tool_name,
			prompt_text = EXCLUDED.prompt_text,
			variables = EXCLUDED.variables,
			tags = EXCLUDED.tags,
			description = EXCLUDED.description,
			enabled = EXCLUDED.enabled,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
	`, promptKey,
		"E2E Worker Patch Prompt",
		"worker",
		"orchestration_send_message",
		"Replace one token in target file and return DONE",
		string(promptVarsJSON),
		string(promptTagsJSON),
		"Prompt template for sub-agent patch task",
		true,
		"e2e-test",
		"e2e-test",
	)
	if err != nil {
		_, fallbackErr := env.pool.Exec(ctx, `
			INSERT INTO prompt_templates (
				prompt_key, title, agent_key, tool_name, prompt_text,
				variables, tags, enabled, created_by, updated_by, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, NOW())
			ON CONFLICT (prompt_key) DO UPDATE SET
				title = EXCLUDED.title,
				agent_key = EXCLUDED.agent_key,
				tool_name = EXCLUDED.tool_name,
				prompt_text = EXCLUDED.prompt_text,
				variables = EXCLUDED.variables,
				tags = EXCLUDED.tags,
				enabled = EXCLUDED.enabled,
				updated_by = EXCLUDED.updated_by,
				updated_at = NOW()
		`, promptKey,
			"E2E Worker Patch Prompt",
			"worker",
			"orchestration_send_message",
			"Replace one token in target file and return DONE",
			string(promptVarsJSON),
			string(promptTagsJSON),
			true,
			"e2e-test",
			"e2e-test",
		)
		if fallbackErr != nil {
			t.Fatalf("insert prompt template failed: primary=%v fallback=%v", err, fallbackErr)
		}
	}

	cmdRaw := env.srv.resourceCommandGet(mustToolArgs(t, map[string]any{"card_key": cardKey}))
	requireToolNoError(t, cmdRaw)
	var cmd store.CommandCard
	if err := json.Unmarshal([]byte(cmdRaw), &cmd); err != nil {
		t.Fatalf("unmarshal command_get: %v", err)
	}
	if cmd.CardKey != cardKey {
		t.Fatalf("command_get mismatch: got=%s want=%s", cmd.CardKey, cardKey)
	}
	t.Logf("command_get ok: %s", cmd.CardKey)

	cmdListRaw := env.srv.resourceCommandList(mustToolArgs(t, map[string]any{"keyword": cardKey}))
	requireToolNoError(t, cmdListRaw)
	if !strings.Contains(cmdListRaw, cardKey) {
		t.Fatalf("command_list missing key %s: %s", cardKey, cmdListRaw)
	}

	promptRaw := env.srv.resourcePromptGet(mustToolArgs(t, map[string]any{"prompt_key": promptKey}))
	requireToolNoError(t, promptRaw)
	var pt store.PromptTemplate
	if err := json.Unmarshal([]byte(promptRaw), &pt); err != nil {
		t.Fatalf("unmarshal prompt_get: %v", err)
	}
	if pt.PromptKey != promptKey {
		t.Fatalf("prompt_get mismatch: got=%s want=%s", pt.PromptKey, promptKey)
	}
	t.Logf("prompt_get ok: %s", pt.PromptKey)

	promptListRaw := env.srv.resourcePromptList(mustToolArgs(t, map[string]any{"keyword": promptKey}))
	requireToolNoError(t, promptListRaw)
	if !strings.Contains(promptListRaw, promptKey) {
		t.Fatalf("prompt_list missing key %s: %s", promptKey, promptListRaw)
	}

	masterRespAny, err := env.srv.InvokeMethod(ctx, "thread/start", mustToolArgs(t, map[string]any{
		"cwd": rootDir,
	}))
	if err != nil {
		t.Fatalf("start master thread: %v", err)
	}
	var masterResp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	decodeAny(t, masterRespAny, &masterResp)
	masterID := masterResp.Thread.ID
	if masterID == "" {
		t.Fatal("master thread id is empty")
	}
	t.Logf("master agent id: %s", masterID)
	_, err = env.srv.InvokeMethod(ctx, "thread/approvals/set", mustToolArgs(t, map[string]any{
		"threadId": masterID,
		"policy":   "never",
	}))
	if err != nil {
		t.Fatalf("set master approvals policy: %v", err)
	}

	skillRespAny, err := env.srv.InvokeMethod(ctx, "skills/config/write", mustToolArgs(t, map[string]any{
		"name":    skillName,
		"content": "# E2E Orchestration Skill\n\nThis is a temporary test skill.",
	}))
	if err != nil {
		t.Fatalf("skills/config/write (file mode): %v", err)
	}
	var skillResp struct {
		Path string `json:"path"`
	}
	decodeAny(t, skillRespAny, &skillResp)
	if skillResp.Path == "" {
		t.Fatalf("skills/config/write returned empty path: %+v", skillRespAny)
	}
	defer os.RemoveAll(filepath.Dir(skillResp.Path))

	_, err = env.srv.InvokeMethod(ctx, "skills/config/write", mustToolArgs(t, map[string]any{
		"agent_id": masterID,
		"skills":   []string{skillName},
	}))
	if err != nil {
		t.Fatalf("skills/config/write (session mode): %v", err)
	}
	masterSkills := env.srv.GetAgentSkills(masterID)
	if !slices.Contains(masterSkills, skillName) {
		t.Fatalf("master skills missing %s: %v", skillName, masterSkills)
	}

	skillsAny, err := env.srv.InvokeMethod(ctx, "skills/list", mustToolArgs(t, map[string]any{}))
	if err != nil {
		t.Fatalf("skills/list: %v", err)
	}
	var skillsResp struct {
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	decodeAny(t, skillsAny, &skillsResp)
	foundSkill := false
	for _, s := range skillsResp.Skills {
		if s.Name == skillName {
			foundSkill = true
			break
		}
	}
	if !foundSkill {
		t.Fatalf("skills/list missing %s", skillName)
	}
	t.Logf("skills ok: %s", skillName)

	_, err = env.srv.InvokeMethod(ctx, "thread/skills/list", mustToolArgs(t, map[string]any{
		"threadId": masterID,
	}))
	if err != nil {
		t.Fatalf("thread/skills/list: %v", err)
	}

	workerIDs := make([]string, 0, 4)
	for i := 1; i <= 4; i++ {
		launchRaw := env.srv.orchestrationLaunchAgent(mustToolArgs(t, map[string]any{
			"name":   fmt.Sprintf("worker-%d", i),
			"cwd":    rootDir,
			"prompt": fmt.Sprintf("You are worker-%d for orchestration capability test.", i),
		}))
		requireToolNoError(t, launchRaw)
		var launched struct {
			AgentID string `json:"agent_id"`
			Status  string `json:"status"`
		}
		if err := json.Unmarshal([]byte(launchRaw), &launched); err != nil {
			t.Fatalf("unmarshal launch response: %v", err)
		}
		if launched.AgentID == "" || launched.Status != "running" {
			t.Fatalf("invalid launch response: %s", launchRaw)
		}
		_, err = env.srv.InvokeMethod(ctx, "thread/approvals/set", mustToolArgs(t, map[string]any{
			"threadId": launched.AgentID,
			"policy":   "never",
		}))
		if err != nil {
			t.Fatalf("set worker approvals policy (%s): %v", launched.AgentID, err)
		}
		workerIDs = append(workerIDs, launched.AgentID)
		t.Logf("worker launched: %s", launched.AgentID)
	}

	nodes := make([]map[string]any, 0, 4)
	for idx, workerID := range workerIDs {
		nodeKey := fmt.Sprintf("node-%d", idx+1)
		deps := []string{}
		if idx > 0 {
			deps = []string{fmt.Sprintf("node-%d", idx)}
		}
		nodes = append(nodes, map[string]any{
			"node_key":    nodeKey,
			"title":       fmt.Sprintf("Worker-%d patch target file", idx+1),
			"assigned_to": workerID,
			"depends_on":  deps,
			"command_ref": cardKey,
		})
	}

	dagCreateRaw := env.srv.resourceTaskCreateDAG(mustToolArgs(t, map[string]any{
		"dag_key":     dagKey,
		"title":       "E2E Full Orchestration Capability Test DAG",
		"description": "Master + 4 workers edit one file and report completion",
		"nodes":       nodes,
	}))
	requireToolNoError(t, dagCreateRaw)
	if !strings.Contains(dagCreateRaw, `"nodes_created":4`) {
		t.Fatalf("unexpected dag create response: %s", dagCreateRaw)
	}
	t.Logf("dag created: %s", dagKey)

	if err := env.srv.mgr.Submit(masterID, fmt.Sprintf(
		"Master bootstrap step: edit file %s, replace MASTER_BOOTSTRAP_PENDING with MASTER_BOOTSTRAP_DONE. Only this token.",
		targetFile,
	), nil, nil); err != nil {
		t.Fatalf("submit master bootstrap prompt: %v", err)
	}
	if !waitFileTokenReplaced(targetFile, "MASTER_BOOTSTRAP_PENDING", "MASTER_BOOTSTRAP_DONE", 90*time.Second) {
		data, _ := os.ReadFile(targetFile)
		t.Fatalf("master did not patch target file in time; content:\n%s", string(data))
	}

	for idx, workerID := range workerIDs {
		nodeKey := fmt.Sprintf("node-%d", idx+1)
		oldToken := fmt.Sprintf("AGENT_%d_PENDING", idx+1)
		newToken := fmt.Sprintf("AGENT_%d_DONE_BY_WORKER_%d", idx+1, idx+1)

		updateRunningRaw := env.srv.resourceTaskUpdateNode(mustToolArgs(t, map[string]any{
			"dag_key":  dagKey,
			"node_key": nodeKey,
			"status":   "running",
			"result":   fmt.Sprintf("worker %d started", idx+1),
		}))
		requireToolNoError(t, updateRunningRaw)

		fallbackCmd := fmt.Sprintf(`python - <<'PY'
from pathlib import Path
p = Path(%q)
text = p.read_text()
old = %q
new = %q
if old not in text:
    raise SystemExit("token not found")
p.write_text(text.replace(old, new, 1))
print("DONE")
PY`, targetFile, oldToken, newToken)

		patched := false
		for attempt := 1; attempt <= 2; attempt++ {
			msg := fmt.Sprintf(`你是子Agent-%d，执行一次精确改动测试（第 %d 次）。
目标文件：%s
只做一个替换：把 "%s" 替换成 "%s"
必须要求：
1. 立即使用工具执行修改（不要只回复文字）
2. 优先用 apply_patch
3. 如果 apply_patch 失败，立刻执行这条命令：
%s
4. 参考命令卡 key: %s
5. 参考提示词 key: %s
完成后回复 DONE-%d。`,
				idx+1, attempt, targetFile, oldToken, newToken, fallbackCmd, cardKey, promptKey, idx+1)

			sendRaw := env.srv.orchestrationSendMessage(mustToolArgs(t, map[string]any{
				"agent_id": workerID,
				"message":  msg,
			}))
			requireToolNoError(t, sendRaw)

			if waitFileTokenReplaced(targetFile, oldToken, newToken, 75*time.Second) {
				patched = true
				break
			}
		}
		if !patched {
			data, _ := os.ReadFile(targetFile)
			t.Fatalf("worker %d did not patch target file in time; content:\n%s", idx+1, string(data))
		}

		updateDoneRaw := env.srv.resourceTaskUpdateNode(mustToolArgs(t, map[string]any{
			"dag_key":  dagKey,
			"node_key": nodeKey,
			"status":   "done",
			"result":   fmt.Sprintf("worker %d patched %s -> %s", idx+1, oldToken, newToken),
		}))
		requireToolNoError(t, updateDoneRaw)
	}

	dagDetailRaw := env.srv.resourceTaskGetDAG(mustToolArgs(t, map[string]any{"dag_key": dagKey}))
	requireToolNoError(t, dagDetailRaw)
	var dagDetail struct {
		DAG   store.TaskDAG       `json:"dag"`
		Nodes []store.TaskDAGNode `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(dagDetailRaw), &dagDetail); err != nil {
		t.Fatalf("unmarshal dag detail: %v", err)
	}
	if dagDetail.DAG.DagKey != dagKey {
		t.Fatalf("dag key mismatch: got=%s want=%s", dagDetail.DAG.DagKey, dagKey)
	}
	if len(dagDetail.Nodes) != 4 {
		t.Fatalf("dag nodes mismatch: got=%d want=4", len(dagDetail.Nodes))
	}
	for _, node := range dagDetail.Nodes {
		if node.Status != "done" {
			t.Fatalf("node %s status=%s want=done", node.NodeKey, node.Status)
		}
	}

	finalData, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("read final target file: %v", err)
	}
	finalText := string(finalData)
	expectedTokens := []string{
		"MASTER_BOOTSTRAP_DONE",
		"AGENT_1_DONE_BY_WORKER_1",
		"AGENT_2_DONE_BY_WORKER_2",
		"AGENT_3_DONE_BY_WORKER_3",
		"AGENT_4_DONE_BY_WORKER_4",
	}
	for _, token := range expectedTokens {
		if !strings.Contains(finalText, token) {
			t.Fatalf("final file missing token %s:\n%s", token, finalText)
		}
	}
	t.Logf("full orchestration capability test passed; dag=%s workers=%d", dagKey, len(workerIDs))
}

func mustToolArgs(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return json.RawMessage(data)
}

func requireToolNoError(t *testing.T, raw string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return
	}
	if errVal, ok := m["error"]; ok && errVal != nil && fmt.Sprint(errVal) != "" {
		t.Fatalf("tool returned error: %v (raw=%s)", errVal, raw)
	}
}

func decodeAny(t *testing.T, src any, dst any) {
	t.Helper()
	data, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal any: %v", err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("unmarshal any: %v", err)
	}
}

func waitFileTokenReplaced(path, oldToken, newToken string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			text := string(data)
			if strings.Contains(text, newToken) && !strings.Contains(text, oldToken) {
				return true
			}
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return false
}
