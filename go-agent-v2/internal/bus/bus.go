// Package bus 提供消息总线和系统编排。
//
// 两层架构:
//   - Agent 间通讯: 查 agent_threads 表获取端口 → 直接 HTTP/WS 互通 (router.go)
//   - 系统编排 (DAG/Task/命令卡/提示词): pub/sub fan-out → 多订阅者 (bus.go)
//
// 桥接:
//   - dashboard/sse.go EventBus — bus 事件自动转发到 SSE
//   - store/bus_log.go — 异常消息自动写入 bus_exception_logs
//   - codex.Client WS — Agent 事件自动发布到 bus
package bus

import (
	"encoding/json"
	"sync"
	"time"
)

// ========================================
// 消息类型
// ========================================

// Message 总线消息。
type Message struct {
	Topic     string          `json:"topic"`   // agent.a0.output / system.health / orchestration.begin
	From      string          `json:"from"`    // 来源 Agent ID 或 "system"
	To        string          `json:"to"`      // 目标 Agent ID 或 "*" (广播)
	Type      string          `json:"type"`    // 消息类型 (task_delegate / status_update / result / error)
	Payload   json.RawMessage `json:"payload"` // 具体数据
	Timestamp time.Time       `json:"timestamp"`
	Seq       int64           `json:"seq"` // 全局序列号
}

// 消息类型常量。
const (
	// --- Agent 间通讯 (直连 via agent_threads) ---

	// MsgTaskDelegate Agent A 委托任务给 Agent B。
	MsgTaskDelegate = "task_delegate"
	// MsgTaskResult Agent B 返回任务结果给 Agent A。
	MsgTaskResult = "task_result"
	// MsgStatusUpdate Agent 广播状态变化。
	MsgStatusUpdate = "status_update"
	// MsgAgentOutput Agent 输出转发 (agent_message_delta)。
	MsgAgentOutput = "agent_output"
	// MsgAgentEvent Agent 事件转发 (turn_started/turn_complete/idle 等)。
	MsgAgentEvent = "agent_event"
	// MsgError 异常消息。
	MsgError = "error"
	// MsgOrchestration 编排状态变化。
	MsgOrchestration = "orchestration"

	// --- 系统编排 (必须经总线 fan-out) ---

	// MsgDAGNodeStart DAG 节点开始执行。
	MsgDAGNodeStart = "dag.node_start"
	// MsgDAGNodeComplete DAG 节点执行完成。
	MsgDAGNodeComplete = "dag.node_complete"
	// MsgDAGNodeFail DAG 节点执行失败。
	MsgDAGNodeFail = "dag.node_fail"
	// MsgDAGRunStart DAG 整体运行开始。
	MsgDAGRunStart = "dag.run_start"
	// MsgDAGRunComplete DAG 整体运行完成。
	MsgDAGRunComplete = "dag.run_complete"

	// MsgTaskAssign Task 被分配给 Agent。
	MsgTaskAssign = "task.assign"
	// MsgTaskProgress Task 进度更新。
	MsgTaskProgress = "task.progress"
	// MsgTaskComplete Task 完成。
	MsgTaskComplete = "task.complete"
	// MsgTaskFail Task 失败。
	MsgTaskFail = "task.fail"

	// MsgCommandCardExec 命令卡触发执行。
	MsgCommandCardExec = "command_card.exec"
	// MsgCommandCardResult 命令卡执行结果。
	MsgCommandCardResult = "command_card.result"

	// MsgPromptUpdate 提示词模板更新。
	MsgPromptUpdate = "prompt.update"
	// MsgPromptApply 提示词模板应用到 Agent。
	MsgPromptApply = "prompt.apply"

	// MsgSkillLoaded Skill 加载完成。
	MsgSkillLoaded = "skill.loaded"
	// MsgSkillExec Skill 执行。
	MsgSkillExec = "skill.exec"
	// MsgSkillResult Skill 执行结果。
	MsgSkillResult = "skill.result"

	// MsgLSPDiagnostic LSP 诊断发布。
	MsgLSPDiagnostic = "lsp.diagnostic"
	// MsgLSPFileChange LSP 文件变更通知。
	MsgLSPFileChange = "lsp.file_change"
	// MsgLSPCodeAction LSP 代码动作建议。
	MsgLSPCodeAction = "lsp.code_action"

	// --- 审批流 ---

	// MsgApprovalRequest 请求人工审批。
	MsgApprovalRequest = "approval.request"
	// MsgApprovalGranted 审批通过。
	MsgApprovalGranted = "approval.granted"
	// MsgApprovalDenied 审批拒绝。
	MsgApprovalDenied = "approval.denied"
	// MsgApprovalTimeout 审批超时。
	MsgApprovalTimeout = "approval.timeout"

	// --- 资源锁 ---

	// MsgLockAcquire 请求资源锁。
	MsgLockAcquire = "lock.acquire"
	// MsgLockRelease 释放资源锁。
	MsgLockRelease = "lock.release"
	// MsgLockConflict 锁冲突通知。
	MsgLockConflict = "lock.conflict"

	// --- 心跳/健康 ---

	// MsgHeartbeat Agent 心跳。
	MsgHeartbeat = "heartbeat.ping"
	// MsgHeartbeatTimeout Agent 心跳超时 (疑似死亡)。
	MsgHeartbeatTimeout = "heartbeat.timeout"
	// MsgHeartbeatRecover Agent 恢复。
	MsgHeartbeatRecover = "heartbeat.recover"

	// --- Token/成本预算 ---

	// MsgBudgetUpdate Token 用量更新。
	MsgBudgetUpdate = "budget.update"
	// MsgBudgetWarning 预算警告 (接近上限)。
	MsgBudgetWarning = "budget.warning"
	// MsgBudgetExhausted 预算耗尽。
	MsgBudgetExhausted = "budget.exhausted"

	// --- 回滚协调 ---

	// MsgRollbackRequest 请求回滚。
	MsgRollbackRequest = "rollback.request"
	// MsgRollbackComplete 回滚完成。
	MsgRollbackComplete = "rollback.complete"
	// MsgRollbackCascade 级联回滚 (DAG 节点失败后通知下游)。
	MsgRollbackCascade = "rollback.cascade"

	// --- 调度/优先级 ---

	// MsgScheduleEnqueue 任务入队。
	MsgScheduleEnqueue = "scheduler.enqueue"
	// MsgScheduleDequeue 任务出队 (开始执行)。
	MsgScheduleDequeue = "scheduler.dequeue"
	// MsgSchedulePreempt 优先级抢占。
	MsgSchedulePreempt = "scheduler.preempt"
)

