package codexadapter

import (
	"time"

	consumerruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/runtime"
)

// SetStreamReadIdleTimeout updates app-server stream read-idle timeout.
func (a *Adapter) SetStreamReadIdleTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	consumerruntime.SetStreamReadIdleTimeout(timeout)
}
