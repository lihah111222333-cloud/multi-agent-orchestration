// resilient.go — 弹性发布层: 总线优先 + DB 降级。
//
// 设计目标: 所有 13 种能力统一走总线, 但总线异常时自动降级到 DB 落盘, 恢复后补发。
//
// 能力清单: Agent/DAG/Task/命令卡/提示词/Skill/LSP/审批/资源锁/心跳/预算/回滚/调度
//
// 降级策略:
//
//	正常: Publish → MessageBus → 实时 fan-out → 订阅者
//	异常: Publish → DB bus_pending 表 → 后台轮询 → 补发到 MessageBus
package bus

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// FallbackStore 降级存储接口 (由 store 层实现)。
type FallbackStore interface {
	// SavePending 保存未投递消息到 DB。
	SavePending(ctx context.Context, msg Message) error
	// LoadPending 加载所有待补发消息 (按 seq 排序)。
	LoadPending(ctx context.Context, limit int) ([]Message, error)
	// DeletePending 删除已补发的消息。
	DeletePending(ctx context.Context, seq int64) error
}

// ResilientPublisher 弹性发布器。
//
// 包装 MessageBus, 添加降级保障:
//   - 总线健康: 直接 Publish, 零开销
//   - 总线异常: 写入 DB bus_pending 表
//   - 后台协程: 定期扫描 pending, 恢复后补发
type ResilientPublisher struct {
	bus      *MessageBus
	fallback FallbackStore
	healthy  atomic.Bool // 总线是否健康
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewResilientPublisher 创建弹性发布器。
func NewResilientPublisher(bus *MessageBus, fallback FallbackStore) *ResilientPublisher {
	rp := &ResilientPublisher{
		bus:      bus,
		fallback: fallback,
		stopCh:   make(chan struct{}),
	}
	rp.healthy.Store(true)
	return rp
}

// Start 启动后台恢复协程。
func (rp *ResilientPublisher) Start(ctx context.Context) {
	rp.wg.Add(1)
	go rp.recoveryLoop(ctx)
}

// Stop 停止后台恢复。
func (rp *ResilientPublisher) Stop() {
	close(rp.stopCh)
	rp.wg.Wait()
}

// Publish 发布消息 (自动降级)。
//
// 正常: 直接走 MessageBus (零分配, 无 DB 开销)
// 异常: 写入 FallbackStore, 后台补发
func (rp *ResilientPublisher) Publish(msg Message) {
	if rp.healthy.Load() {
		// 尝试直接发布
		if rp.tryPublish(msg) {
			return
		}
		// 发布失败, 标记不健康
		rp.healthy.Store(false)
		slog.Warn("bus: marked unhealthy, switching to DB fallback")
	}

	// 降级: 写入 DB
	rp.saveToDB(msg)
}

// SetHealthy 手动恢复总线状态 (诊断/测试用)。
func (rp *ResilientPublisher) SetHealthy(healthy bool) {
	rp.healthy.Store(healthy)
}

// Healthy 返回总线是否健康。
func (rp *ResilientPublisher) Healthy() bool {
	return rp.healthy.Load()
}

// Bus 返回底层 MessageBus (用于直接订阅)。
func (rp *ResilientPublisher) Bus() *MessageBus {
	return rp.bus
}

// tryPublish 尝试发布, 捕获 panic。
func (rp *ResilientPublisher) tryPublish(msg Message) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			slog.Error("bus: publish panicked", "error", r)
		}
	}()
	rp.bus.Publish(msg)
	return true
}

// saveToDB 降级写入 DB。
func (rp *ResilientPublisher) saveToDB(msg Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	if err := rp.fallback.SavePending(ctx, msg); err != nil {
		slog.Error("bus: fallback save failed", "topic", msg.Topic, "error", err)
		return
	}
	slog.Info("bus: message saved to DB fallback", "topic", msg.Topic)
}

// recoveryLoop 后台恢复: 定期扫描 pending 消息, 恢复后补发。
func (rp *ResilientPublisher) recoveryLoop(ctx context.Context) {
	defer rp.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rp.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			rp.recoverPending(ctx)
		}
	}
}

// recoverPending 补发 pending 消息。
func (rp *ResilientPublisher) recoverPending(ctx context.Context) {
	msgs, err := rp.fallback.LoadPending(ctx, 100)
	if err != nil {
		return
	}
	if len(msgs) == 0 {
		// 无 pending 消息, 恢复健康
		if !rp.healthy.Load() {
			rp.healthy.Store(true)
			slog.Info("bus: recovered, marked healthy")
		}
		return
	}

	// 尝试补发
	for _, msg := range msgs {
		if !rp.tryPublish(msg) {
			// 总线还没恢复, 等下一轮
			return
		}
		// 补发成功, 删除 pending
		if err := rp.fallback.DeletePending(ctx, msg.Seq); err != nil {
			slog.Error("bus: delete pending failed", "seq", msg.Seq, "error", err)
		}
	}

	slog.Info("bus: replayed pending messages", "count", len(msgs))
}

// ========================================
// 通用发布方法 (替代 12 个重复的 Publish* 方法)
// ========================================

// PublishTo 发布系统事件到指定 topic。
//
// topicPrefix 使用 bus.go 中的 Topic 常量 (TopicDAG, TopicTask, ...)。
// id 为资源标识 (taskID, runID, agentID 等)。
//
// 示例:
//
//	rp.PublishTo(TopicDAG, "run-1", MsgDAGNodeStart, nodePayload)
//	rp.PublishTo(TopicTask, "task-42", MsgTaskComplete, resultPayload)
//	rp.PublishTo(TopicLock, "file:main.go", MsgLockAcquire, lockPayload)
func (rp *ResilientPublisher) PublishTo(topicPrefix, id, msgType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("bus: marshal payload failed", "topic", topicPrefix+"."+id, "error", err)
		return
	}
	rp.Publish(Message{
		Topic:   topicPrefix + "." + id,
		From:    "system",
		Type:    msgType,
		Payload: data,
	})
}

// PublishFrom 发布来自指定 Agent 的事件。
//
// 用于需要标识来源 Agent 的场景 (审批请求、心跳等)。
//
// 示例:
//
//	rp.PublishFrom(TopicApproval, "req-1", agentID, MsgApprovalRequest, reqPayload)
//	rp.PublishFrom(TopicHeartbeat, agentID, agentID, MsgHeartbeat, nil)
func (rp *ResilientPublisher) PublishFrom(topicPrefix, id, from, msgType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("bus: marshal payload failed", "topic", topicPrefix+"."+id, "error", err)
		return
	}
	rp.Publish(Message{
		Topic:   topicPrefix + "." + id,
		From:    from,
		Type:    msgType,
		Payload: data,
	})
}
