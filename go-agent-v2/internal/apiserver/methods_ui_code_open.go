package apiserver

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/dashboard"
)

type uiCodeOpenParams = dashboard.CodeOpenParams

func uiCodeOpenTyped(s *Server, _ context.Context, p uiCodeOpenParams) (any, error) {
	service := dashboard.NewCodeOpenService(dashboard.CodeOpenHooks{
		OpenLSPFile: func(path, content string) error {
			if s == nil || s.lsp == nil {
				return nil
			}
			return s.lsp.OpenFile(path, content)
		},
		DiagnosticsByURI: func(uri string) []dashboard.CodeOpenDiagnostic {
			if s == nil || strings.TrimSpace(uri) == "" {
				return nil
			}
			diags := getDiagnostics(s, uri)
			if len(diags) == 0 {
				return nil
			}
			out := make([]dashboard.CodeOpenDiagnostic, 0, len(diags))
			for _, diag := range diags {
				out = append(out, dashboard.CodeOpenDiagnostic{
					Line:     diag.Range.Start.Line + 1,
					Column:   diag.Range.Start.Character + 1,
					Severity: diag.Severity.String(),
					Message:  diag.Message,
				})
			}
			return out
		},
	})
	return service.Open(p)
}
