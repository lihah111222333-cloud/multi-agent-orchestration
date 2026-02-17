package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/service"
)

func TestWorkspaceRunMethods_CreateListMergeAbort(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	ctx := context.Background()
	if err := database.Migrate(ctx, env.pool, "../../migrations"); err != nil {
		t.Fatalf("migrate DB: %v", err)
	}

	workspaceRoot := t.TempDir()
	workspaceMgr, err := service.NewWorkspaceManager(
		env.srv.workspaceRunStore,
		workspaceRoot,
		1000,
		2<<20,
		32<<20,
	)
	if err != nil {
		t.Fatalf("create workspace manager: %v", err)
	}
	env.srv.workspaceMgr = workspaceMgr

	var (
		notifyMu      sync.Mutex
		workspaceMsgs []string
	)
	env.srv.SetNotifyHook(func(method string, _ any) {
		if !strings.HasPrefix(method, "workspace/run/") {
			return
		}
		notifyMu.Lock()
		workspaceMsgs = append(workspaceMsgs, method)
		notifyMu.Unlock()
	})

	sourceRoot := t.TempDir()
	relFile := filepath.Join("pkg", "a.txt")
	sourceFile := filepath.Join(sourceRoot, relFile)
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(sourceFile, []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	runKey := fmt.Sprintf("run-create-merge-%d", time.Now().UnixMilli())
	createAny, err := env.srv.InvokeMethod(ctx, "workspace/run/create", mustJSONParams(t, map[string]any{
		"runKey":     runKey,
		"sourceRoot": sourceRoot,
		"createdBy":  "test",
		"files":      []string{relFile},
		"metadata":   map[string]any{"case": "create-merge"},
	}))
	if err != nil {
		t.Fatalf("workspace/run/create: %v", err)
	}

	var createResp struct {
		Run struct {
			RunKey        string `json:"run_key"`
			SourceRoot    string `json:"source_root"`
			WorkspacePath string `json:"workspace_path"`
			Status        string `json:"status"`
		} `json:"run"`
	}
	decodeJSONAny(t, createAny, &createResp)
	if createResp.Run.RunKey != runKey {
		t.Fatalf("runKey mismatch: got=%s want=%s", createResp.Run.RunKey, runKey)
	}
	if createResp.Run.Status != service.WorkspaceRunStatusActive {
		t.Fatalf("run status mismatch: got=%s", createResp.Run.Status)
	}

	workspaceFile := filepath.Join(createResp.Run.WorkspacePath, relFile)
	data, err := os.ReadFile(workspaceFile)
	if err != nil {
		t.Fatalf("read workspace bootstrap file: %v", err)
	}
	if string(data) != "v1\n" {
		t.Fatalf("workspace bootstrap content mismatch: %q", string(data))
	}

	listAny, err := env.srv.InvokeMethod(ctx, "workspace/run/list", mustJSONParams(t, map[string]any{
		"status": "active",
		"limit":  20,
	}))
	if err != nil {
		t.Fatalf("workspace/run/list: %v", err)
	}
	var listResp struct {
		Runs []map[string]any `json:"runs"`
	}
	decodeJSONAny(t, listAny, &listResp)
	if len(listResp.Runs) == 0 {
		t.Fatal("workspace/run/list returned empty runs")
	}

	if err := os.WriteFile(workspaceFile, []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	mergeDryAny, err := env.srv.InvokeMethod(ctx, "workspace/run/merge", mustJSONParams(t, map[string]any{
		"runKey":    runKey,
		"updatedBy": "test",
		"dryRun":    true,
	}))
	if err != nil {
		t.Fatalf("workspace/run/merge dryRun: %v", err)
	}
	var mergeDryResp struct {
		Result struct {
			Status string `json:"status"`
			Merged int    `json:"merged"`
			DryRun bool   `json:"dryRun"`
		} `json:"result"`
	}
	decodeJSONAny(t, mergeDryAny, &mergeDryResp)
	if !mergeDryResp.Result.DryRun {
		t.Fatalf("dry run flag mismatch")
	}
	sourceData, _ := os.ReadFile(sourceFile)
	if string(sourceData) != "v1\n" {
		t.Fatalf("source file should not change in dry run: %q", string(sourceData))
	}

	mergeAny, err := env.srv.InvokeMethod(ctx, "workspace/run/merge", mustJSONParams(t, map[string]any{
		"runKey":    runKey,
		"updatedBy": "test",
		"dryRun":    false,
	}))
	if err != nil {
		t.Fatalf("workspace/run/merge: %v", err)
	}
	var mergeResp struct {
		Result struct {
			Status string `json:"status"`
			Merged int    `json:"merged"`
			DryRun bool   `json:"dryRun"`
		} `json:"result"`
	}
	decodeJSONAny(t, mergeAny, &mergeResp)
	if mergeResp.Result.DryRun {
		t.Fatalf("merge should not be dry run")
	}
	if mergeResp.Result.Merged == 0 {
		t.Fatalf("merge expected at least 1 merged file")
	}
	sourceData, _ = os.ReadFile(sourceFile)
	if string(sourceData) != "v2\n" {
		t.Fatalf("source file merge mismatch: %q", string(sourceData))
	}

	getAny, err := env.srv.InvokeMethod(ctx, "workspace/run/get", mustJSONParams(t, map[string]any{
		"runKey": runKey,
	}))
	if err != nil {
		t.Fatalf("workspace/run/get: %v", err)
	}
	var getResp struct {
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
	}
	decodeJSONAny(t, getAny, &getResp)
	if getResp.Run.Status != service.WorkspaceRunStatusMerged {
		t.Fatalf("run status should be merged, got=%s", getResp.Run.Status)
	}

	abortRunKey := fmt.Sprintf("run-abort-%d", time.Now().UnixMilli())
	_, err = env.srv.InvokeMethod(ctx, "workspace/run/create", mustJSONParams(t, map[string]any{
		"runKey":     abortRunKey,
		"sourceRoot": sourceRoot,
		"createdBy":  "test",
		"files":      []string{relFile},
	}))
	if err != nil {
		t.Fatalf("workspace/run/create for abort: %v", err)
	}
	abortAny, err := env.srv.InvokeMethod(ctx, "workspace/run/abort", mustJSONParams(t, map[string]any{
		"runKey":    abortRunKey,
		"updatedBy": "test",
		"reason":    "manual abort in test",
	}))
	if err != nil {
		t.Fatalf("workspace/run/abort: %v", err)
	}
	var abortResp struct {
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
	}
	decodeJSONAny(t, abortAny, &abortResp)
	if abortResp.Run.Status != service.WorkspaceRunStatusAborted {
		t.Fatalf("abort status mismatch: %s", abortResp.Run.Status)
	}

	notifyMu.Lock()
	defer notifyMu.Unlock()
	required := []string{
		"workspace/run/created",
		"workspace/run/merged",
		"workspace/run/aborted",
	}
	for _, event := range required {
		found := false
		for _, got := range workspaceMsgs {
			if got == event {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing workspace notify event %s, got=%v", event, workspaceMsgs)
		}
	}
}

func TestWorkspaceRunMergeConflict(t *testing.T) {
	env := setupDashTestServer(t)
	defer env.pool.Close()

	ctx := context.Background()
	if err := database.Migrate(ctx, env.pool, "../../migrations"); err != nil {
		t.Fatalf("migrate DB: %v", err)
	}

	workspaceMgr, err := service.NewWorkspaceManager(
		env.srv.workspaceRunStore,
		t.TempDir(),
		1000,
		2<<20,
		32<<20,
	)
	if err != nil {
		t.Fatalf("create workspace manager: %v", err)
	}
	env.srv.workspaceMgr = workspaceMgr

	sourceRoot := t.TempDir()
	relFile := "main.txt"
	sourceFile := filepath.Join(sourceRoot, relFile)
	if err := os.WriteFile(sourceFile, []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	runKey := fmt.Sprintf("run-conflict-%d", time.Now().UnixMilli())
	createAny, err := env.srv.InvokeMethod(ctx, "workspace/run/create", mustJSONParams(t, map[string]any{
		"runKey":     runKey,
		"sourceRoot": sourceRoot,
		"createdBy":  "test",
		"files":      []string{relFile},
	}))
	if err != nil {
		t.Fatalf("workspace/run/create: %v", err)
	}
	var createResp struct {
		Run struct {
			WorkspacePath string `json:"workspace_path"`
		} `json:"run"`
	}
	decodeJSONAny(t, createAny, &createResp)
	workspaceFile := filepath.Join(createResp.Run.WorkspacePath, relFile)

	if err := os.WriteFile(sourceFile, []byte("external-change\n"), 0o644); err != nil {
		t.Fatalf("external change source file: %v", err)
	}
	if err := os.WriteFile(workspaceFile, []byte("workspace-change\n"), 0o644); err != nil {
		t.Fatalf("change workspace file: %v", err)
	}

	mergeAny, err := env.srv.InvokeMethod(ctx, "workspace/run/merge", mustJSONParams(t, map[string]any{
		"runKey":    runKey,
		"updatedBy": "test",
		"dryRun":    false,
	}))
	if err != nil {
		t.Fatalf("workspace/run/merge conflict: %v", err)
	}
	var mergeResp struct {
		Result struct {
			Status    string `json:"status"`
			Conflicts int    `json:"conflicts"`
			Merged    int    `json:"merged"`
		} `json:"result"`
	}
	decodeJSONAny(t, mergeAny, &mergeResp)
	if mergeResp.Result.Conflicts == 0 {
		t.Fatalf("expected conflict > 0")
	}
	if mergeResp.Result.Status != service.WorkspaceRunStatusFailed {
		t.Fatalf("merge status expected failed, got=%s", mergeResp.Result.Status)
	}
	data, _ := os.ReadFile(sourceFile)
	if string(data) != "external-change\n" {
		t.Fatalf("source file should keep external change on conflict: %q", string(data))
	}
}

func mustJSONParams(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return json.RawMessage(data)
}

func decodeJSONAny(t *testing.T, src any, dst any) {
	t.Helper()
	data, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal any: %v", err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("unmarshal any: %v", err)
	}
}
