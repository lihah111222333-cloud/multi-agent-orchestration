package apiserver

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestP3ProcClientUsageIsIsolated(t *testing.T) {
	t.Helper()

	root := "."
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel := filepath.Clean(path)
		if strings.HasPrefix(rel, "codexadapter"+string(filepath.Separator)) {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(data)
		clientPrefix := "Client"
		if strings.Contains(content, clientPrefix+".Submit") ||
			strings.Contains(content, clientPrefix+".SendCommand") ||
			strings.Contains(content, clientPrefix+".GetThreadID") ||
			strings.Contains(content, clientPrefix+".ResumeThread") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/apiserver: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("unexpected proc.Client usage outside codexadapter: %v", offenders)
	}
}

func TestP3CodexEntryMethodsDelegateToCodexAdapter(t *testing.T) {
	t.Helper()

	checks := []string{
		"turnStartTyped",
		"turnSteerTyped",
		"turnInterrupt",
		"turnForceComplete",
		"reviewStartTyped",
		"threadResumeTyped",
		"threadRecoverTyped",
		"threadNameSetTyped",
		"threadRollbackTyped",
		"threadMessagesTyped",
		"threadArchiveTyped",
		"threadUnarchiveTyped",
	}

	files := parseAPIServerNonTestFiles(t)
	for _, fn := range checks {
		fd, fileName, ok := findFuncDecl(files, fn)
		if !ok {
			t.Fatalf("function %s not found", fn)
		}
		if !funcDeclContainsCodexAdapterCall(fd) {
			t.Fatalf("%s in %s must delegate via s.codexAdapter", fn, fileName)
		}
	}

	removed := []string{
		"ensureThreadReadyForTurn",
		"completeTrackedTurnByID",
		"checkTurnStall",
		"executeStallAutoInterrupt",
	}
	for _, fn := range removed {
		if _, _, ok := findFuncDecl(files, fn); ok {
			t.Fatalf("function %s should be removed from apiserver and owned by codexadapter", fn)
		}
	}
}

func TestP3ThreadArchiveHelpersMovedToCodexAdapter(t *testing.T) {
	t.Helper()

	removed := []string{
		"pruneArchivedCodexSourceFiles",
		"collectThreadArtifactCandidates",
		"findLatestThreadArchiveManifestPath",
		"readThreadArchiveManifest",
		"writeThreadArchiveManifest",
		"restoreThreadArchiveSources",
		"inspectThreadArchiveForRestore",
		"normalizeThreadArchiveMap",
		"archiveThreadArtifacts",
		"threadExistsForArchive",
		"persistThreadArchivedState",
		"removeThreadArchivedState",
		"resolveRolloutHistorySource",
	}
	files := parseAPIServerNonTestFiles(t)
	for _, fn := range removed {
		if _, _, ok := findFuncDecl(files, fn); ok {
			t.Fatalf("function %s should be removed from apiserver and owned by codexadapter", fn)
		}
	}
}

func TestP3HistoryResolutionHelpersDelegateToCodexAdapter(t *testing.T) {
	t.Helper()

	removed := []string{
		"threadExistsInHistory",
		"resolveCodexThreadCandidates",
	}
	files := parseAPIServerNonTestFiles(t)
	for _, fn := range removed {
		if _, _, ok := findFuncDecl(files, fn); ok {
			t.Fatalf("function %s should be removed from apiserver and owned by codexadapter", fn)
		}
	}
}

func TestP3PayloadAndApprovalRemainThinBoundary(t *testing.T) {
	t.Helper()

	type fileCheck struct {
		file      string
		forbidden []string
	}

	checks := []fileCheck{
		{
			file: "server_payload.go",
			forbidden: []string{
				"s.captureAndInjectTurnSummary(",
				"s.touchTrackedTurnLastEvent(",
				"s.hasActiveTrackedTurn(",
				"s.maybeFinalizeTrackedTurn(",
			},
		},
		{
			file: "server_approval.go",
			forbidden: []string{
				"s.touchTrackedTurnLastEvent(",
				"defaultStallThreshold / 6",
				"s.stallThreshold / 6",
			},
		},
	}

	for _, check := range checks {
		data, err := os.ReadFile(check.file)
		if err != nil {
			t.Fatalf("read %s: %v", check.file, err)
		}
		content := string(data)
		for _, needle := range check.forbidden {
			if strings.Contains(content, needle) {
				t.Fatalf("%s must not contain %q (strict thin boundary)", check.file, needle)
			}
		}
	}
}

