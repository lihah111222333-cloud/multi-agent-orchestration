package lsp

import "encoding/json"

// BindDynamicTool is a no-op binder on ToolHandlers.
//
// Dynamic tool ext registration is assembled by tooladapter via its own
// binding context. This method exists to satisfy tools.LSPProvider interface.
func (h *ToolHandlers) BindDynamicTool(_ string, _ func(json.RawMessage) string) {}
