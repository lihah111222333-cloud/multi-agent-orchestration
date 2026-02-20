// Package executor 提供命令卡执行引擎 (对应 Python command_card_executor.py 681 行)。
//
// 流程: 模板渲染 → 危险检测 → 审批 → exec.CommandContext → 审计
// Go 优势: regexp.MustCompile 编译时、context.WithTimeout、struct tag 校验
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/store"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// ========================================
// 常量 (对应 Python 模块级常量)
// ========================================

const (
	defaultTimeoutSec = 240
	minTimeoutSec     = 1
	maxTimeoutSec     = 3600
	defaultOutputLim  = 20000
	maxOutputLim      = 200000
)

// 需要人工审批的风险等级。
var approvalRequiredRisks = map[string]bool{"high": true, "critical": true}

// 允许自动审批的风险等级。
var autoApproveAllowedRisks = map[string]bool{"low": true, "normal": true}

// dangerousPatterns 危险命令正则 (编译时检查，对应 Python _DANGEROUS_COMMAND_PATTERNS)。
var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])rm\s+-rf(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])shutdown(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])reboot(?:\s|$)`),
	regexp.MustCompile(`(?i)curl[^\n|]*\|\s*(?:bash|sh)(?:\s|$)`),
	regexp.MustCompile(`(?i)wget[^\n|]*\|\s*(?:bash|sh)(?:\s|$)`),
}

// placeholderRe 匹配占位符 {name} 格式 — 仅字母/数字/下划线，排除 JSON 等内容。
var placeholderRe = regexp.MustCompile(`\{([a-zA-Z_]\w*)\}`)

// run 表列名常量。
const runCols = `id, card_key, requested_by, params, rendered_command, risk_level,
	status, requires_review, interaction_id, output, error, exit_code,
	created_at, updated_at, executed_at`

// ========================================
// CommandCardExecutor
// ========================================

// CommandCardExecutor 命令卡执行器。
type CommandCardExecutor struct {
	pool     *pgxpool.Pool
	cards    *store.CommandCardStore
	auditLog *store.AuditLogStore
}

// NewCommandCardExecutor 创建命令卡执行器。
func NewCommandCardExecutor(pool *pgxpool.Pool, cards *store.CommandCardStore, audit *store.AuditLogStore) *CommandCardExecutor {
	return &CommandCardExecutor{pool: pool, cards: cards, auditLog: audit}
}

// ========================================
// Prepare — 渲染并创建运行实例 (对应 Python prepare_command_card_run)
// ========================================

// PrepareResult 准备结果。
type PrepareResult struct {
	OK               bool                  `json:"ok"`
	NeedsReview      bool                  `json:"needs_review"`
	DangerousCommand bool                  `json:"dangerous_command"`
	DangerousPattern string                `json:"dangerous_pattern,omitempty"`
	Run              *store.CommandCardRun `json:"run,omitempty"`
	Message          string                `json:"message,omitempty"`
}

// Prepare 创建命令卡运行实例，渲染模板并写入 DB (对应 Python prepare_command_card_run)。
func (e *CommandCardExecutor) Prepare(ctx context.Context, cardKey string, params map[string]string, requestedBy string) (*PrepareResult, error) {
	logger.Infow("executor: prepare",
		logger.FieldCardKey, cardKey,
		"requested_by", requestedBy,
	)
	cardKey = strings.TrimSpace(cardKey)
	if cardKey == "" {
		return &PrepareResult{OK: false, Message: "card_key 不能为空"}, nil
	}
	if requestedBy == "" {
		requestedBy = "agent"
	}

	card, err := e.cards.Get(ctx, cardKey)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return &PrepareResult{OK: false, Message: fmt.Sprintf("命令卡不存在: %s", cardKey)}, nil
	}
	if !card.Enabled {
		return &PrepareResult{OK: false, Message: fmt.Sprintf("命令卡已禁用: %s", cardKey)}, nil
	}

	rendered, err := renderTemplate(card.CommandTemplate, params)
	if err != nil {
		return &PrepareResult{OK: false, Message: err.Error()}, nil
	}

	riskLevel := strings.ToLower(strings.TrimSpace(card.RiskLevel))
	if riskLevel == "" {
		riskLevel = "normal"
	}
	dp := detectDangerous(rendered)
	needsReview := approvalRequiredRisks[riskLevel] || dp != ""

	status := RunStatusReady
	if needsReview {
		status = RunStatusPendingReview
	}

	// 写入 DB
	paramsJSON := marshalJSON(params)
	rows, err := e.pool.Query(ctx,
		`INSERT INTO command_card_runs (card_key, requested_by, params, rendered_command,
		    risk_level, status, requires_review, output, error)
		 VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, '', '')
		 RETURNING `+runCols,
		cardKey, requestedBy, paramsJSON, rendered, riskLevel, status, needsReview)
	if err != nil {
		return nil, pkgerr.Wrap(err, "CommandCard.Prepare", "insert run")
	}
	run, err := store.CollectOneExported[store.CommandCardRun](rows)
	if err != nil {
		return nil, err
	}

	// 审计
	if err := e.auditLog.Append(ctx, &store.AuditEvent{
		EventType: "command_card_run",
		Action:    "prepare",
		Result:    status,
		Actor:     requestedBy,
		Target:    cardKey,
		Detail:    fmt.Sprintf("run_id=%d dangerous=%v", run.ID, dp != ""),
		Level:     "INFO",
	}); err != nil {
		logger.Warn("executor: audit append failed", logger.FieldError, err)
	}

	return &PrepareResult{
		OK:               true,
		NeedsReview:      needsReview,
		DangerousCommand: dp != "",
		DangerousPattern: dp,
		Run:              run,
	}, nil
}

// ========================================
// Review — 审批/拒绝 (对应 Python review_command_card_run)
// ========================================

// ReviewResult 审批结果。
type ReviewResult struct {
	OK      bool                  `json:"ok"`
	Run     *store.CommandCardRun `json:"run,omitempty"`
	Message string                `json:"message,omitempty"`
}

// Review 对 pending_review 状态的运行实例进行审批 (对应 Python review_command_card_run)。
func (e *CommandCardExecutor) Review(ctx context.Context, runID int, decision, reviewer, note string) (*ReviewResult, error) {
	logger.Infow("executor: review",
		logger.FieldRunID, runID,
		logger.FieldDecision, decision,
		"reviewer", reviewer,
	)
	run, err := e.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return &ReviewResult{OK: false, Message: fmt.Sprintf("run 不存在: %d", runID)}, nil
	}

	if run.Status != RunStatusPendingReview {
		return &ReviewResult{OK: false, Run: run,
			Message: fmt.Sprintf("run 当前状态 (%s) 不允许审批，需 pending_review", run.Status)}, nil
	}

	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != DecisionApproved && decision != DecisionRejected {
		return &ReviewResult{OK: false, Message: "decision 必须是 approved/rejected"}, nil
	}

	nextStatus := RunStatusRejected
	if decision == DecisionApproved {
		nextStatus = RunStatusReady
	}

	rows, err := e.pool.Query(ctx,
		`UPDATE command_card_runs SET status=$1, updated_at=NOW()
		 WHERE id=$2 RETURNING `+runCols,
		nextStatus, runID)
	if err != nil {
		return nil, err
	}
	updated, err := store.CollectOneExported[store.CommandCardRun](rows)
	if err != nil {
		return nil, err
	}

	// 审计
	if err := e.auditLog.Append(ctx, &store.AuditEvent{
		EventType: "command_card_run",
		Action:    "review",
		Result:    decision,
		Actor:     reviewer,
		Target:    run.CardKey,
		Detail:    fmt.Sprintf("run_id=%d note=%s", runID, note),
		Level:     "INFO",
	}); err != nil {
		logger.Warn("executor: audit append failed", logger.FieldError, err)
	}

	return &ReviewResult{OK: true, Run: updated}, nil
}

// ========================================
// Execute — 实际执行命令 (对应 Python execute_command_card_run 的 subprocess 部分)
// ========================================

// ExecResult 执行结果。
type ExecResult struct {
	OK       bool                  `json:"ok"`
	Output   string                `json:"output"`
	ExitCode int                   `json:"exit_code"`
	Run      *store.CommandCardRun `json:"run,omitempty"`
	Message  string                `json:"message,omitempty"`
}

// Execute 执行已就绪的运行实例 (对应 Python execute_command_card_run)。
func (e *CommandCardExecutor) Execute(ctx context.Context, runID int, actor string, timeoutSec int) (*ExecResult, error) {
	if actor == "" {
		actor = "agent"
	}

	run, err := e.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return &ExecResult{OK: false, Message: fmt.Sprintf("run 不存在: %d", runID), ExitCode: -1}, nil
	}
	if run.Status != RunStatusReady {
		return &ExecResult{OK: false, Run: run, ExitCode: -1,
			Message: fmt.Sprintf("run 当前状态 (%s) 不可执行，需 ready", run.Status)}, nil
	}

	command := run.RenderedCommand
	if strings.TrimSpace(command) == "" {
		return &ExecResult{OK: false, Message: "空命令不可执行", ExitCode: -1}, nil
	}

	// 标记 running
	if _, err := e.pool.Exec(ctx, "UPDATE command_card_runs SET status=$2, updated_at=NOW() WHERE id=$1", runID, RunStatusRunning); err != nil {
		logger.Warn("executor: mark running failed", logger.FieldRunID, runID, logger.FieldError, err)
	}

	logger.Infow("executor: executing command",
		logger.FieldRunID, runID,
		logger.FieldCommand, func() string {
			if len(command) > 200 {
				return command[:200] + "..."
			}
			return command
		}(),
		"timeout_sec", timeoutSec,
		"actor", actor,
	)

	output, exitCode, execErr := runShellCommand(ctx, command, timeoutSec)

	status := RunStatusSuccess
	errText := ""
	if exitCode != 0 {
		status = RunStatusFailed
		if execErr != nil {
			errText = execErr.Error()
		}
	}

	// 更新 DB
	rows, err := e.pool.Query(ctx,
		`UPDATE command_card_runs
		 SET status=$1, output=$2, error=$3, exit_code=$4, executed_at=NOW(), updated_at=NOW()
		 WHERE id=$5 RETURNING `+runCols,
		status, output, errText, exitCode, runID)
	if err != nil {
		return nil, err
	}
	updated, err := store.CollectOneExported[store.CommandCardRun](rows)
	if err != nil {
		return nil, err
	}

	// 审计
	if err := e.auditLog.Append(ctx, &store.AuditEvent{
		EventType: "command_card_run",
		Action:    "execute",
		Result:    status,
		Actor:     actor,
		Detail:    fmt.Sprintf("run_id=%d exit_code=%d output_len=%d", runID, exitCode, len(output)),
		Level:     "INFO",
	}); err != nil {
		logger.Warn("executor: audit append failed", logger.FieldError, err)
	}

	logger.Infow("command executed",
		"run_id", runID,
		"exit_code", exitCode,
		"output_len", len(output),
		"actor", actor)

	return &ExecResult{
		OK:       exitCode == 0,
		Output:   output,
		ExitCode: exitCode,
		Run:      updated,
	}, nil
}

func runShellCommand(ctx context.Context, command string, timeoutSec int) (output string, exitCode int, err error) {
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeoutSec
	}
	timeout := util.ClampInt(timeoutSec, minTimeoutSec, maxTimeoutSec)
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = util.NewLimitedWriter(&stdout, maxOutputLim)
	cmd.Stderr = util.NewLimitedWriter(&stderr, maxOutputLim)

	execErr := cmd.Run()
	exitCode = 0
	if execErr != nil {
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	output = stdout.String()
	if stderr.Len() > 0 {
		output += "\n--- STDERR ---\n" + stderr.String()
	}
	if len(output) > maxOutputLim {
		output = output[:maxOutputLim] + "\n...[truncated]"
	}
	return output, exitCode, execErr
}

// ========================================
// RunOne — 一站式执行 (对应 Python execute_command_card)
// ========================================

// RunOneOpts 一站式执行参数。
type RunOneOpts struct {
	AutoApprove bool
	Reviewer    string
	ReviewNote  string
	TimeoutSec  int
}

// RunOne 一站式: Prepare → (Review) → Execute (对应 Python execute_command_card)。
func (e *CommandCardExecutor) RunOne(ctx context.Context, cardKey string, params map[string]string, requestedBy string, opts RunOneOpts) (*ExecResult, error) {
	logger.Infow("executor: run_one",
		logger.FieldCardKey, cardKey,
		"requested_by", requestedBy,
		"auto_approve", opts.AutoApprove,
	)
	prepared, err := e.Prepare(ctx, cardKey, params, requestedBy)
	if err != nil {
		return nil, err
	}
	if !prepared.OK {
		return &ExecResult{OK: false, Message: prepared.Message, ExitCode: -1}, nil
	}

	run := prepared.Run
	runID := run.ID

	// 需要审批的情况
	if prepared.NeedsReview {
		if !opts.AutoApprove {
			return &ExecResult{OK: true, Run: run, ExitCode: -1,
				Message: "命令已生成，等待人工审批"}, nil
		}
		// 危险命令禁止自动审批
		if prepared.DangerousCommand {
			return &ExecResult{OK: true, Run: run, ExitCode: -1,
				Message: "检测到危险命令模式，禁止自动审批，需人工审批"}, nil
		}
		// 高/严重风险禁止自动审批
		if !autoApproveAllowedRisks[run.RiskLevel] {
			return &ExecResult{OK: true, Run: run, ExitCode: -1,
				Message: "高风险命令禁止自动审批，需人工审批"}, nil
		}
		// 自动审批
		reviewer := opts.Reviewer
		if reviewer == "" {
			reviewer = requestedBy
		}
		reviewed, reviewErr := e.Review(ctx, runID, "approved", reviewer, opts.ReviewNote)
		if reviewErr != nil {
			return nil, reviewErr
		}
		if !reviewed.OK {
			return &ExecResult{OK: false, Message: reviewed.Message, ExitCode: -1}, nil
		}
	}

	return e.Execute(ctx, runID, requestedBy, opts.TimeoutSec)
}

// ========================================
// GetRun / ListRuns — 查询运行记录
// ========================================

// GetRun 获取单条运行记录 (对应 Python get_command_card_run)。
func (e *CommandCardExecutor) GetRun(ctx context.Context, runID int) (*store.CommandCardRun, error) {
	rows, err := e.pool.Query(ctx,
		"SELECT "+runCols+" FROM command_card_runs WHERE id = $1", runID)
	if err != nil {
		return nil, err
	}
	return store.CollectOneExported[store.CommandCardRun](rows)
}

// ListRuns 查询运行记录 (对应 Python list_command_card_runs)。
func (e *CommandCardExecutor) ListRuns(ctx context.Context, cardKey, status, requestedBy string, limit int) ([]store.CommandCardRun, error) {
	q := store.NewQueryBuilder().
		Eq("card_key", cardKey).
		Eq("status", status).
		Eq("requested_by", requestedBy)
	sql, params := q.Build("SELECT "+runCols+" FROM command_card_runs", "created_at DESC, id DESC", limit)
	rows, err := e.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return store.CollectRowsExported[store.CommandCardRun](rows)
}

// ========================================
// RecoverStaleRuns — 恢复超时任务 (对应 Python _recover_stale_runs)
// ========================================

// RecoverStaleRuns 恢复因崩溃卡在 running 状态的任务 (对应 Python _recover_stale_runs)。
func (e *CommandCardExecutor) RecoverStaleRuns(ctx context.Context, timeoutSec int) (int64, error) {
	threshold := util.ClampInt(timeoutSec*2, 300, 7200)
	tag, err := e.pool.Exec(ctx,
		`UPDATE command_card_runs SET status='failed', error='[timeout_recovery] process crash or timeout',
		 exit_code=-3, updated_at=NOW()
		 WHERE status='running' AND updated_at < NOW() - $1 * INTERVAL '1 second'`, threshold)
	if err != nil {
		return 0, err
	}
	count := tag.RowsAffected()
	if count > 0 {
		if err := e.auditLog.Append(ctx, &store.AuditEvent{
			EventType: "command_card_run",
			Action:    "recover_stale",
			Result:    "ok",
			Actor:     "system",
			Target:    "command_card_runs",
			Detail:    fmt.Sprintf("recovered %d stale running task(s)", count),
			Level:     "INFO",
		}); err != nil {
			logger.Warn("executor: audit append failed", logger.FieldError, err)
		}
	}
	return count, nil
}

// ========================================
// 内部工具 (DRY: 共享逻辑)
// ========================================

// renderTemplate 渲染命令模板 (对应 Python _render_template)。
func renderTemplate(tmpl string, params map[string]string) (string, error) {
	result := tmpl
	for k, v := range params {
		placeholder := "{" + k + "}"
		if !strings.Contains(result, placeholder) {
			continue
		}
		escaped := shellQuote(v)
		result = strings.ReplaceAll(result, placeholder, escaped)
	}
	// 检查未替换的占位符 — 只匹配 {identifier} 格式，避免 JSON 等内容误报
	if match := placeholderRe.FindString(result); match != "" {
		return "", pkgerr.Newf("CommandCard.renderTemplate", "命令模板缺少参数: %s", match)
	}
	return result, nil
}

// shellQuote 简单 shell 转义。
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// detectDangerous 检测危险命令模式 (对应 Python _detect_dangerous_pattern)。
func detectDangerous(command string) string {
	for _, p := range dangerousPatterns {
		if p.MatchString(command) {
			return p.String()
		}
	}
	return ""
}

// marshalJSON 安全序列化 (DRY helper)。
func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		logger.Debug("marshalJSON failed", logger.FieldError, err)
		return "{}"
	}
	return string(b)
}
