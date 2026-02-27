package codexadapter

import (
	"context"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
)

type fakeRecoverClient struct {
	fakeClient
	recoverReason string
	recoverErr    error
}

func (c *fakeRecoverClient) RecoverConnection(reason string) error {
	c.recoverReason = reason
	return c.recoverErr
}

func TestRecoverConnection_UnsupportedClient(t *testing.T) {
	a := &Adapter{}
	err := a.RecoverConnection(codexsdk.NewAgentProcess(&fakeClient{}), "manual")
	if err == nil || !strings.Contains(err.Error(), "does not support connection recovery") {
		t.Fatalf("RecoverConnection() err = %v, want unsupported", err)
	}
}

func TestRecoverConnection_Success(t *testing.T) {
	client := &fakeRecoverClient{}
	a := &Adapter{}
	err := a.RecoverConnection(codexsdk.NewAgentProcess(client), "  manual_recover  ")
	if err != nil {
		t.Fatalf("RecoverConnection() err = %v", err)
	}
	if client.recoverReason != "manual_recover" {
		t.Fatalf("RecoverConnection() reason = %q, want manual_recover", client.recoverReason)
	}
}

func TestThreadRecover_Success(t *testing.T) {
	clients := make(map[string]*fakeRecoverClient)
	factory := func(port int, agentID string) codexsdk.Client {
		client := &fakeRecoverClient{
			fakeClient: fakeClient{
				port:     port,
				threadID: agentID,
				running:  true,
			},
		}
		clients[agentID] = client
		return client
	}
	manager, err := codexsdk.NewAgentManager(factory, factory)
	if err != nil {
		t.Fatalf("NewAgentManager() err = %v", err)
	}
	if err := manager.Launch(context.Background(), "thread-1", "thread-1", "", ".", "", nil); err != nil {
		t.Fatalf("Launch() err = %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop("thread-1") })

	a := &Adapter{ctx: testDeps(manager)}
	result, err := a.ThreadRecover(context.Background(), "thread-1")
	if err != nil {
		t.Fatalf("ThreadRecover() err = %v", err)
	}
	if !result.Recovered || result.Mode != "respawn" || result.Status != "recovering" {
		t.Fatalf("ThreadRecover() result = %+v", result)
	}
	if got := clients["thread-1"].recoverReason; got != "manual_ui_recover" {
		t.Fatalf("client recover reason = %q, want manual_ui_recover", got)
	}
}
