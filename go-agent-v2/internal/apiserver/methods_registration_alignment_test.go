package apiserver

import "testing"

func TestRustV2MethodAlignmentRegistrations(t *testing.T) {
	s := &Server{methods: make(map[string]Handler)}
	s.registerMethods()

	required := []string{
		"externalAgentConfig/detect",
		"externalAgentConfig/import",
		"thread/realtime/start",
		"thread/realtime/appendAudio",
		"thread/realtime/appendText",
		"thread/realtime/stop",
		"windowsSandbox/setupStart",
		"mock/experimentalMethod",
		"skills/remote/list",
		"skills/remote/export",
	}
	for _, method := range required {
		if _, ok := s.methods[method]; !ok {
			t.Fatalf("missing rust-v2 aligned method: %s", method)
		}
	}
}
