package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/lsp"
)

type serverDiagnosticsAccessor struct {
	s *Server
}

func (a serverDiagnosticsAccessor) SetDiagnostics(uri string, diagnostics []lsp.Diagnostic) {
	setDiagnostics(a.s, uri, diagnostics)
}

func (a serverDiagnosticsAccessor) GetDiagnostics(uri string) []lsp.Diagnostic {
	return getDiagnostics(a.s, uri)
}

func (a serverDiagnosticsAccessor) GetAllDiagnostics() map[string][]lsp.Diagnostic {
	return getAllDiagnostics(a.s)
}

func diagnosticsAccessor(s *Server) lsp.DiagnosticsAccessor {
	return serverDiagnosticsAccessor{s: s}
}

func setDiagnostics(s *Server, uri string, diagnostics []lsp.Diagnostic) {
	if s == nil {
		return
	}
	normalized := strings.TrimSpace(uri)
	if normalized == "" {
		return
	}
	copied := cloneDiagnostics(diagnostics)
	s.diagnosticsCacheState.diagMu.Lock()
	defer s.diagnosticsCacheState.diagMu.Unlock()
	if s.diagnosticsCacheState.diagCache == nil {
		s.diagnosticsCacheState.diagCache = map[string][]lsp.Diagnostic{}
	}
	if len(copied) == 0 {
		delete(s.diagnosticsCacheState.diagCache, normalized)
		return
	}
	s.diagnosticsCacheState.diagCache[normalized] = copied
}

func getDiagnostics(s *Server, uri string) []lsp.Diagnostic {
	if s == nil {
		return nil
	}
	normalized := strings.TrimSpace(uri)
	if normalized == "" {
		return nil
	}
	s.diagnosticsCacheState.diagMu.RLock()
	defer s.diagnosticsCacheState.diagMu.RUnlock()
	return cloneDiagnostics(s.diagnosticsCacheState.diagCache[normalized])
}

func getAllDiagnostics(s *Server) map[string][]lsp.Diagnostic {
	if s == nil {
		return map[string][]lsp.Diagnostic{}
	}
	s.diagnosticsCacheState.diagMu.RLock()
	defer s.diagnosticsCacheState.diagMu.RUnlock()
	out := make(map[string][]lsp.Diagnostic, len(s.diagnosticsCacheState.diagCache))
	for uri, diagnostics := range s.diagnosticsCacheState.diagCache {
		out[uri] = cloneDiagnostics(diagnostics)
	}
	return out
}

func cloneDiagnostics(in []lsp.Diagnostic) []lsp.Diagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make([]lsp.Diagnostic, len(in))
	copy(out, in)
	return out
}
