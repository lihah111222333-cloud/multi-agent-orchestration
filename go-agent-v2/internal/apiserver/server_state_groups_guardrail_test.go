package apiserver

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestServerStateGroupsAreEmbedded(t *testing.T) {
	t.Parallel()

	serverType := reflect.TypeOf(Server{})
	required := []reflect.Type{
		reflect.TypeOf(connManagerState{}),
		reflect.TypeOf(diagnosticsCacheState{}),
		reflect.TypeOf(codeRunState{}),
		reflect.TypeOf(turnTrackingState{}),
		reflect.TypeOf(uiThrottleState{}),
		reflect.TypeOf(toolCallState{}),
		reflect.TypeOf(sseState{}),
		reflect.TypeOf(notifyHookState{}),
		reflect.TypeOf(runtimeGuardState{}),
		reflect.TypeOf(threadAliasState{}),
		reflect.TypeOf(storeBundle{}),
	}
	for _, groupType := range required {
		field, ok := findTopLevelFieldByType(serverType, groupType)
		if !ok {
			t.Fatalf("Server must embed %s", groupType.Name())
		}
		if !field.Anonymous {
			t.Fatalf("Server field %s must be embedded (anonymous)", field.Name)
		}
	}
}

func TestServerLegacyStateFieldsNotAtTopLevel(t *testing.T) {
	t.Parallel()

	serverType := reflect.TypeOf(Server{})
	topFields := make(map[string]struct{}, serverType.NumField())
	for i := 0; i < serverType.NumField(); i++ {
		field := serverType.Field(i)
		topFields[field.Name] = struct{}{}
	}

	forbidden := []string{
		"mu", "conns", "nextID",
		"pendingMu", "pending", "nextReqID",
		"diagMu", "diagCache",
		"codeRunMu", "activeCodeRuns", "codeRunSeq",
		"agentWorkDirMu", "agentWorkDirs",
		"threadSeq", "fileChangeMu", "fileChangeByThread",
		"orchestrationReportMu", "orchestrationPendingReports", "orchestrationReportTTL",
		"uiThrottleMu", "uiThrottleEntries",
		"toolCallMu", "toolCallCount",
		"sseMu", "sseClients",
		"notifyHookMu", "notifyHook",
		"approvalInFlight", "cleanupOnce",
		"threadAliasMu",
		"dagStore", "cmdStore", "promptStore", "fileStore", "workspaceRunStore", "sysLogStore",
		"agentStatusStore", "auditLogStore", "aiLogStore", "busLogStore", "taskAckStore", "taskTraceStore",
		"bindingStore",
	}
	for _, name := range forbidden {
		if _, exists := topFields[name]; exists {
			t.Fatalf("state field %q must live in an embedded state group, not at Server top-level", name)
		}
	}
}

func TestServerStateGroupShapes(t *testing.T) {
	t.Parallel()

	mustHaveFields(t, reflect.TypeOf(connManagerState{}), []string{
		"mu", "conns", "nextID", "pendingMu", "pending", "nextReqID",
	})
	mustHaveFields(t, reflect.TypeOf(diagnosticsCacheState{}), []string{
		"diagMu", "diagCache",
	})
	mustHaveFields(t, reflect.TypeOf(codeRunState{}), []string{
		"codeRunMu", "activeCodeRuns", "codeRunSeq", "agentWorkDirMu", "agentWorkDirs",
	})
	mustHaveFields(t, reflect.TypeOf(turnTrackingState{}), []string{
		"threadSeq", "fileChangeMu", "fileChangeByThread",
		"orchestrationReportMu", "orchestrationPendingReports", "orchestrationReportTTL",
	})
	mustHaveFields(t, reflect.TypeOf(uiThrottleState{}), []string{
		"uiThrottleMu", "uiThrottleEntries",
	})
	mustHaveFields(t, reflect.TypeOf(toolCallState{}), []string{
		"toolCallMu", "toolCallCount",
	})
	mustHaveFields(t, reflect.TypeOf(sseState{}), []string{
		"clients",
	})
	mustHaveFields(t, reflect.TypeOf(notifyHookState{}), []string{
		"notifyHookMu", "notifyHook",
	})
	mustHaveFields(t, reflect.TypeOf(runtimeGuardState{}), []string{
		"approvalInFlight", "cleanupOnce",
	})
	mustHaveFields(t, reflect.TypeOf(threadAliasState{}), []string{
		"threadAliasMu",
	})
	mustHaveFields(t, reflect.TypeOf(storeBundle{}), []string{
		"dagStore", "cmdStore", "promptStore", "fileStore", "workspaceRunStore", "sysLogStore",
		"agentStatusStore", "auditLogStore", "aiLogStore", "busLogStore", "taskAckStore", "taskTraceStore",
		"bindingStore",
	})
}

