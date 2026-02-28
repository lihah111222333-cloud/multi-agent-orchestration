package dashboard

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]chan Event
}

type Event struct {
	Type string
	Data any
}

func NewEventBus() *EventBus { return &EventBus{subscribers: make(map[string]chan Event)} }

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

func (b *EventBus) PublishAgentStatus(snapshot map[string]any) {
	b.Publish(Event{Type: "agent_status", Data: snapshot})
}

func (b *EventBus) Subscribe(id string) <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 32)
	b.subscribers[id] = ch
	return ch
}

func (b *EventBus) Unsubscribe(id string) {
	b.mu.Lock()
	ch, ok := b.subscribers[id]
	delete(b.subscribers, id)
	b.mu.Unlock()
	if ok && ch != nil {
		close(ch)
	}
}

func (s *Server) sseHandler(c *gin.Context) {
	clientID := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	ch := s.bus.Subscribe(clientID)
	keepalive := time.NewTicker(30 * time.Second)
	defer logger.Info("dashboard: SSE client disconnected", "client_id", clientID)
	defer s.bus.Unsubscribe(clientID)
	defer keepalive.Stop()

	logger.Info("dashboard: SSE client connected", "client_id", clientID)

	c.Stream(func(io.Writer) bool {
		select {
		case evt, ok := <-ch:
			if !ok {
				return false
			}
			c.SSEvent(evt.Type, evt.Data)
			return true
		case <-keepalive.C:
			c.SSEvent("ping", "keepalive")
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
