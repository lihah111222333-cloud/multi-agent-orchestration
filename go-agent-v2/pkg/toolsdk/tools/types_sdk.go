package tools

import "time"

type CodeRunRequest struct {
	Mode     string        `json:"mode"`
	Language string        `json:"language"`
	Code     string        `json:"code,omitempty"`
	Command  string        `json:"command,omitempty"`
	TestFunc string        `json:"test_func,omitempty"`
	TestPkg  string        `json:"test_pkg,omitempty"`
	AutoWrap bool          `json:"auto_wrap"`
	WorkDir  string        `json:"work_dir,omitempty"`
	Timeout  time.Duration `json:"timeout,omitempty"`
}

type CodeRunResult struct {
	Success   bool          `json:"success"`
	Output    string        `json:"output"`
	ExitCode  int           `json:"exit_code"`
	Duration  time.Duration `json:"duration"`
	Language  string        `json:"language"`
	Mode      string        `json:"mode"`
	Truncated bool          `json:"truncated"`
}

type AuditEvent struct {
	Ts        time.Time `json:"ts"`
	EventType string    `json:"event_type"`
	Action    string    `json:"action"`
	Result    string    `json:"result"`
	Actor     string    `json:"actor"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	Level     string    `json:"level"`
	Extra     any       `json:"extra"`
}
