package lsp

import (
	"strings"
)

func (h *ToolHandlers) contextualToolError(toolName, filePath string, line, column int, err error) string {
	if err == nil {
		return "error: unknown lsp error"
	}
	base := "error: " + err.Error()
	hint := lspToolCursorHint(toolName, err.Error())
	if hint == "" {
		return base
	}
	if hover := h.hoverSummaryAt(filePath, line, column); hover != "" {
		return base + "\nhint: " + hint + "\nhover: " + hover
	}
	return base + "\nhint: " + hint
}

func lspToolCursorHint(toolName, errText string) string {
	lower := strings.ToLower(strings.TrimSpace(errText))

	typeNameMismatch := strings.Contains(lower, "not a type name") ||
		strings.Contains(lower, "cannot find type name") ||
		strings.Contains(lower, "pkgname, not a type")

	notMethod := strings.Contains(lower, "not a method") ||
		strings.Contains(lower, "is a function, not a method")

	switch toolName {
	case "lsp_type_hierarchy":
		if typeNameMismatch || notMethod {
			return "place cursor on a type or interface identifier (for example Handler), not on package names or function declarations"
		}
	case "lsp_type_definition":
		if typeNameMismatch || notMethod {
			return "place cursor on an expression or identifier with a concrete type (for example a variable, field, or interface name)"
		}
	case "lsp_implementation":
		if typeNameMismatch || notMethod {
			return "place cursor on an interface type name, an interface method declaration, or a method call site"
		}
	}
	return ""
}

func (h *ToolHandlers) hoverSummaryAt(filePath string, line, column int) string {
	if h.managerUnavailable() || strings.TrimSpace(filePath) == "" || line < 0 || column < 0 {
		return ""
	}
	result, err := h.manager.Hover(filePath, line, column)
	if err != nil || result == nil {
		return ""
	}
	return firstHoverSummaryLine(result.Contents.Value)
}

func firstHoverSummaryLine(contents string) string {
	for _, raw := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line == "---" {
			continue
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		line = strings.Trim(line, "`")
		if line == "" {
			continue
		}
		if len(line) > 160 {
			return line[:157] + "..."
		}
		return line
	}
	return ""
}
