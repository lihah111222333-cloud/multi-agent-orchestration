package codexadapter

import (
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

// SetStreamReadIdleTimeout updates app-server stream read-idle timeout.
func (a *Adapter) SetStreamReadIdleTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	codex.SetAppServerReadIdleTimeout(timeout)
}
