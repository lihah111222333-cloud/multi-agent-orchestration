package codexadapter

import (
	"time"

	runtimesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
)

// SetStreamReadIdleTimeout updates app-server stream read-idle timeout.
func (a *Adapter) SetStreamReadIdleTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	runtimesvc.SetStreamReadIdleTimeout(timeout)
}
