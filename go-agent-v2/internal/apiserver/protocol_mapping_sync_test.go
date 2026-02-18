package apiserver

import (
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/protocolsync"
)

func TestEventMethodMap_TargetMethodsKnownByProtocol(t *testing.T) {
	catalog, sourcePath, err := protocolsync.LoadDefaultMethodCatalog()
	if err != nil {
		t.Skipf("skip protocol sync check: %v", err)
	}
	t.Logf("protocol source: %s", sourcePath)

	knownMethods := catalog.All()

	var invalid []string
	for eventType, method := range eventMethodMap {
		if strings.HasPrefix(method, "codex/event/") || strings.HasPrefix(method, "agent/event/") {
			continue
		}
		if _, ok := knownMethods[method]; !ok {
			invalid = append(invalid, eventType+"->"+method)
		}
	}

	if len(invalid) > 0 {
		t.Fatalf("event->method contains targets not in codex-rs protocol (%d): %v", len(invalid), invalid)
	}
}
