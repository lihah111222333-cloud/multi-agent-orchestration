package apiserver

import "github.com/multi-agent/go-agent-v2/internal/lsp"

func setDiagnosticsCacheState(s *Server, uri string, diagnostics []lsp.Diagnostic) {
	if s == nil {
		return
	}
	s.diagnosticsCacheState.setDiagnostics(uri, diagnostics)
}

func getDiagnosticsCacheState(s *Server, uri string) []lsp.Diagnostic {
	if s == nil {
		return nil
	}
	return s.diagnosticsCacheState.getDiagnostics(uri)
}

func allDiagnosticsCacheState(s *Server) map[string][]lsp.Diagnostic {
	if s == nil {
		return map[string][]lsp.Diagnostic{}
	}
	return s.diagnosticsCacheState.allDiagnostics()
}
