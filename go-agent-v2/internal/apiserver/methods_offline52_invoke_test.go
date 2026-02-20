package apiserver

import (
	"context"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/config"
)

func TestOffline52_Invoke_ReturnMethodNotFound(t *testing.T) {
	srv := New(Deps{Config: &config.Config{DisableOffline52Methods: true}})
	for _, method := range offline52MethodList() {
		resp := srv.dispatchRequest(context.Background(), 1, method, nil)
		if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
			t.Fatalf("method=%s want CodeMethodNotFound got %+v", method, resp)
		}
	}
}
