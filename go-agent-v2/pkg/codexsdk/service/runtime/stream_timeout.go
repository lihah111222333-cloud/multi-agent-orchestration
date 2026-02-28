package runtime

import "time"
import "github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"

func SetStreamReadIdleTimeout(d time.Duration) {
	codex.SetAppServerReadIdleTimeout(d)
}
