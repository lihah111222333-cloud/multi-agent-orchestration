package runner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

type fakeClient struct {
	port     int
	threadID string
	spawnErr error
	running  bool

	handler            agentcore.EventHandler
	setEventCalls      int
	spawnAndConnectHit int
	killHit            int
	shutdownHit        int
}

func (f *fakeClient) GetPort() int { return f.port }

func (f *fakeClient) GetThreadID() string { return f.threadID }

func (f *fakeClient) SetEventHandler(h agentcore.EventHandler) {
	f.handler = h
	f.setEventCalls++
}

func (f *fakeClient) SpawnAndConnect(_ context.Context, _ string, _ string, _ string, _ string, _ []agentcore.DynamicTool) error {
	f.spawnAndConnectHit++
	if f.spawnErr != nil {
		return f.spawnErr
	}
	f.running = true
	return nil
}

func (f *fakeClient) Submit(string, []string, []string, json.RawMessage) error { return nil }

func (f *fakeClient) SendCommand(string, string) error { return nil }

func (f *fakeClient) SendDynamicToolResult(string, string, *int64) error { return nil }

func (f *fakeClient) RespondError(int64, int, string) error { return nil }

func (f *fakeClient) ListThreads() ([]agentcore.ThreadInfo, error) { return nil, nil }

func (f *fakeClient) ResumeThread(agentcore.ResumeThreadRequest) error { return nil }

func (f *fakeClient) ForkThread(agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error) {
	return &agentcore.ForkThreadResponse{}, nil
}

func (f *fakeClient) Shutdown() error {
	f.shutdownHit++
	f.running = false
	return nil
}

func (f *fakeClient) Kill() error {
	f.killHit++
	f.running = false
	return nil
}

func (f *fakeClient) Running() bool { return f.running }

func TestNewAgentManagerRejectsNilFactories(t *testing.T) {
	rest := &fakeClient{}
	app := &fakeClient{}
	tests := []struct {
		name        string
		appFactory  agentcore.ClientFactory
		restFactory agentcore.ClientFactory
	}{
		{
			name:       "nil app factory",
			appFactory: nil,
			restFactory: func(int, string) agentcore.Client {
				return rest
			},
		},
		{
			name: "nil rest factory",
			appFactory: func(int, string) agentcore.Client {
				return app
			},
			restFactory: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewAgentManager(tt.appFactory, tt.restFactory)
			if err == nil {
				t.Fatalf("expected constructor error for %s", tt.name)
			}
			if mgr != nil {
				t.Fatalf("expected nil manager for %s", tt.name)
			}
		})
	}
}

func TestSetClientFactoriesOverridesFactories(t *testing.T) {
	baseApp := &fakeClient{}
	baseRest := &fakeClient{}
	m, err := NewAgentManager(
		func(int, string) agentcore.Client { return baseApp },
		func(int, string) agentcore.Client { return baseRest },
	)
	if err != nil {
		t.Fatalf("NewAgentManager() error = %v", err)
	}

	appCalls := 0
	restCalls := 0
	appFactory := agentcore.ClientFactory(func(int, string) agentcore.Client {
		appCalls++
		return nil
	})
	restFactory := agentcore.ClientFactory(func(int, string) agentcore.Client {
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

func TestLaunchFallbackToRESTWhenAppServerSpawnFails(t *testing.T) {
	app := &fakeClient{spawnErr: errors.New("app-server unavailable")}
	rest := &fakeClient{}

	appFactoryCalls := 0
	restFactoryCalls := 0
	mgr, err := NewAgentManager(
		func(int, string) agentcore.Client {
			appFactoryCalls++
			return app
		},
		func(int, string) agentcore.Client {
			restFactoryCalls++
			return rest
		},
	)
	if err != nil {
		t.Fatalf("NewAgentManager() error = %v", err)
	}

	var events []agentcore.Event
	mgr.SetOnEvent(func(_ string, event agentcore.Event) {
		events = append(events, event)
	})

	if launchErr := mgr.Launch(context.Background(), "a-fallback", "fallback", "hello", ".", "", nil); launchErr != nil {
		t.Fatalf("Launch() error = %v", launchErr)
	}

	if appFactoryCalls != 1 {
		t.Fatalf("app factory calls = %d, want 1", appFactoryCalls)
	}
	if restFactoryCalls != 1 {
		t.Fatalf("rest factory calls = %d, want 1", restFactoryCalls)
	}
	if app.spawnAndConnectHit != 1 {
		t.Fatalf("app spawn calls = %d, want 1", app.spawnAndConnectHit)
	}
	if app.killHit != 1 {
		t.Fatalf("app kill calls = %d, want 1", app.killHit)
	}
	if rest.spawnAndConnectHit != 1 {
		t.Fatalf("rest spawn calls = %d, want 1", rest.spawnAndConnectHit)
	}

	proc, getErr := mgr.get("a-fallback")
	if getErr != nil {
		t.Fatalf("manager get failed: %v", getErr)
	}
	if proc.Client != rest {
		t.Fatalf("expected fallback REST client to be active")
	}

	foundBackground := false
	for _, event := range events {
		if event.Type == agentcore.EventBackgroundEvent {
			foundBackground = true
			break
		}
	}
	if !foundBackground {
		t.Fatalf("expected background event for fallback path")
	}
}
