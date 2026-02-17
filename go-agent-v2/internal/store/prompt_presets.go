// prompt_presets.go — 预置常用提示词模板 (对应 Python config/prompt_template_presets.py, 8 条)。
package store

import "context"

// CommonPromptPresets 是系统内置的 8 条通用提示词模板。
// 对应 Python _COMMON_PROMPT_TEMPLATES 列表。
var CommonPromptPresets = []PromptTemplate{
	{
		PromptKey: "orch.review.plan_dag",
		Title:     "主Agent审阅模式（握手+拆DAG）",
		AgentKey:  "master",
		ToolName:  "task",
		PromptText: `你是主Agent（A0）。当前 MODE=review，只做拆解不执行。

流程：
1) 读取计划全文，先输出：目标 / 范围 / 不做。
2) 完成 MCP 能力握手：列工具分组、做只读探针。
3) 输出能力矩阵（可用/不可用/降级方案）。
4) 按 Phase + Batch 产出 DAG，并给出 task.create 草案。
5) 明确并行边界与冲突资源，禁止未握手先执行。

输出格式：
A. 执行总览
B. DAG任务清单
C. 最近一次验证结果
D. 下一步动作（等待"开始执行"）

参数：PROJECT_ROOT / PLAN_FILE / PROJECT_TAG / MODE=review`,
		Variables: map[string]any{
			"PROJECT_ROOT": "项目根目录",
			"PLAN_FILE":    "计划文件路径",
			"PROJECT_TAG":  "项目标签",
			"MODE":         "review",
		},
		Tags:    []string{"preset", "orchestration", "review"},
		Enabled: true,
	},
	{
		PromptKey: "orch.run.plan_dag",
		Title:     "主Agent执行模式（按DAG推进）",
		AgentKey:  "master",
		ToolName:  "task",
		PromptText: `你是主Agent（A0）。当前 MODE=run，完成握手后按 DAG 执行。

强制要求：
1) 先握手，后执行。
2) 先做 B00 基线，再推进后续批次。
3) 并行任务必须文件隔离，冲突资源先 lock.acquire。
4) 每批次必须提交证据：变更文件、验证命令、task.update。
5) 失败批次必须回滚并写修复方案。
6) 禁止 iterm(action="launch")，统一使用命令卡 launch.wjboot.workspace。

执行流程：
Step0 检查子Agent工作区
Step1 创建任务DAG
Step2 并行分发 ready 批次
Step3 持续汇总进度并处理阻塞
Step4 Gatekeeper 验收与最终报告

输出格式：A/B/C/D 四段固定。`,
		Variables: map[string]any{
			"PROJECT_ROOT": "项目根目录",
			"PLAN_FILE":    "计划文件路径",
			"PROJECT_TAG":  "项目标签",
			"MODE":         "run",
		},
		Tags:    []string{"preset", "orchestration", "run"},
		Enabled: true,
	},
	{
		PromptKey: "orch.handshake.readonly_probe",
		Title:     "MCP能力握手（只读探针）",
		AgentKey:  "master",
		ToolName:  "db",
		PromptText: `在任何实施前先完成 MCP 握手：
1) 输出可调用工具分组与函数清单；
2) 只读探针：db.query / iterm.list / task.ready / command_card.list / lock.list / approval.list；
3) 输出能力矩阵（可用/不可用/降级方案）；
4) 若探针失败，先修复链路再继续。

未完成握手前禁止执行改动。`,
		Tags:    []string{"preset", "handshake", "safety"},
		Enabled: true,
	},
	{
		PromptKey: "scheduler.lock_dispatch",
		Title:     "调度器：先加锁再分发",
		AgentKey:  "scheduler",
		ToolName:  "lock",
		PromptText: `你是调度器（A2）。在 task.assign 前执行：
1) 检查任务声明的 files_glob / output_glob；
2) 对冲突资源执行 lock.acquire；
3) 加锁失败则标记 blocked 并上报 owner；
4) 加锁成功再 interaction + task.assign；
5) 任务完成或失败后必须 lock.release。

输出：任务ID、锁资源、owner、派发结果。`,
		Tags:    []string{"preset", "scheduler", "lock"},
		Enabled: true,
	},
	{
		PromptKey: "worker.batch.execute",
		Title:     "Worker批次执行模板",
		AgentKey:  "worker",
		ToolName:  "task",
		PromptText: `你是 Worker。仅处理分配给你的单批次任务。

要求：
1) 先复述任务目标与边界；
2) 仅修改授权文件范围；
3) 完成后提交证据：变更文件清单、验证命令、输出摘要；
4) 不直接宣布最终通过，由 Gatekeeper 统一验收。

输出格式：
- 变更文件
- 验证命令
- 结果摘要
- 风险与回滚点`,
		Tags:    []string{"preset", "worker", "execution"},
		Enabled: true,
	},
	{
		PromptKey: "gatekeeper.batch.verify",
		Title:     "Gatekeeper门禁验收模板",
		AgentKey:  "gatekeeper",
		ToolName:  "task",
		PromptText: `你是 Gatekeeper（A1），负责唯一验收结论。

每批次必做：
1) 执行门禁命令（go test/go build/go vet）；
2) 检查任务证据完整性；
3) 通过则 task.update(status=done)；
4) 失败则 task.update(status=failed) 并给修复建议。

输出：门禁命令、结论、阻塞项、下一步。`,
		Tags:    []string{"preset", "gatekeeper", "verification"},
		Enabled: true,
	},
	{
		PromptKey: "incident.batch.rollback",
		Title:     "失败批次回滚模板",
		AgentKey:  "master",
		ToolName:  "task",
		PromptText: `当批次失败时执行：
1) 回滚到批次开始前状态；
2) 记录失败原因（根因、触发条件、影响面）；
3) 生成修复子任务并设置 depends_on；
4) 更新 task / interaction 并同步风险。

输出：回滚结果、失败根因、修复计划、是否可继续推进。`,
		Tags:    []string{"preset", "rollback", "incident"},
		Enabled: true,
	},
	{
		PromptKey: "report.round.abcd",
		Title:     "轮次进度汇报模板（ABCD）",
		AgentKey:  "master",
		ToolName:  "task",
		PromptText: `每轮汇报请固定输出：
A. 执行总览（当前阶段、已完成/进行中/阻塞）
B. DAG任务清单（task_id、assignee、depends_on、status）
C. 最近一次验证结果（命令 + 结论）
D. 下一步动作（可直接执行）

保持简洁，避免无证据结论。`,
		Tags:    []string{"preset", "reporting", "status"},
		Enabled: true,
	},
}

// SeedPromptPresets 批量 UPSERT 预置模板到 DB。
// 使用 PromptTemplateStore.Save 实现幂等写入。
func SeedPromptPresets(ctx context.Context, s *PromptTemplateStore) (int, error) {
	count := 0
	for i := range CommonPromptPresets {
		preset := CommonPromptPresets[i]
		preset.CreatedBy = "system"
		preset.UpdatedBy = "system"
		if _, err := s.Save(ctx, &preset); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
