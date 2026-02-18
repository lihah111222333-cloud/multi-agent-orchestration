package codex

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/protocolsync"
)

func TestProtocolMethodCoverage_FromCodexRs(t *testing.T) {
	catalog, sourcePath, err := protocolsync.LoadDefaultMethodCatalog()
	if err != nil {
		t.Skipf("skip protocol sync check: %v", err)
	}

	t.Logf("protocol source: %s", sourcePath)

	var missing []string
	for _, method := range catalog.SortedAll() {
		if _, ok := mapMethodToEventType(method); !ok {
			missing = append(missing, method)
		}
	}

	if len(missing) > 0 {
		t.Fatalf("missing method->event mappings (%d): %v", len(missing), missing)
	}
}