func TestP3ResidualMethodsMustDelegateViaCodexAdapter(t *testing.T) {
	t.Helper()

	checks := []string{
		"threadArchiveTyped",
		"threadUnarchiveTyped",
	}

	files := parseAPIServerNonTestFiles(t)
	for _, fn := range checks {
		fd, fileName, ok := findFuncDecl(files, fn)
		if !ok {
			t.Fatalf("function %s not found", fn)
		}
		if !funcDeclContainsCodexAdapterCall(fd) {
			t.Fatalf("%s in %s must delegate via s.codexAdapter", fn, fileName)
		}
	}
}

func TestP4BoundaryMethodsAvoidDirectClientCalls(t *testing.T) {
	t.Helper()

	checks := []struct {
		fn     string
		method string
	}{
		{fn: "threadForkTyped", method: "ForkThread"},
		{fn: "threadReadTyped", method: "ListThreads"},
		{fn: "handleApprovalRequest", method: "Submit"},
		{fn: "AgentEventHandler", method: "GetThreadID"},
		{fn: "handleDynamicToolCall", method: "RespondError"},
		{fn: "handleDynamicToolCall", method: "SendDynamicToolResult"},
	}

	files := parseAPIServerNonTestFiles(t)
	for _, item := range checks {
		fd, fileName, ok := findFuncDecl(files, item.fn)
		if !ok {
			t.Fatalf("function %s not found", item.fn)
		}
		if funcDeclContainsClientMethodCall(fd, item.method) {
			t.Fatalf("%s in %s must not call proc.Client.%s directly", item.fn, fileName, item.method)
		}
		if !funcDeclContainsCodexAdapterCall(fd) {
			t.Fatalf("%s in %s must delegate via s.codexAdapter", item.fn, fileName)
		}
	}
}

func TestP4NoDirectCodexPackageImportOutsideAdapter(t *testing.T) {
	t.Helper()

	root := "."
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if filepath.Clean(path) == filepath.Clean("codexadapter") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel := filepath.Clean(path)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			if imp == nil || imp.Path == nil {
				continue
			}
			importPath := strings.Trim(imp.Path.Value, "\"")
			if importPath == "github.com/multi-agent/go-agent-v2/internal/codex" {
				offenders = append(offenders, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk imports under internal/apiserver: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("unexpected internal/codex import outside codexadapter: %v", offenders)
	}
}

func TestP4MethodsHelpersContainOnlySlashHandlers(t *testing.T) {
	t.Helper()

	data, err := os.ReadFile("methods.go")
	if err != nil {
		t.Fatalf("read methods.go: %v", err)
	}
	content := string(data)

	forbidden := []string{
		"func (s *Server) withThread(",
		"func (s *Server) extractInputs(",
		"func buildAttachmentName(",
		"func buildAttachmentPreviewURL(",
		"func buildUserTimelineAttachments(",
		"func buildUserTimelineAttachmentsFromInputs(",
		"func (s *Server) debugRuntime(",
		"func (s *Server) debugForceGC(",
	}
	for _, needle := range forbidden {
		if strings.Contains(content, needle) {
			t.Fatalf("methods.go must not contain %q after P4 convergence", needle)
		}
	}

	required := []string{
		"func (s *Server) threadBgTerminalsClean(",
		"func (s *Server) threadUndo(",
		"func (s *Server) threadModelSet(",
		"func (s *Server) threadPersonality(",
		"func (s *Server) threadApprovals(",
		"func (s *Server) threadMCPList(",
		"func (s *Server) threadSkillsList(",
		"func (s *Server) threadDebugMemory(",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("methods.go missing required slash handler %q", needle)
		}
	}
}
