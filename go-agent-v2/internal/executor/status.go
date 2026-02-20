// status.go — 命令卡运行实例状态常量 (消除硬编码字符串散布)。
package executor

// 命令卡运行实例 (command_card_runs) 状态。
const (
	RunStatusReady         = "ready"
	RunStatusPendingReview = "pending_review"
	RunStatusRunning       = "running"
	RunStatusSuccess       = "success"
	RunStatusFailed        = "failed"
	RunStatusRejected      = "rejected"
)

// 审批决策。
const (
	DecisionApproved = "approved"
	DecisionRejected = "rejected"
)
