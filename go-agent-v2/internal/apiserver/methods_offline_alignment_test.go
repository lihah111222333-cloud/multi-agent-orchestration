package apiserver

import (
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/config"
)

func TestRegisterOfflineMethodsRespectConfigGate(t *testing.T) {
	t.Helper()

	offlineMethods := offline52MethodList()
	if len(offlineMethods) == 0 {
		t.Fatalf("offline52MethodList is empty")
	}
	seen := make(map[string]struct{}, len(offlineMethods))
	for _, method := range offlineMethods {
		method = strings.TrimSpace(method)
		if method == "" {
			t.Fatalf("offline52MethodList contains empty method")
		}
		if _, dup := seen[method]; dup {
			t.Fatalf("offline52MethodList contains duplicate method: %s", method)
		}
		seen[method] = struct{}{}
	}

	enabled := &Server{methods: make(map[string]Handler), cfg: &config.Config{DisableOffline52Methods: false}}
	enabled.registerMethods()
	disabled := &Server{methods: make(map[string]Handler), cfg: &config.Config{DisableOffline52Methods: true}}
	disabled.registerMethods()

	for _, method := range offlineMethods {
		if _, ok := enabled.methods[method]; !ok {
			t.Fatalf("offline-gated method missing in enabled mode: %s", method)
		}
		if _, ok := disabled.methods[method]; ok {
			t.Fatalf("offline-gated method should be absent in disabled mode: %s", method)
		}
	}
}
