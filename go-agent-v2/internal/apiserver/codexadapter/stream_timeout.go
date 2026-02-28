package codexadapter

import (
	"time"

	runtime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
)

func (a *Adapter) SetStreamReadIdleTimeout(timeout time.Duration) {
	if timeout > 0 {
		runtime.SetStreamReadIdleTimeout(timeout)
	}
}
