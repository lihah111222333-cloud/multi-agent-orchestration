package apiserver

import (
	"context"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/config"
)

func TestOffline52_DefaultDisabled_ReturnsMethodNotFound(t *testing.T) {
	srv := New(Deps{Config: &config.Config{DisableOffline52Methods: true}})
	resp := srv.dispatchRequest(context.Background(), 1, "thread/resume", nil)
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("want method not found, got %+v", resp)
	}
}

func TestOffline52_RollbackSwitch_ReEnableMethods(t *testing.T) {
	srv := New(Deps{Config: &config.Config{DisableOffline52Methods: false}})
	if _, ok := srv.methods["thread/resume"]; !ok {
		t.Fatal("thread/resume should be registered when switch is off")
	}
}
