package bus

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type Message struct {
	Topic     string          `json:"topic"`   // agent.a0.output / system.health / orchestration.begin
	From      string          `json:"from"`    // 来源 Agent ID 或 "system"
	To        string          `json:"to"`      // 目标 Agent ID 或 "*" (广播)
	Type      string          `json:"type"`    // 消息类型 (task_delegate / status_update / result / error)
	Payload   json.RawMessage `json:"payload"` // 具体数据
	Timestamp time.Time       `json:"timestamp"`
	Seq       int64           `json:"seq"` // 全局序列号
}

const (
	MsgTaskDelegate  = "task_delegate"
	MsgTaskResult    = "task_result"
	MsgStatusUpdate  = "status_update"
	MsgAgentOutput   = "agent_output"
	MsgAgentEvent    = "agent_event"
	MsgUserMessage   = "user_message"
	MsgError         = "error"
	MsgOrchestration = "orchestration"

	MsgDAGNodeStart    = "dag.node_start"
	MsgDAGNodeComplete = "dag.node_complete"
	MsgDAGNodeFail     = "dag.node_fail"
	MsgDAGRunStart     = "dag.run_start"
	MsgDAGRunComplete  = "dag.run_complete"

	MsgTaskAssign   = "task.assign"
	MsgTaskProgress = "task.progress"
	MsgTaskComplete = "task.complete"
	MsgTaskFail     = "task.fail"

	MsgCommandCardExec   = "command_card.exec"
	MsgCommandCardResult = "command_card.result"

	MsgPromptUpdate = "prompt.update"
	MsgPromptApply  = "prompt.apply"

	MsgSkillLoaded = "skill.loaded"
	MsgSkillExec   = "skill.exec"
	MsgSkillResult = "skill.result"

	MsgLSPDiagnostic = "lsp.diagnostic"
	MsgLSPFileChange = "lsp.file_change"
	MsgLSPCodeAction = "lsp.code_action"

	MsgApprovalRequest = "approval.request"
	MsgApprovalGranted = "approval.granted"
	MsgApprovalDenied  = "approval.denied"
	MsgApprovalTimeout = "approval.timeout"

	MsgLockAcquire  = "lock.acquire"
	MsgLockRelease  = "lock.release"
	MsgLockConflict = "lock.conflict"

	MsgHeartbeat        = "heartbeat.ping"
	MsgHeartbeatTimeout = "heartbeat.timeout"
	MsgHeartbeatRecover = "heartbeat.recover"

	MsgBudgetUpdate    = "budget.update"
	MsgBudgetWarning   = "budget.warning"
	MsgBudgetExhausted = "budget.exhausted"

	MsgRollbackRequest  = "rollback.request"
	MsgRollbackComplete = "rollback.complete"
	MsgRollbackCascade  = "rollback.cascade"

	MsgScheduleEnqueue = "scheduler.enqueue"
	MsgScheduleDequeue = "scheduler.dequeue"
	MsgSchedulePreempt = "scheduler.preempt"
)

const (
	TopicAgentPrefix   = "agent."
	TopicSystem        = "system"
	TopicOrchestration = "orchestration"

	TopicDAG         = "dag"
	TopicTask        = "task"
	TopicCommandCard = "command_card"
	TopicPrompt      = "prompt"
	TopicSkill       = "skill"
	TopicLSP         = "lsp"
	TopicApproval    = "approval"
	TopicLock        = "lock"
	TopicHeartbeat   = "heartbeat"
	TopicBudget      = "budget"
	TopicRollback    = "rollback"
	TopicScheduler   = "scheduler"

	TopicAll = "*"
)

type Subscriber struct {
	ID     string       // 唯一标识
	Filter string       // topic 前缀过滤 ("agent.a0" / "*" / "system")
	Ch     chan Message // 消息通道
}

type MessageBus struct {
	mu          sync.RWMutex
	subscribers map[string]*Subscriber // key = subscriber ID
	seq         atomic.Int64           // 全局序列号 (原子操作, 无需持锁)
	dropped     atomic.Int64           // 累计丢弃消息数 (通道满时递增)
	onPublish   func(Message)          // 可选: 每条消息的全局回调 (用于桥接 SSE/日志)
}

func NewMessageBus() *MessageBus {
	return &MessageBus{
		subscribers: make(map[string]*Subscriber),
	}
}

func (b *MessageBus) SetOnPublish(fn func(Message)) {
	b.mu.Lock()
	b.onPublish = fn
	b.mu.Unlock()
}

func (b *MessageBus) Publish(msg Message) {
	msg.Seq = b.seq.Add(1)
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	b.mu.RLock()
	onPub := b.onPublish

	matched := make([]*Subscriber, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		if matchTopic(sub.Filter, msg.Topic) {
			matched = append(matched, sub)
		}
	}
	b.mu.RUnlock()

	for _, sub := range matched {
		func() {
			defer func() {
				if r := recover(); r != nil {
					b.dropped.Add(1)
				}
			}()
			select {
			case sub.Ch <- msg:
			default:
				b.dropped.Add(1)
				logger.Warn("bus: subscriber channel full, message dropped",
					logger.FieldSubscriber, sub.ID,
					logger.FieldTopic, msg.Topic,
					logger.FieldSeq, msg.Seq,
				)
			}
		}()
	}

	if onPub != nil {
		onPub(msg)
	}
}

func (b *MessageBus) Subscribe(id, filter string) *Subscriber {
	sub := &Subscriber{
		ID:     id,
		Filter: filter,
		Ch:     make(chan Message, 64),
	}
	b.mu.Lock()
	b.subscribers[id] = sub
	b.mu.Unlock()
	return sub
}

func (b *MessageBus) Unsubscribe(id string) {
	b.mu.Lock()
	sub, ok := b.subscribers[id]
	delete(b.subscribers, id)
	b.mu.Unlock()
	if ok && sub != nil {
		close(sub.Ch)
	}
}

func (b *MessageBus) SubscriberCount() int {
	b.mu.RLock()
	n := len(b.subscribers)
	b.mu.RUnlock()
	return n
}

func (b *MessageBus) Seq() int64 {
	return b.seq.Load()
}

func (b *MessageBus) Dropped() int64 {
	return b.dropped.Load()
}

func matchTopic(filter, topic string) bool {
	return filter == TopicAll || topic == filter ||
		strings.HasPrefix(topic, filter+".")
}
