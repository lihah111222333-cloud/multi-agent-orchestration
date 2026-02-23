package runner

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
)

func TestSetClientFactoriesOverridesFactories(t *testing.T) {
	m := NewAgentManager()

	appCalls := 0
	restCalls := 0
	appFactory := agentcore.ClientFactory(func(port int, agentID string) agentcore.Client {
		appCalls++
		return nil
	})
	restFactory := agentcore.ClientFactory(func(port int, agentID string) agentcore.Client {
		restCalls++
		return nil
	})

	m.SetClientFactories(appFactory, restFactory)
	_ = m.appServerFactory(20001, "agent-app")
	_ = m.restFactory(20002, "agent-rest")

	if appCalls != 1 {
		t.Fatalf("app factory call count = %d, want 1", appCalls)
	}
	if restCalls != 1 {
		t.Fatalf("rest factory call count = %d, want 1", restCalls)
	}
}
