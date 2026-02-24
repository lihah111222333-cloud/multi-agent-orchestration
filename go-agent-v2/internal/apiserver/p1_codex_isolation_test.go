package apiserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestP1CodexSymbolsAreIsolated enforces P1 symbol placement in same-package codex files.
func TestP1CodexSymbolsAreIsolated(t *testing.T) {
	t.Helper()

	fset := token.NewFileSet()
	funcToFile := make(map[string]string)
	typeToFile := make(map[string]string)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".go" || isTestGoFile(name) {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse file %s: %v", name, parseErr)
		}
		base := filepath.Base(name)
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				funcToFile[d.Name.Name] = base
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					continue
				}
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					typeToFile[ts.Name.Name] = base
				}
			}
		}
	}

	assertFuncInFile(t, funcToFile, "threadResumeTyped", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "threadMessagesTyped", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "threadArchiveTyped", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "threadUnarchiveTyped", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "threadRollbackTyped", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "threadNameSetTyped", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "resolveRolloutHistorySource", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "loadAllThreadMessagesFromCodexRollout", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "archiveThreadArtifacts", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "collectThreadArtifactCandidates", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "pruneArchivedCodexSourceFiles", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "restoreThreadArchiveSources", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "inspectThreadArchiveForRestore", "methods_thread_codex.go")
	assertFuncInFile(t, funcToFile, "findLatestThreadArchiveManifestPath", "methods_thread_codex.go")

	assertFuncInFile(t, funcToFile, "isLikelyCodexThreadID", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "normalizeCodexThreadID", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "buildResumeCandidates", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "tryResumeCandidates", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "resolvePrimaryCodexThreadID", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "resolveCodexThreadCandidates", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "ensureThreadReadyForTurn", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "registerBinding", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "resolveSlashCommandThread", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "resolveThreadForSlashCommand", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "sendSlashCommand", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "sendSlashCommandWithArgs", "methods_helpers_codex.go")
	assertFuncInFile(t, funcToFile, "threadExistsInHistory", "methods_helpers_codex.go")

	assertFuncInFile(t, funcToFile, "resolveClientActiveTurnID", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "turnStartTyped", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "turnSteerTyped", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "turnInterrupt", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "turnForceComplete", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "reviewStartTyped", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "normalizeInterruptState", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "readThreadRuntimeState", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "waitInterruptSettled", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "waitInterruptOutcome", "methods_turn_codex.go")
	assertFuncInFile(t, funcToFile, "interruptSettleMode", "methods_turn_codex.go")

	assertTypeInFile(t, typeToFile, "trackedTurn", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "beginTrackedTurn", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "hasActiveTrackedTurn", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "markTrackedTurnInterruptRequested", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "waitTrackedTurnTerminal", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "completeTrackedTurn", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "completeTrackedTurnByID", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "maybeFinalizeTrackedTurn", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "peekTrackedTurnMeta", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "markTrackedTurnStallHint", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "checkTurnStall", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "rescheduleStallCheck", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "handleStallGracePeriod", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "executeStallAutoInterrupt", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "touchTrackedTurnLastEvent", "turn_tracker_codex.go")
	assertFuncInFile(t, funcToFile, "shouldLogTrackedTurnStallHint", "turn_tracker_codex.go")
}

func assertFuncInFile(t *testing.T, functions map[string]string, fn string, wantFile string) {
	t.Helper()
	gotFile, ok := functions[fn]
	if !ok {
		t.Fatalf("function %s not found", fn)
	}
	if gotFile != wantFile {
		t.Fatalf("function %s in %s, want %s", fn, gotFile, wantFile)
	}
}

func assertTypeInFile(t *testing.T, types map[string]string, typ string, wantFile string) {
	t.Helper()
	gotFile, ok := types[typ]
	if !ok {
		t.Fatalf("type %s not found", typ)
	}
	if gotFile != wantFile {
		t.Fatalf("type %s in %s, want %s", typ, gotFile, wantFile)
	}
}

func isTestGoFile(name string) bool {
	if len(name) < len("_test.go") {
		return false
	}
	return name[len(name)-len("_test.go"):] == "_test.go"
}
