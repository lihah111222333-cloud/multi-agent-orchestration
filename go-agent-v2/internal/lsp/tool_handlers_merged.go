package lsp

import (
	"encoding/json"
	"fmt"
)

type lspMergedActionParam struct {
	Action string `json:"action"`
}

func decodeMergedAction(args json.RawMessage) (string, error) {
	p, err := decodeArgs[lspMergedActionParam](args)
	if err != nil {
		return "", err
	}
	if p.Action == "" {
		return "", fmt.Errorf("missing required param: action")
	}
	return p.Action, nil
}

func decodeFileAction(args json.RawMessage) (string, error) {
	payload, err := decodeArgs[map[string]any](args)
	if err != nil {
		return "", err
	}
	if action, ok := payload["action"].(string); ok && action != "" {
		return action, nil
	}
	if _, hasContent := payload["new_content"]; hasContent {
		return "change", nil
	}
	return "open", nil
}

// LSPFile routes open/change actions.
func (h *ToolHandlers) LSPFile(args json.RawMessage) string {
	action, err := decodeFileAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "open":
		return h.OpenFile(args)
	case "change":
		return h.DidChange(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_file action: %s", action))
	}
}

// LSPInspect routes inspect operations.
func (h *ToolHandlers) LSPInspect(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "hover":
		return h.Hover(args)
	case "diagnostics":
		return h.Diagnostics(args)
	case "signature_help":
		return h.SignatureHelp(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_inspect action: %s", action))
	}
}

// LSPXRef routes cross-reference operations.
func (h *ToolHandlers) LSPXRef(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "definition":
		return h.Definition(args)
	case "references":
		return h.References(args)
	case "implementation":
		return h.Implementation(args)
	case "type_definition":
		return h.TypeDefinition(args)
	case "workspace_symbol":
		return h.WorkspaceSymbol(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_xref action: %s", action))
	}
}

// LSPGrep routes text/ast search operations.
func (h *ToolHandlers) LSPGrep(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "text_search":
		return h.TextSearch(args)
	case "ast_search":
		return h.AstSearch(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_grep action: %s", action))
	}
}

// LSPStructure routes structure/hierarchy operations.
func (h *ToolHandlers) LSPStructure(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "document_symbol":
		return h.DocumentSymbol(args)
	case "call_hierarchy":
		return h.CallHierarchy(args)
	case "type_hierarchy":
		return h.TypeHierarchy(args)
	case "semantic_tokens":
		return h.SemanticTokens(args)
	case "folding_range":
		return h.FoldingRange(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_structure action: %s", action))
	}
}

// LSPEdit routes edit operations.
func (h *ToolHandlers) LSPEdit(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "rename":
		return h.Rename(args)
	case "code_action":
		return h.CodeAction(args)
	case "format":
		return h.Format(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_edit action: %s", action))
	}
}
