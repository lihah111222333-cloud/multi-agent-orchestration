package store

import (
	"errors"
	"time"
)

var (
	ErrInvalidPath, ErrReadOnlyViolation = errors.New("invalid file path"), errors.New("read-only SQL violation: write keywords detected")
	ErrMultiStatement, ErrDangerousSQL   = errors.New("only single SQL statement allowed"), errors.New("dangerous SQL operation blocked")
)

type Interaction struct {
	ID             int        `db:"id" json:"id"`
	ThreadID       string     `db:"thread_id" json:"thread_id"`
	ParentID       *int       `db:"parent_id" json:"parent_id"`
	Sender         string     `db:"sender" json:"sender"`
	Receiver       string     `db:"receiver" json:"receiver"`
	MsgType        string     `db:"msg_type" json:"msg_type"`
	Status         string     `db:"status" json:"status"`
	RequiresReview bool       `db:"requires_review" json:"requires_review"`
	ReviewedBy     string     `db:"reviewed_by" json:"reviewed_by"`
	ReviewNote     string     `db:"review_note" json:"review_note"`
	ReviewedAt     *time.Time `db:"reviewed_at" json:"reviewed_at"`
	Payload        any        `db:"payload" json:"payload"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
}

type TaskTrace struct {
	ID           int        `db:"id" json:"id"`
	TraceID      string     `db:"trace_id" json:"trace_id"`
	SpanID       string     `db:"span_id" json:"span_id"`
	ParentSpanID *string    `db:"parent_span_id" json:"parent_span_id"`
	SpanName     string     `db:"span_name" json:"span_name"`
	Component    string     `db:"component" json:"component"`
	Status       string     `db:"status" json:"status"`
	Input        any        `db:"input_payload" json:"input_payload"`
	Output       any        `db:"output_payload" json:"output_payload"`
	ErrorText    string     `db:"error_text" json:"error_text"`
	Metadata     any        `db:"metadata" json:"metadata"`
	StartedAt    time.Time  `db:"started_at" json:"started_at"`
	FinishedAt   *time.Time `db:"finished_at" json:"finished_at"`
	DurationMS   int        `db:"duration_ms" json:"duration_ms"`
}

type PromptTemplate struct {
	ID          int       `db:"id" json:"id"`
	PromptKey   string    `db:"prompt_key" json:"prompt_key"`
	Title       string    `db:"title" json:"title"`
	AgentKey    string    `db:"agent_key" json:"agent_key"`
	ToolName    string    `db:"tool_name" json:"tool_name"`
	PromptText  string    `db:"prompt_text" json:"prompt_text"`
	Variables   any       `db:"variables" json:"variables"`
	Tags        any       `db:"tags" json:"tags"`
	Description string    `db:"description" json:"description"`
	Enabled     bool      `db:"enabled" json:"enabled"`
	CreatedBy   string    `db:"created_by" json:"created_by"`
	UpdatedBy   string    `db:"updated_by" json:"updated_by"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

type CommandCard struct {
	ID              int        `db:"id" json:"id"`
	CardKey         string     `db:"card_key" json:"card_key"`
	Title           string     `db:"title" json:"title"`
	Description     string     `db:"description" json:"description"`
	CommandTemplate string     `db:"command_template" json:"command_template"`
	ArgsSchema      any        `db:"args_schema" json:"args_schema"`
	RiskLevel       string     `db:"risk_level" json:"risk_level"`
	Enabled         bool       `db:"enabled" json:"enabled"`
	CreatedBy       string     `db:"created_by" json:"created_by"`
	UpdatedBy       string     `db:"updated_by" json:"updated_by"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
	LastRunAt       *time.Time `db:"last_run_at" json:"last_run_at,omitempty"`
	RunCount        int        `db:"run_count" json:"run_count"`
}

type TaskAck struct {
	ID            int        `db:"id" json:"id"`
	AckKey        string     `db:"ack_key" json:"ack_key"`
	Title         string     `db:"title" json:"title"`
	Description   string     `db:"description" json:"description"`
	AssignedTo    string     `db:"assigned_to" json:"assigned_to"`
	RequestedBy   string     `db:"requested_by" json:"requested_by"`
	Priority      string     `db:"priority" json:"priority"`
	Status        string     `db:"status" json:"status"`
	Progress      int        `db:"progress" json:"progress"`
	AckMessage    string     `db:"ack_message" json:"ack_message"`
	ResultSummary string     `db:"result_summary" json:"result_summary"`
	Metadata      any        `db:"metadata" json:"metadata"`
	DueAt         *time.Time `db:"due_at" json:"due_at"`
	AckedAt       *time.Time `db:"acked_at" json:"acked_at"`
	StartedAt     *time.Time `db:"started_at" json:"started_at"`
	FinishedAt    *time.Time `db:"finished_at" json:"finished_at"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

type TaskDAG struct {
	ID          int        `db:"id" json:"id"`
	DagKey      string     `db:"dag_key" json:"dag_key"`
	Title       string     `db:"title" json:"title"`
	Description string     `db:"description" json:"description"`
	Status      string     `db:"status" json:"status"`
	CreatedBy   string     `db:"created_by" json:"created_by"`
	Metadata    any        `db:"metadata" json:"metadata"`
	StartedAt   *time.Time `db:"started_at" json:"started_at"`
	FinishedAt  *time.Time `db:"finished_at" json:"finished_at"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at" json:"updated_at"`
}

type TaskDAGNode struct {
	ID         int        `db:"id" json:"id"`
	DagKey     string     `db:"dag_key" json:"dag_key"`
	NodeKey    string     `db:"node_key" json:"node_key"`
	Title      string     `db:"title" json:"title"`
	NodeType   string     `db:"node_type" json:"node_type"`
	AssignedTo string     `db:"assigned_to" json:"assigned_to"`
	DependsOn  any        `db:"depends_on" json:"depends_on"`
	Status     string     `db:"status" json:"status"`
	CommandRef string     `db:"command_ref" json:"command_ref"`
	Config     any        `db:"config" json:"config"`
	Result     any        `db:"result" json:"result"`
	StartedAt  *time.Time `db:"started_at" json:"started_at"`
	FinishedAt *time.Time `db:"finished_at" json:"finished_at"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time  `db:"updated_at" json:"updated_at"`
}

type WorkspaceRun struct {
	ID            int        `db:"id" json:"id"`
	RunKey        string     `db:"run_key" json:"run_key"`
	DagKey        string     `db:"dag_key" json:"dag_key"`
	SourceRoot    string     `db:"source_root" json:"source_root"`
	WorkspacePath string     `db:"workspace_path" json:"workspace_path"`
	Status        string     `db:"status" json:"status"`
	CreatedBy     string     `db:"created_by" json:"created_by"`
	UpdatedBy     string     `db:"updated_by" json:"updated_by"`
	Metadata      any        `db:"metadata" json:"metadata"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
	FinishedAt    *time.Time `db:"finished_at" json:"finished_at"`
}

type WorkspaceRunFile struct {
	ID                 int       `db:"id" json:"id"`
	RunKey             string    `db:"run_key" json:"run_key"`
	RelativePath       string    `db:"relative_path" json:"relative_path"`
	BaselineSHA256     string    `db:"baseline_sha256" json:"baseline_sha256"`
	WorkspaceSHA256    string    `db:"workspace_sha256" json:"workspace_sha256"`
	SourceSHA256Before string    `db:"source_sha256_before" json:"source_sha256_before"`
	SourceSHA256After  string    `db:"source_sha256_after" json:"source_sha256_after"`
	State              string    `db:"state" json:"state"`
	LastError          string    `db:"last_error" json:"last_error"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time `db:"updated_at" json:"updated_at"`
}

type AuditEvent struct {
	Ts        time.Time `db:"ts" json:"ts"`
	EventType string    `db:"event_type" json:"event_type"`
	Action    string    `db:"action" json:"action"`
	Result    string    `db:"result" json:"result"`
	Actor     string    `db:"actor" json:"actor"`
	Target    string    `db:"target" json:"target"`
	Detail    string    `db:"detail" json:"detail"`
	Level     string    `db:"level" json:"level"`
	Extra     any       `db:"extra" json:"extra"`
}

type SystemLog struct {
	ID         int       `db:"id" json:"id"`
	Ts         time.Time `db:"ts" json:"ts"`
	Level      string    `db:"level" json:"level"`
	Logger     string    `db:"logger" json:"logger"`
	Message    string    `db:"message" json:"message"`
	Raw        string    `db:"raw" json:"raw"`
	Source     string    `db:"source" json:"source"`
	Component  string    `db:"component" json:"component"`
	AgentID    string    `db:"agent_id" json:"agent_id"`
	ThreadID   string    `db:"thread_id" json:"thread_id"`
	TraceID    string    `db:"trace_id" json:"trace_id"`
	EventType  string    `db:"event_type" json:"event_type"`
	ToolName   string    `db:"tool_name" json:"tool_name"`
	DurationMS *int      `db:"duration_ms" json:"duration_ms"`
	Extra      any       `db:"extra" json:"extra"`
}

type AILogRow struct {
	Ts         time.Time `json:"ts"`
	Level      string    `json:"level"`
	Logger     string    `json:"logger"`
	Message    string    `json:"message"`
	Raw        string    `json:"raw"`
	Category   string    `json:"category"`
	Method     string    `json:"method"`
	URL        string    `json:"url"`
	Endpoint   string    `json:"endpoint"`
	StatusCode string    `json:"status_code"`
	StatusText string    `json:"status_text"`
	Model      string    `json:"model"`
}

type BusException struct {
	Ts        time.Time `db:"ts" json:"ts"`
	Category  string    `db:"category" json:"category"`
	Severity  string    `db:"severity" json:"severity"`
	Source    string    `db:"source" json:"source"`
	ToolName  string    `db:"tool_name" json:"tool_name"`
	Message   string    `db:"message" json:"message"`
	Traceback string    `db:"traceback" json:"traceback"`
	Extra     any       `db:"extra" json:"extra"`
}

type SharedFile struct {
	Path      string    `db:"path" json:"path"`
	Content   string    `db:"content" json:"content"`
	UpdatedBy string    `db:"updated_by" json:"updated_by"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

type AgentStatus struct {
	AgentID     string    `db:"agent_id" json:"agent_id"`
	AgentName   string    `db:"agent_name" json:"agent_name"`
	SessionID   string    `db:"session_id" json:"session_id"`
	Status      string    `db:"status" json:"status"`
	StagnantSec int       `db:"stagnant_sec" json:"stagnant_sec"`
	Error       string    `db:"error" json:"error"`
	OutputTail  any       `db:"output_tail" json:"output_tail"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

type TopologyApproval struct {
	ID           int       `db:"id" json:"id"`
	ProposalHash string    `db:"proposal_hash" json:"proposal_hash"`
	ProposalJSON any       `db:"proposal_json" json:"proposal_json"`
	Status       string    `db:"status" json:"status"`
	RequestedBy  string    `db:"requested_by" json:"requested_by"`
	ApprovedBy   *string   `db:"approved_by" json:"approved_by"`
	RejectedBy   *string   `db:"rejected_by" json:"rejected_by"`
	ExpiresAt    time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

type CommandCardRun struct {
	ID              int        `db:"id" json:"id"`
	CardKey         string     `db:"card_key" json:"card_key"`
	RequestedBy     string     `db:"requested_by" json:"requested_by"`
	Parameters      any        `db:"params" json:"params"`
	RenderedCommand string     `db:"rendered_command" json:"rendered_command"`
	RiskLevel       string     `db:"risk_level" json:"risk_level"`
	Status          string     `db:"status" json:"status"`
	RequiresReview  bool       `db:"requires_review" json:"requires_review"`
	InteractionID   *int       `db:"interaction_id" json:"interaction_id"`
	Output          string     `db:"output" json:"output"`
	Error           string     `db:"error" json:"error"`
	ExitCode        *int       `db:"exit_code" json:"exit_code"`
	ExecutedAt      *time.Time `db:"executed_at" json:"executed_at"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
}
