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

type FallbackStore interface {
	SavePending(ctx context.Context, msg Message) error
	LoadPending(ctx context.Context, limit int) ([]Message, error)
	DeletePending(ctx context.Context, seq int64) error
}

type ResilientPublisher struct {
	bus      *MessageBus
	fallback FallbackStore
	healthy  atomic.Bool
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewResilientPublisher(bus *MessageBus, fallback FallbackStore) *ResilientPublisher {
	rp := &ResilientPublisher{
		bus:      bus,
		fallback: fallback,
		stopCh:   make(chan struct{}),
	}
	rp.healthy.Store(true)
	return rp
}

func (rp *ResilientPublisher) Start(ctx context.Context) {
	rp.wg.Add(1)
	util.SafeGo(func() { rp.recoveryLoop(ctx) })
}

func (rp *ResilientPublisher) Stop() { rp.stopOnce.Do(func() { close(rp.stopCh) }); rp.wg.Wait() }

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

func (rp *ResilientPublisher) SetHealthy(healthy bool) { rp.healthy.Store(healthy) }
func (rp *ResilientPublisher) Healthy() bool           { return rp.healthy.Load() }
func (rp *ResilientPublisher) Bus() *MessageBus        { return rp.bus }

func (rp *ResilientPublisher) tryPublish(msg Message) (ok bool) {
	if rp.bus == nil {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			ok = false
			logger.Error("bus: publish panicked", logger.FieldError, r)
		}
	}()
	rp.bus.Publish(msg)
	return true
}

func (rp *ResilientPublisher) saveToDB(msg Message) {
	if rp.fallback == nil {
		logger.Warn("bus: fallback unavailable, dropping message", logger.FieldTopic, msg.Topic)
		return
	}
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

func (rp *ResilientPublisher) recoverPending(ctx context.Context) {
	if rp.fallback == nil {
		return
	}
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

func (rp *ResilientPublisher) PublishTo(topicPrefix, id, msgType string, payload any) {
	rp.publishInternal(topicPrefix, id, "system", msgType, payload)
}
func (rp *ResilientPublisher) PublishFrom(topicPrefix, id, from, msgType string, payload any) {
	rp.publishInternal(topicPrefix, id, from, msgType, payload)
}

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
