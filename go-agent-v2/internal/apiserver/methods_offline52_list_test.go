package apiserver

import "testing"

func TestOffline52MethodList_CountAndUnique(t *testing.T) {
	list := offline52MethodList()
	if len(list) != 51 {
		t.Fatalf("offline52 len=%d, want 51", len(list))
	}
	seen := map[string]struct{}{}
	for _, method := range list {
		if _, ok := seen[method]; ok {
			t.Fatalf("duplicate method: %s", method)
		}
		seen[method] = struct{}{}
	}
	if _, ok := seen["thread/compact/start"]; ok {
		t.Fatalf("thread/compact/start must stay available when offline list is enabled")
	}
}
