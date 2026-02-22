// sse.go — SSE 事件总线 + handler。
package dashboard

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// EventBus 事件总线 (SSE 推送)。
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]chan Event
}

// Event SSE 事件。
type Event struct {
	Type string
	Data any
}

// NewEventBus 创建事件总线。
func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[string]chan Event)}
}

// Publish 广播事件。
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

// PublishAgentStatus 实现 monitor.EventPublisher 接口。
func (b *EventBus) PublishAgentStatus(snapshot map[string]any) {
	b.Publish(Event{Type: "agent_status", Data: snapshot})
}

// Subscribe 订阅。
func (b *EventBus) Subscribe(id string) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 32)
	b.subscribers[id] = ch
	return ch
}

// Unsubscribe 取消订阅。
//
// 不关闭 ch — sseHandler 通过 ctx.Done() 退出, GC 回收未引用的 channel。
func (b *EventBus) Unsubscribe(id string) {
	b.mu.Lock()
	delete(b.subscribers, id)
	b.mu.Unlock()
}

// sseHandler Gin SSE handler。
func (s *Server) sseHandler(c *gin.Context) {
	clientID := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	ch := s.bus.Subscribe(clientID)
	defer func() {
		s.bus.Unsubscribe(clientID)
		logger.Info("dashboard: SSE client disconnected", "client_id", clientID)
	}()

	logger.Info("dashboard: SSE client connected", "client_id", clientID)

	c.Stream(func(w io.Writer) bool {
		// 复用 timer 避免每次循环创建新定时器 (GC 压力)
		keepalive := time.NewTimer(30 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return false
				}
				c.SSEvent(evt.Type, evt.Data)
				if !keepalive.Stop() {
					select {
					case <-keepalive.C:
					default:
					}
				}
				keepalive.Reset(30 * time.Second)
				return true
			case <-keepalive.C:
				c.SSEvent("ping", "keepalive")
				keepalive.Reset(30 * time.Second)
				return true
			case <-c.Request.Context().Done():
				return false
			}
		}
	})
}