// Topic 模式常量。
const (
	// TopicAgentPrefix Agent 消息前缀: agent.{id}.{subtopic}。
	TopicAgentPrefix = "agent."
	// TopicSystem 系统消息。
	TopicSystem = "system"
	// TopicOrchestration 编排消息。
	TopicOrchestration = "orchestration"

	// TopicDAG DAG 执行事件。
	TopicDAG = "dag"
	// TopicTask Task 生命周期。
	TopicTask = "task"
	// TopicCommandCard 命令卡事件。
	TopicCommandCard = "command_card"
	// TopicPrompt 提示词事件。
	TopicPrompt = "prompt"
	// TopicSkill Skill 事件。
	TopicSkill = "skill"
	// TopicLSP LSP 事件。
	TopicLSP = "lsp"
	// TopicApproval 审批事件。
	TopicApproval = "approval"
	// TopicLock 资源锁事件。
	TopicLock = "lock"
	// TopicHeartbeat 心跳事件。
	TopicHeartbeat = "heartbeat"
	// TopicBudget 预算事件。
	TopicBudget = "budget"
	// TopicRollback 回滚事件。
	TopicRollback = "rollback"
	// TopicScheduler 调度事件。
	TopicScheduler = "scheduler"

	// TopicAll 广播 (所有订阅者收到)。
	TopicAll = "*"
)

// ========================================
// Subscriber
// ========================================

// Subscriber 订阅者。
type Subscriber struct {
	ID     string       // 唯一标识
	Filter string       // topic 前缀过滤 ("agent.a0" / "*" / "system")
	Ch     chan Message // 消息通道
}

// ========================================
// MessageBus — topic pub/sub
// ========================================

// MessageBus 进程内消息总线。
//
// 支持 topic 前缀匹配和广播:
//   - 订阅 "agent.a0" → 收到 agent.a0.output, agent.a0.status 等
//   - 订阅 "*" → 收到所有消息
//   - 发布 agent.a0.output → 匹配 "agent.a0", "agent.", "*" 的订阅者
type MessageBus struct {
	mu          sync.RWMutex
	subscribers map[string]*Subscriber // key = subscriber ID
	seq         int64
	onPublish   func(Message) // 可选: 每条消息的全局回调 (用于桥接 SSE/日志)
}

// NewMessageBus 创建消息总线。
func NewMessageBus() *MessageBus {
	return &MessageBus{
		subscribers: make(map[string]*Subscriber),
	}
}

// SetOnPublish 设置全局发布回调 (用于桥接到 dashboard EventBus / bus_log)。
func (b *MessageBus) SetOnPublish(fn func(Message)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onPublish = fn
}

// Publish 发布消息到匹配的订阅者。
//
// seq 递增和 fan-out 在同一把锁下执行, 保证消息到达顺序与 seq 一致。
func (b *MessageBus) Publish(msg Message) {
	b.mu.Lock()
	b.seq++
	msg.Seq = b.seq
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	onPub := b.onPublish

	// 在同一把锁下完成 fan-out, 保证 seq 顺序
	for _, sub := range b.subscribers {
		if matchTopic(sub.Filter, msg.Topic) {
			select {
			case sub.Ch <- msg:
			default:
				// 通道满, 丢弃 (避免阻塞发布者)
			}
		}
	}
	b.mu.Unlock()

	// 全局回调在锁外执行 (回调可能耗时, 避免持锁太久)
	if onPub != nil {
		onPub(msg)
	}
}

// Subscribe 订阅消息。filter 为 topic 前缀 ("agent.a0" / "*" / "system")。
func (b *MessageBus) Subscribe(id, filter string) *Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub := &Subscriber{
		ID:     id,
		Filter: filter,
		Ch:     make(chan Message, 64),
	}
	b.subscribers[id] = sub
	return sub
}

// Unsubscribe 取消订阅。
func (b *MessageBus) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if sub, ok := b.subscribers[id]; ok {
		close(sub.Ch)
		delete(b.subscribers, id)
	}
}

// SubscriberCount 返回当前订阅者数量。
func (b *MessageBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Seq 返回当前序列号。
func (b *MessageBus) Seq() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.seq
}

// ========================================
// Topic 匹配
// ========================================

// matchTopic 检查 topic 是否匹配 filter。
//
// 规则:
//   - filter "*" 匹配所有 topic
//   - filter "agent.a0" 匹配 "agent.a0", "agent.a0.output", "agent.a0.xxx"
//   - filter "system" 匹配 "system", "system.health"
func matchTopic(filter, topic string) bool {
	if filter == TopicAll {
		return true
	}
	if topic == filter {
		return true
	}
	// 前缀匹配: filter="agent.a0" 匹配 topic="agent.a0.output"
	if len(topic) > len(filter) && topic[:len(filter)] == filter && topic[len(filter)] == '.' {
		return true
	}
	return false
}
