package apiserver

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

// TestHandleApprovalRequest_DeduplicatesConcurrent 验证同一 agentID+method 并发调用被去重:
// 只有第 1 个进入执行, 后续并发调用被跳过。
func TestHandleApprovalRequest_DeduplicatesConcurrent(t *testing.T) {
	s := &Server{
		mgr:     nil, // mgr==nil → 快速走 deny 路径
		conns:   map[string]*connEntry{},
		pending: make(map[int64]chan *Response),
	}

	var execCount atomic.Int64
	event := codex.Event{
		Type: "exec_approval_request",
		DenyFunc: func() error {
			execCount.Add(1)
			return nil
		},
	}

	const concurrency = 5
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			s.handleApprovalRequest("agent-1", "item/commandExecution/requestApproval", nil, event)
		}()
	}
	wg.Wait()

	// 由于 mgr==nil, 函数走 deny+return, 但应只执行一次
	count := execCount.Load()
	if count != 1 {
		t.Fatalf("handleApprovalRequest executed %d times, want 1 (dedup should prevent concurrent calls)", count)
	}
}

// TestHandleApprovalRequest_DifferentMethodsNotDeduplicated 验证不同 method 不冲突。
func TestHandleApprovalRequest_DifferentMethodsNotDeduplicated(t *testing.T) {
	s := &Server{
		mgr:     nil,
		conns:   map[string]*connEntry{},
		pending: make(map[int64]chan *Response),
	}

	var execCount atomic.Int64
	makeEvent := func() codex.Event {
		return codex.Event{
			Type: "exec_approval_request",
			DenyFunc: func() error {
				execCount.Add(1)
				return nil
			},
		}
	}

	s.handleApprovalRequest("agent-1", "item/commandExecution/requestApproval", nil, makeEvent())
	s.handleApprovalRequest("agent-1", "item/fileChange/requestApproval", nil, makeEvent())

	count := execCount.Load()
	if count != 2 {
		t.Fatalf("handleApprovalRequest executed %d times, want 2 (different methods should not dedup)", count)
	}
}
