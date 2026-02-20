package apiserver

import "testing"

func TestOffline52MethodList_CountAndUnique(t *testing.T) {
	list := offline52MethodList()
	if len(list) != 52 {
		t.Fatalf("offline52 len=%d, want 52", len(list))
	}
	seen := map[string]struct{}{}
	for _, method := range list {
		if _, ok := seen[method]; ok {
			t.Fatalf("duplicate method: %s", method)
		}
		seen[method] = struct{}{}
	}
}
