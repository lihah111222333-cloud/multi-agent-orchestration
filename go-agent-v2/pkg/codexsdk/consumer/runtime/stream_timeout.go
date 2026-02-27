package runtime

import (
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
)

func SetStreamReadIdleTimeout(timeout time.Duration) {
	codex.SetAppServerReadIdleTimeout(timeout)
}
