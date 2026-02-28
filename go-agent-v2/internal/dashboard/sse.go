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
func (b *EventBus) Subscribe(id string) <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 32)
	b.subscribers[id] = ch
	return ch
}

// Unsubscribe 取消订阅并关闭 channel。
//
// 关闭 ch 确保 sseHandler goroutine 能通过 ok==false 检查退出,
// 避免 goroutine 泄漏。
func (b *EventBus) Unsubscribe(id string) {
	b.mu.Lock()
	ch, ok := b.subscribers[id]
	delete(b.subscribers, id)
	b.mu.Unlock()
	if ok && ch != nil {
		close(ch)
	}
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

	c.Stream(func(io.Writer) bool {
		select {
		case evt, ok := <-ch:
			if !ok {
				return false
			}
			c.SSEvent(evt.Type, evt.Data)
			return true
		case <-time.After(30 * time.Second):
			c.SSEvent("ping", "keepalive")
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
