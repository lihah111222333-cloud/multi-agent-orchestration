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

const (
	defaultTimeoutSec = 240
	minTimeoutSec     = 1
	maxTimeoutSec     = 3600
	maxOutputLim      = 200000
)

var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])rm\s+-rf(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])shutdown(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])reboot(?:\s|$)`),
	regexp.MustCompile(`(?i)curl[^\n|]*\|\s*(?:bash|sh)(?:\s|$)`),
	regexp.MustCompile(`(?i)wget[^\n|]*\|\s*(?:bash|sh)(?:\s|$)`),
}

var placeholderRe = regexp.MustCompile(`\{([a-zA-Z_]\w*)\}`)

const runCols = `id, card_key, requested_by, params, rendered_command, risk_level,
	status, requires_review, interaction_id, output, error, exit_code,
	created_at, updated_at, executed_at`

type CommandCardExecutor struct {
	pool     *pgxpool.Pool
	cards    *store.CommandCardStore
	auditLog *store.AuditLogStore
}

func NewCommandCardExecutor(pool *pgxpool.Pool, cards *store.CommandCardStore, audit *store.AuditLogStore) *CommandCardExecutor {
	return &CommandCardExecutor{pool: pool, cards: cards, auditLog: audit}
}

type PrepareResult struct {
	OK               bool                  `json:"ok"`
	NeedsReview      bool                  `json:"needs_review"`
	DangerousCommand bool                  `json:"dangerous_command"`
	DangerousPattern string                `json:"dangerous_pattern,omitempty"`
	Run              *store.CommandCardRun `json:"run,omitempty"`
	Message          string                `json:"message,omitempty"`
}

func (e *CommandCardExecutor) Prepare(ctx context.Context, cardKey string, params map[string]string, requestedBy string) (*PrepareResult, error) {
	logger.Info("executor: prepare",
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
	needsReview := riskLevel == "high" || riskLevel == "critical" || dp != ""
	status := RunStatusReady
	if needsReview { status = RunStatusPendingReview }

	paramsJSON := "{}"
	if b, err := json.Marshal(params); err == nil {
		paramsJSON = string(b)
	} else {
		logger.Warn("executor: marshal params failed", logger.FieldError, err)
	}
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

type ReviewResult struct {
	OK      bool                  `json:"ok"`
	Run     *store.CommandCardRun `json:"run,omitempty"`
	Message string                `json:"message,omitempty"`
}

func (e *CommandCardExecutor) Review(ctx context.Context, runID int, decision, reviewer, note string) (*ReviewResult, error) {
	logger.Info("executor: review",
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
	if decision == DecisionApproved { nextStatus = RunStatusReady }

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

type ExecResult struct {
	OK       bool                  `json:"ok"`
	Output   string                `json:"output"`
	ExitCode int                   `json:"exit_code"`
	Run      *store.CommandCardRun `json:"run,omitempty"`
	Message  string                `json:"message,omitempty"`
}

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

	if _, err := e.pool.Exec(ctx, "UPDATE command_card_runs SET status=$2, updated_at=NOW() WHERE id=$1", runID, RunStatusRunning); err != nil {
		logger.Warn("executor: mark running failed", logger.FieldRunID, runID, logger.FieldError, err)
	}

	logger.Info("executor: executing command",
		logger.FieldRunID, runID,
		logger.FieldCommand, TruncateForAudit(command, 200),
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

	logger.Info("executor: command executed",
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

type RunOneOpts struct {
	AutoApprove bool
	Reviewer    string
	ReviewNote  string
	TimeoutSec  int
}

func (e *CommandCardExecutor) RunOne(ctx context.Context, cardKey string, params map[string]string, requestedBy string, opts RunOneOpts) (*ExecResult, error) {
	logger.Info("executor: run_one",
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

	if prepared.NeedsReview {
		switch {
		case !opts.AutoApprove:
			return &ExecResult{OK: true, Run: run, ExitCode: -1, Message: "命令已生成，等待人工审批"}, nil
		case prepared.DangerousCommand:
			return &ExecResult{OK: true, Run: run, ExitCode: -1, Message: "检测到危险命令模式，禁止自动审批，需人工审批"}, nil
		case run.RiskLevel != "low" && run.RiskLevel != "normal":
			return &ExecResult{OK: true, Run: run, ExitCode: -1, Message: "高风险命令禁止自动审批，需人工审批"}, nil
		}
		reviewer := opts.Reviewer
		if reviewer == "" { reviewer = requestedBy }
		reviewed, reviewErr := e.Review(ctx, run.ID, DecisionApproved, reviewer, opts.ReviewNote)
		if reviewErr != nil {
			return nil, reviewErr
		}
		if !reviewed.OK {
			return &ExecResult{OK: false, Message: reviewed.Message, ExitCode: -1}, nil
		}
	}

	return e.Execute(ctx, run.ID, requestedBy, opts.TimeoutSec)
}

func (e *CommandCardExecutor) GetRun(ctx context.Context, runID int) (*store.CommandCardRun, error) {
	rows, err := e.pool.Query(ctx,
		"SELECT "+runCols+" FROM command_card_runs WHERE id = $1", runID)
	if err != nil {
		return nil, err
	}
	return store.CollectOneExported[store.CommandCardRun](rows)
}

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

func renderTemplate(tmpl string, params map[string]string) (string, error) {
	result := tmpl
	for k, v := range params {
		placeholder := "{" + k + "}"
		escaped := shellQuote(v)
		result = strings.ReplaceAll(result, placeholder, escaped)
	}
	if match := placeholderRe.FindString(result); match != "" {
		return "", pkgerr.Newf("CommandCard.renderTemplate", "命令模板缺少参数: %s", match)
	}
	return result, nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func detectDangerous(command string) string {
	for _, p := range dangerousPatterns {
		if p.MatchString(command) {
			return p.String()
		}
	}
	return ""
}
