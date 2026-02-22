package apiserver

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/executor"
)

func TestServerCleanupRuntimeResources_RemovesCodeRunnerTempRoot(t *testing.T) {
	cr, err := executor.NewCodeRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tempRoot := reflect.ValueOf(cr).Elem().FieldByName("tempRoot").String()
	if tempRoot == "" {
		t.Fatal("expected non-empty tempRoot")
	}
	if _, err := os.Stat(tempRoot); err != nil {
		t.Fatalf("expected tempRoot to exist before cleanup: %v", err)
	}

	s := &Server{codeRunner: cr}
	s.cleanupRuntimeResources()

	if _, err := os.Stat(tempRoot); !os.IsNotExist(err) {
		t.Fatalf("expected tempRoot to be removed, stat err=%v", err)
	}
}

func TestListenAndServe_StartFailure_CleansRuntimeResources(t *testing.T) {
	cr, err := executor.NewCodeRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tempRoot := reflect.ValueOf(cr).Elem().FieldByName("tempRoot").String()
	if _, err := os.Stat(tempRoot); err != nil {
		t.Fatalf("expected tempRoot to exist before listen: %v", err)
	}

	s := &Server{codeRunner: cr}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 缺少端口的地址会导致 ListenAndServe 直接失败返回。
	listenErr := s.ListenAndServe(ctx, "127.0.0.1")
	if listenErr == nil {
		t.Fatal("expected listen failure")
	}
	if !strings.Contains(listenErr.Error(), "listen") {
		t.Fatalf("expected wrapped listen error, got: %v", listenErr)
	}
	if _, err := os.Stat(tempRoot); !os.IsNotExist(err) {
		t.Fatalf("expected tempRoot removed on listen failure, stat err=%v", err)
	}
}