func findTopLevelFieldByType(t reflect.Type, target reflect.Type) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Type == target {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func mustHaveFields(t *testing.T, st reflect.Type, required []string) {
	t.Helper()
	for _, name := range required {
		if _, ok := st.FieldByName(name); !ok {
			t.Fatalf("%s must contain field %q", st.Name(), name)
		}
	}
}

func TestToolCallStateIncrementAndGet(t *testing.T) {
	t.Parallel()

	var st toolCallState
	if got := st.increment("lsp.open_file"); got != 1 {
		t.Fatalf("first increment mismatch: got %d want 1", got)
	}
	if got := st.increment("lsp.open_file"); got != 2 {
		t.Fatalf("second increment mismatch: got %d want 2", got)
	}
	if got := st.get("lsp.open_file"); got != 2 {
		t.Fatalf("get mismatch: got %d want 2", got)
	}
	if got := st.increment("   "); got != 0 {
		t.Fatalf("blank name should return 0, got %d", got)
	}
}

func TestTurnTrackingStateNextThreadSeq(t *testing.T) {
	t.Parallel()

	var st turnTrackingState
	if got := st.nextThreadSeq(); got != 1 {
		t.Fatalf("first seq mismatch: got %d want 1", got)
	}
	if got := st.nextThreadSeq(); got != 2 {
		t.Fatalf("second seq mismatch: got %d want 2", got)
	}
}

func TestCodeRunStateAgentWorkDirLifecycle(t *testing.T) {
	t.Parallel()

	var st codeRunState
	st.setAgentWorkDir("agent-1", ".")
	got := st.getAgentWorkDir("agent-1")
	if got == "" {
		t.Fatalf("expected non-empty normalized workdir")
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute workdir, got %q", got)
	}

	st.clearAgentWorkDir("agent-1")
	if got := st.getAgentWorkDir("agent-1"); got != "" {
		t.Fatalf("expected cleared workdir, got %q", got)
	}
}

func TestCodeRunStateRunCancelLifecycle(t *testing.T) {
	t.Parallel()

	var st codeRunState
	cancelled := 0

	key1 := st.registerCodeRunCancel("agent-a", "call", func() { cancelled++ })
	key2 := st.registerCodeRunCancel("agent-a", "call", func() { cancelled++ })
	if key1 == "" || key2 == "" {
		t.Fatalf("expected non-empty run keys, got key1=%q key2=%q", key1, key2)
	}
	if key1 == key2 {
		t.Fatalf("expected unique run keys, both were %q", key1)
	}

	st.unregisterCodeRunCancel("agent-a", key1)
	if got := st.cancelCodeRuns("agent-a"); got != 1 {
		t.Fatalf("cancelCodeRuns mismatch: got %d want 1", got)
	}
	if cancelled != 1 {
		t.Fatalf("cancel callback mismatch: got %d want 1", cancelled)
	}
	if got := st.cancelCodeRuns("agent-a"); got != 0 {
		t.Fatalf("second cancelCodeRuns mismatch: got %d want 0", got)
	}

	st.registerCodeRunCancel("agent-a", "again", func() { cancelled++ })
	st.registerCodeRunCancel("agent-b", "again", func() { cancelled++ })
	if got := st.cancelAllCodeRuns(); got != 2 {
		t.Fatalf("cancelAllCodeRuns mismatch: got %d want 2", got)
	}
	if cancelled != 3 {
		t.Fatalf("cancel callback total mismatch: got %d want 3", cancelled)
	}
}

func TestConnManagerStateConnLifecycle(t *testing.T) {
	t.Parallel()

	var st connManagerState
	if got := st.connectionCount(); got != 0 {
		t.Fatalf("initial connection count mismatch: got %d want 0", got)
	}

	entry := &connEntry{}
	st.addConn("conn-1", entry)
	if got := st.connectionCount(); got != 1 {
		t.Fatalf("connection count mismatch: got %d want 1", got)
	}
	if got := st.firstConnID(); got == "" {
		t.Fatalf("expected non-empty first conn id")
	}
	if got, ok := st.getConn("conn-1"); !ok || got != entry {
		t.Fatalf("getConn mismatch: ok=%v got=%p want=%p", ok, got, entry)
	}
	snapshot := st.connsSnapshot()
	if got := len(snapshot); got != 1 {
		t.Fatalf("snapshot size mismatch: got %d want 1", got)
	}
	if snapshot["conn-1"] != entry {
		t.Fatalf("snapshot entry mismatch")
	}
	if removed, ok := st.removeConn("conn-1"); !ok || removed != entry {
		t.Fatalf("removeConn mismatch: ok=%v removed=%p want=%p", ok, removed, entry)
	}
	if got := st.connectionCount(); got != 0 {
		t.Fatalf("post-remove connection count mismatch: got %d want 0", got)
	}
}

func TestConnManagerStatePendingLifecycle(t *testing.T) {
	t.Parallel()

	var st connManagerState
	reqID, ch, cleanup := st.allocPendingRequest()
	if reqID != 1 {
		t.Fatalf("first pending req id mismatch: got %d want 1", reqID)
	}

	resp := &Response{ID: reqID}
	found, delivered := st.deliverPendingResponse(reqID, resp)
	if !found || !delivered {
		t.Fatalf("deliver mismatch: found=%v delivered=%v", found, delivered)
	}

	select {
	case got := <-ch:
		if got != resp {
			t.Fatalf("response pointer mismatch: got %p want %p", got, resp)
		}
	default:
		t.Fatalf("expected pending response in channel")
	}

	cleanup()
	if found, delivered := st.deliverPendingResponse(reqID, resp); found || delivered {
		t.Fatalf("expected missing pending entry after cleanup, got found=%v delivered=%v", found, delivered)
	}
}

func TestCodeRunStateClearAllAgentWorkDirs(t *testing.T) {
	t.Parallel()

	var st codeRunState
	st.setAgentWorkDir("agent-a", ".")
	st.setAgentWorkDir("agent-b", ".")
	st.clearAllAgentWorkDirs()
	if got := st.getAgentWorkDir("agent-a"); got != "" {
		t.Fatalf("expected cleared workdir for agent-a, got %q", got)
	}
	if got := st.getAgentWorkDir("agent-b"); got != "" {
		t.Fatalf("expected cleared workdir for agent-b, got %q", got)
	}
}

func TestSSEStateClientLifecycle(t *testing.T) {
	t.Parallel()

	var st sseState
	ch1 := make(chan []byte, 1)
	ch2 := make(chan []byte, 1)

	st.clients.add(ch1)
	st.clients.add(ch2)
	if got := st.clients.count(); got != 2 {
		t.Fatalf("clientCount mismatch: got %d want 2", got)
	}
	snapshot := st.clients.snapshot()
	if got := len(snapshot); got != 2 {
		t.Fatalf("snapshot size mismatch: got %d want 2", got)
	}

	st.clients.remove(ch1)
	if got := st.clients.count(); got != 1 {
		t.Fatalf("post-remove clientCount mismatch: got %d want 1", got)
	}
	st.clients.remove(ch2)
	if got := st.clients.count(); got != 0 {
		t.Fatalf("final clientCount mismatch: got %d want 0", got)
	}
}

func TestNotifyHookStateSetAndHasHook(t *testing.T) {
	t.Parallel()

	var st notifyHookState
	if st.hasHook() {
		t.Fatalf("hasHook should be false by default")
	}

	called := 0
	st.setHook(func(method string, params any) {
		called++
	})
	if !st.hasHook() {
		t.Fatalf("hasHook should be true after setHook")
	}
	h := st.hook()
	if h == nil {
		t.Fatalf("hook should not be nil after setHook")
	}
	h("m", nil)
	if called != 1 {
		t.Fatalf("hook call mismatch: got %d want 1", called)
	}

	st.setHook(nil)
	if st.hasHook() {
		t.Fatalf("hasHook should be false after clearing hook")
	}
}

func TestConnManagerStateAllocConnID(t *testing.T) {
	t.Parallel()

	var st connManagerState
	if got := st.allocConnID(); got != "conn-1" {
		t.Fatalf("first conn id mismatch: got %q want conn-1", got)
	}
	if got := st.allocConnID(); got != "conn-2" {
		t.Fatalf("second conn id mismatch: got %q want conn-2", got)
	}
}

func TestRuntimeGuardStateApprovalDedupAndRelease(t *testing.T) {
	t.Parallel()

	var st runtimeGuardState
	key := "agent:method:call"
	if ok := st.tryBeginApproval(key); !ok {
		t.Fatalf("first tryBeginApproval should succeed")
	}
	if ok := st.tryBeginApproval(key); ok {
		t.Fatalf("second tryBeginApproval should be deduped")
	}
	st.endApproval(key)
	if ok := st.tryBeginApproval(key); !ok {
		t.Fatalf("tryBeginApproval should succeed after endApproval")
	}
}

func TestRuntimeGuardStateDoCleanupOnce(t *testing.T) {
	t.Parallel()

	var st runtimeGuardState
	calls := 0
	st.doCleanup(func() { calls++ })
	st.doCleanup(func() { calls++ })
	if calls != 1 {
		t.Fatalf("doCleanup should run once: got %d want 1", calls)
	}
}

func TestTurnTrackingStateReportRequesterLifecycle(t *testing.T) {
	t.Parallel()

	var st turnTrackingState
	now := time.Now()
	if got := st.rememberReportRequester("worker-a", "req-b", now); got != 1 {
		t.Fatalf("first waiter count mismatch: got %d want 1", got)
	}
	if got := st.rememberReportRequester("worker-a", "req-a", now); got != 2 {
		t.Fatalf("second waiter count mismatch: got %d want 2", got)
	}

	requesters := st.takeReportRequesters("worker-a", now)
	sort.Strings(requesters)
	if len(requesters) != 2 || requesters[0] != "req-a" || requesters[1] != "req-b" {
		t.Fatalf("requesters mismatch: %#v", requesters)
	}
	if got := st.takeReportRequesters("worker-a", now); len(got) != 0 {
		t.Fatalf("expected drained waiters, got %#v", got)
	}
}

func TestTurnTrackingStateFileChangeLifecycle(t *testing.T) {
	t.Parallel()

	var st turnTrackingState
	st.rememberFileChanges("thread-a", []string{"a.txt", "b.txt"})
	files := st.consumeFileChanges("thread-a")
	if len(files) != 2 || files[0] != "a.txt" || files[1] != "b.txt" {
		t.Fatalf("file changes mismatch: %#v", files)
	}
	if got := st.consumeFileChanges("thread-a"); len(got) != 0 {
		t.Fatalf("expected drained file changes, got %#v", got)
	}
}

func TestUIThrottleStateStageAndFlush(t *testing.T) {
	t.Parallel()

	var st uiThrottleState
	now := time.Now()
	first := map[string]any{"seq": 1}
	pending, emitNow := st.stageUIStateChanged("_global", first, now, 250*time.Millisecond, nil)
	if !emitNow || pending["seq"] != 1 {
		t.Fatalf("first stage mismatch: emit=%v pending=%#v", emitNow, pending)
	}

	second := map[string]any{"seq": 2}
	pending, emitNow = st.stageUIStateChanged("_global", second, now, time.Hour, nil)
	if emitNow || pending != nil {
		t.Fatalf("second stage should be throttled: emit=%v pending=%#v", emitNow, pending)
	}

	flushed, ok := st.flushUIStateChanged("_global", now.Add(time.Second))
	if !ok || flushed["seq"] != 2 {
		t.Fatalf("flush mismatch: ok=%v payload=%#v", ok, flushed)
	}
	if flushed, ok := st.flushUIStateChanged("_global", now.Add(2*time.Second)); ok || flushed != nil {
		t.Fatalf("second flush should be empty: ok=%v payload=%#v", ok, flushed)
	}
}

func TestThreadAliasStateWithLock(t *testing.T) {
	t.Parallel()

	var st threadAliasState
	calls := 0
	st.withLock(func() { calls++ })
	if calls != 1 {
		t.Fatalf("withLock mismatch: got %d want 1", calls)
	}

	var nilState *threadAliasState
	nilState.withLock(func() { calls++ })
	if calls != 2 {
		t.Fatalf("nil withLock mismatch: got %d want 2", calls)
	}
}
