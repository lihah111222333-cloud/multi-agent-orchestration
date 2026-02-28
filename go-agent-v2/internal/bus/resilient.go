// resilient.go — 弹性发布层: 总线优先, 异常时自动降级到 DB 并在恢复后补发。
package bus

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
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

// ResilientPublisher 包装 MessageBus，提供 DB fallback 和后台补发。
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
	util.SafeGo(func() { rp.recoveryLoop(ctx) })
}

// Stop 停止后台恢复。
func (rp *ResilientPublisher) Stop() {
	close(rp.stopCh)
	rp.wg.Wait()
}

// Publish 发布消息（自动降级）。
func (rp *ResilientPublisher) Publish(msg Message) {
	if rp.healthy.Load() {
		if rp.tryPublish(msg) {
			return
		}
		rp.healthy.Store(false)
		logger.Warn("bus: marked unhealthy, switching to DB fallback")
	}

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
			logger.Error("bus: publish panicked", logger.FieldError, r)
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
		logger.Error("bus: fallback save failed", logger.FieldTopic, msg.Topic, logger.FieldError, err)
		return
	}
	logger.Info("bus: message saved to DB fallback", logger.FieldTopic, msg.Topic)
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
		logger.Warn("bus: load pending failed", logger.FieldError, err)
		return
	}
	if len(msgs) == 0 {
		if rp.healthy.CompareAndSwap(false, true) {
			logger.Info("bus: recovered, marked healthy")
		}
		return
	}

	for _, msg := range msgs {
		if !rp.tryPublish(msg) {
			return
		}
		if err := rp.fallback.DeletePending(ctx, msg.Seq); err != nil {
			logger.Error("bus: delete pending failed", logger.FieldSeq, msg.Seq, logger.FieldError, err)
		}
	}

	logger.Info("bus: replayed pending messages", logger.FieldCount, len(msgs))
}

// PublishTo 发布系统事件到指定 topic。
func (rp *ResilientPublisher) PublishTo(topicPrefix, id, msgType string, payload any) {
	rp.publishInternal(topicPrefix, id, "system", msgType, payload)
}

// PublishFrom 发布来自指定 Agent 的事件。
func (rp *ResilientPublisher) PublishFrom(topicPrefix, id, from, msgType string, payload any) {
	rp.publishInternal(topicPrefix, id, from, msgType, payload)
}

// publishInternal 共享发布逻辑。
func (rp *ResilientPublisher) publishInternal(topicPrefix, id, from, msgType string, payload any) {
	topic := topicPrefix + "." + id
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("bus: marshal payload failed", logger.FieldTopic, topic, logger.FieldError, err)
		return
	}
	rp.Publish(Message{
		Topic:   topic,
		From:    from,
		Type:    msgType,
		Payload: data,
	})
}
