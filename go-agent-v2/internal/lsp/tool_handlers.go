package lsp

// DiagnosticsAccessor defines the thread-safe diagnostics cache surface required by ToolHandlers.
type DiagnosticsAccessor interface {
	SetDiagnostics(uri string, diagnostics []Diagnostic)
	GetDiagnostics(uri string) []Diagnostic
	GetAllDiagnostics() map[string][]Diagnostic
}

// ToolHandlers provides dynamic-tool compatible LSP handlers backed by Manager.
type ToolHandlers struct {
	manager     *Manager
	diagnostics DiagnosticsAccessor
}

// NewToolHandlers creates a ToolHandlers set with manager + diagnostics cache access.
func NewToolHandlers(manager *Manager, diagnostics DiagnosticsAccessor) *ToolHandlers {
	return &ToolHandlers{
		manager:     manager,
		diagnostics: diagnostics,
	}
}

func (h *ToolHandlers) managerUnavailable() bool {
	return h == nil || h.manager == nil
}

func (h *ToolHandlers) diagnosticsAccessor() DiagnosticsAccessor {
	if h == nil {
		return nil
	}
	return h.diagnostics
}
