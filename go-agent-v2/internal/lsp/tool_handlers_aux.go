package lsp

import (
	"path/filepath"
	"sort"
	"strings"
)

// AvailabilitySummary returns language server availability summary for UI/config use.
func (h *ToolHandlers) AvailabilitySummary() map[string]any {
	summary := map[string]any{
		"hasManager":           !h.managerUnavailable(),
		"hasAvailableServer":   false,
		"availableServerCount": 0,
		"servers":              []map[string]any{},
	}
	if h.managerUnavailable() {
		return summary
	}

	statuses := h.manager.Statuses()
	sort.SliceStable(statuses, func(i, j int) bool {
		return statuses[i].Language < statuses[j].Language
	})

	serverRows := make([]map[string]any, 0, len(statuses))
	availableCount := 0
	for _, st := range statuses {
		if st.Available {
			availableCount++
		}
		serverRows = append(serverRows, map[string]any{
			"language":  st.Language,
			"command":   st.Command,
			"available": st.Available,
			"running":   st.Running,
		})
	}

	summary["servers"] = serverRows
	summary["availableServerCount"] = availableCount
	summary["hasAvailableServer"] = availableCount > 0
	return summary
}

// DiagnosticsQuery returns diagnostics in JSON-RPC compatible map form.
func (h *ToolHandlers) DiagnosticsQuery(filePath string) map[string]any {
	if h.managerUnavailable() {
		return map[string]any{}
	}
	accessor := h.diagnosticsAccessor()
	if accessor == nil {
		return map[string]any{}
	}

	formatDiagnostics := func(diags []Diagnostic) []map[string]any {
		out := make([]map[string]any, 0, len(diags))
		for _, d := range diags {
			out = append(out, map[string]any{
				"message":  d.Message,
				"severity": d.Severity.String(),
				"line":     d.Range.Start.Line,
				"column":   d.Range.Start.Character,
			})
		}
		return out
	}

	result := map[string]any{}
	trimmed := strings.TrimSpace(filePath)
	if trimmed != "" {
		uri := trimmed
		if !strings.HasPrefix(uri, "file://") {
			if abs, err := filepath.Abs(uri); err == nil {
				uri = "file://" + abs
			}
		}
		diags := accessor.GetDiagnostics(uri)
		if len(diags) > 0 {
			result[uri] = formatDiagnostics(diags)
		}
		return result
	}

	for uri, diags := range accessor.GetAllDiagnostics() {
		if len(diags) == 0 {
			continue
		}
		result[uri] = formatDiagnostics(diags)
	}
	return result
}
