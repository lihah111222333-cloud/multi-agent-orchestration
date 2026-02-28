package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
)

func (s *Server) SetDiagnostics(uri string, diagnostics []lsp.Diagnostic) {
	setDiagnostics(s, uri, diagnostics)
}

func (s *Server) GetDiagnostics(uri string) []lsp.Diagnostic { return getDiagnostics(s, uri) }

func (s *Server) GetAllDiagnostics() map[string][]lsp.Diagnostic { return allDiagnosticsCacheState(s) }

var diagnosticsAccessor = func(s *Server) lsp.DiagnosticsAccessor { return s }

func setDiagnostics(s *Server, uri string, diagnostics []lsp.Diagnostic) {
	if uri = strings.TrimSpace(uri); s == nil || uri == "" {
		return
	}
	setDiagnosticsCacheState(s, uri, diagnostics)
}

func getDiagnostics(s *Server, uri string) []lsp.Diagnostic {
	if uri = strings.TrimSpace(uri); s == nil || uri == "" {
		return nil
	}
	return getDiagnosticsCacheState(s, uri)
}

func cloneDiagnostics(in []lsp.Diagnostic) []lsp.Diagnostic {
	return append([]lsp.Diagnostic(nil), in...)
}
