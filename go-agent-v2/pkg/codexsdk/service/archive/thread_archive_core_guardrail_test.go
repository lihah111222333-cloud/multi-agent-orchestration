package archive

import (
	"go/ast"
	"go/parser"
	"go/token"
	"runtime"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/pathutil"
)

func TestThreadArchiveCoreExportSurfaceGuardrail(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	corePath := pathutil.Join(pathutil.Dir(thisFile), "thread_archive_core.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, corePath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse thread_archive_core.go: %v", err)
	}

	allowedExports := map[string]struct{}{
		"InferThreadArtifactKind":        {},
		"ThreadArchiveFile":              {},
		"ThreadArchiveManifest":          {},
		"ThreadArchiveRestoreNotice":     {},
		"ThreadArchiveRestoreDeps":       {},
		"ThreadArtifactCandidate":        {},
		"ThreadArchiveFileState":         {},
		"MergeThreadArchiveMaps":         {},
		"ParseArchiveTimestamp":          {},
		"BuildThreadArchiveRestoreDeps":  {},
		"InspectThreadArchiveForRestore": {},
		"RestoreThreadArchiveSources":    {},
		"PruneArchivedCodexSourceFiles":  {},
	}
	foundAllowed := map[string]struct{}{}
	unexpected := make([]string, 0)

	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			if typed.Recv != nil || typed.Name == nil {
				continue
			}
			name := strings.TrimSpace(typed.Name.Name)
			if name == "" || !ast.IsExported(name) {
				continue
			}
			if _, ok := allowedExports[name]; ok {
				foundAllowed[name] = struct{}{}
				continue
			}
			unexpected = append(unexpected, name)
		case *ast.GenDecl:
			if typed.Tok != token.TYPE {
				continue
			}
			for _, spec := range typed.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name == nil {
					continue
				}
				name := strings.TrimSpace(typeSpec.Name.Name)
				if name == "" || !ast.IsExported(name) {
					continue
				}
				if _, ok := allowedExports[name]; ok {
					foundAllowed[name] = struct{}{}
					continue
				}
				unexpected = append(unexpected, name)
			}
		}
	}

	if len(unexpected) > 0 {
		t.Fatalf("thread_archive_core export surface regression: unexpected exports: %s", strings.Join(unexpected, ", "))
	}
	for name := range allowedExports {
		if _, ok := foundAllowed[name]; !ok {
			t.Fatalf("thread_archive_core export surface regression: missing expected export: %s", name)
		}
	}
}
