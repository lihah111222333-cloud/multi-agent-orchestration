package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// LocationLink 是 definition/implementation/typeDefinition 的联合返回类型之一。
// 坐标保持 LSP 0-based，不在服务端做转换。
type LocationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// Command 是 textDocument/codeAction 联合返回类型之一。
type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

// SymbolInformation 是 workspace/symbol 旧形态。
type SymbolInformation struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Location      Location   `json:"location"`
	ContainerName string     `json:"containerName,omitempty"`
}

// WorkspaceSymbolLocation 是 workspace/symbol 新形态 location。
type WorkspaceSymbolLocation struct {
	URI string `json:"uri"`
}

// WorkspaceSymbol 是 workspace/symbol 新形态。
type WorkspaceSymbol struct {
	Name          string `json:"name"`
	Kind          int    `json:"kind"`
	Location      any    `json:"location,omitempty"` // Location | WorkspaceSymbolLocation
	ContainerName string `json:"containerName,omitempty"`
	Data          any    `json:"data,omitempty"`
}

// WorkspaceSymbolParams workspace/symbol 请求参数。
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// CodeAction 是 textDocument/codeAction 联合返回类型之一。
type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
	Command     *Command       `json:"command,omitempty"`
	Data        any            `json:"data,omitempty"`
}

// CodeActionContext code action 请求上下文。
type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
	TriggerKind int          `json:"triggerKind,omitempty"`
}

// CodeActionParams textDocument/codeAction 请求参数。
type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

// CallHierarchyItem prepareCallHierarchy 返回项。
type CallHierarchyItem struct {
	Name           string `json:"name"`
	Kind           int    `json:"kind"`
	URI            string `json:"uri"`
	Range          Range  `json:"range"`
	SelectionRange Range  `json:"selectionRange"`
	Data           any    `json:"data,omitempty"`
}

// TypeHierarchyItem prepareTypeHierarchy 返回项。
type TypeHierarchyItem struct {
	Name           string `json:"name"`
	Kind           int    `json:"kind"`
	URI            string `json:"uri"`
	Range          Range  `json:"range"`
	SelectionRange Range  `json:"selectionRange"`
	Data           any    `json:"data,omitempty"`
}

// SemanticTokensLegend 语义高亮 legend。
type SemanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

// SemanticTokensOptions semanticTokensProvider 对象形态。
type SemanticTokensOptions struct {
	Legend SemanticTokensLegend `json:"legend"`
}

// LocationResult 是 Location/LocationLink 的统一封装。
type LocationResult struct {
	Location     *Location     `json:"location,omitempty"`
	LocationLink *LocationLink `json:"locationLink,omitempty"`
	Canonical    *Location     `json:"canonical,omitempty"`
}

// PrimaryLocation 返回最适合作为兼容输出的位置。
func (r LocationResult) PrimaryLocation() *Location {
	if r.Location != nil {
		return r.Location
	}
	return r.Canonical
}

// WorkspaceSymbolResult 是 workspace/symbol 新旧返回的统一封装。
type WorkspaceSymbolResult struct {
	SymbolInformation *SymbolInformation `json:"symbolInformation,omitempty"`
	WorkspaceSymbol   *WorkspaceSymbol   `json:"workspaceSymbol,omitempty"`
}

// CodeActionResult 是 CodeAction|Command 的统一封装。
type CodeActionResult struct {
	CodeAction *CodeAction `json:"codeAction,omitempty"`
	Command    *Command    `json:"command,omitempty"`
}

func isNullRaw(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null"
}

// decodeLocationsLike 兼容解码:
// Location | []Location | []LocationLink | null
func decodeLocationsLike(raw json.RawMessage) ([]LocationResult, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]LocationResult, 0, len(arr))
		for _, item := range arr {
			one, err := decodeLocationLikeOne(item)
			if err != nil {
				return nil, err
			}
			out = append(out, one)
		}
		return out, nil
	}

	one, err := decodeLocationLikeOne(raw)
	if err != nil {
		return nil, err
	}
	return []LocationResult{one}, nil
}

func decodeLocationLikeOne(raw json.RawMessage) (LocationResult, error) {
	var probe struct {
		URI       *string `json:"uri"`
		TargetURI *string `json:"targetUri"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return LocationResult{}, fmt.Errorf("decode location-like: %w", err)
	}

	if probe.TargetURI != nil {
		var link LocationLink
		if err := json.Unmarshal(raw, &link); err != nil {
			return LocationResult{}, fmt.Errorf("decode locationLink: %w", err)
		}
		canonicalRange := link.TargetSelectionRange
		if canonicalRange == (Range{}) {
			canonicalRange = link.TargetRange
		}
		return LocationResult{
			LocationLink: &link,
			Canonical: &Location{
				URI:   link.TargetURI,
				Range: canonicalRange,
			},
		}, nil
	}

	if probe.URI != nil {
		var loc Location
		if err := json.Unmarshal(raw, &loc); err != nil {
			return LocationResult{}, fmt.Errorf("decode location: %w", err)
		}
		return LocationResult{Location: &loc}, nil
	}

	return LocationResult{}, fmt.Errorf("decode location-like: unsupported payload")
}

// decodeWorkspaceSymbols 兼容解码:
// []SymbolInformation | []WorkspaceSymbol | null
func decodeWorkspaceSymbols(raw json.RawMessage) ([]WorkspaceSymbolResult, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("decode workspace symbols: %w", err)
	}

	out := make([]WorkspaceSymbolResult, 0, len(arr))
	for _, item := range arr {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			return nil, fmt.Errorf("decode workspace symbol item: %w", err)
		}

		locationRaw := obj["location"]
		if len(locationRaw) > 0 {
			var locObj map[string]json.RawMessage
			if err := json.Unmarshal(locationRaw, &locObj); err == nil {
				if _, hasRange := locObj["range"]; hasRange {
					var legacy SymbolInformation
					if err := json.Unmarshal(item, &legacy); err != nil {
						return nil, fmt.Errorf("decode SymbolInformation: %w", err)
					}
					out = append(out, WorkspaceSymbolResult{SymbolInformation: &legacy})
					continue
				}
			}
		}

		var modern WorkspaceSymbol
		if err := json.Unmarshal(item, &modern); err != nil {
			return nil, fmt.Errorf("decode WorkspaceSymbol: %w", err)
		}
		out = append(out, WorkspaceSymbolResult{WorkspaceSymbol: &modern})
	}

	return out, nil
}

// decodeCodeActions 兼容解码:
// (CodeAction | Command)[] | null
func decodeCodeActions(raw json.RawMessage) ([]CodeActionResult, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("decode code actions: %w", err)
	}

	out := make([]CodeActionResult, 0, len(arr))
	for _, item := range arr {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			return nil, fmt.Errorf("decode code action item: %w", err)
		}

		decoded, err := decodeCodeActionOrCommand(item, obj)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}

	return out, nil
}

func decodeCodeActionOrCommand(item json.RawMessage, obj map[string]json.RawMessage) (CodeActionResult, error) {
	if !isCodeActionLike(obj) {
		var cmd Command
		if err := json.Unmarshal(item, &cmd); err == nil && strings.TrimSpace(cmd.Command) != "" {
			return CodeActionResult{Command: &cmd}, nil
		}
	}

	var action CodeAction
	if err := json.Unmarshal(item, &action); err == nil {
		return CodeActionResult{CodeAction: &action}, nil
	}

	var cmd Command
	if err := json.Unmarshal(item, &cmd); err == nil {
		return CodeActionResult{Command: &cmd}, nil
	}

	return CodeActionResult{}, fmt.Errorf("decode code action item: unsupported payload")
}

func isCodeActionLike(obj map[string]json.RawMessage) bool {
	if _, ok := obj["kind"]; ok {
		return true
	}
	if _, ok := obj["edit"]; ok {
		return true
	}
	if _, ok := obj["diagnostics"]; ok {
		return true
	}
	if _, ok := obj["isPreferred"]; ok {
		return true
	}
	if _, ok := obj["disabled"]; ok {
		return true
	}
	if _, ok := obj["data"]; ok {
		return true
	}
	commandRaw, hasCommand := obj["command"]
	if hasCommand {
		var commandName string
		if err := json.Unmarshal(commandRaw, &commandName); err == nil {
			return false
		}
		// command 为对象时属于 CodeAction.command，而非顶层 Command.command。
		return true
	}
	return false
}

// decodeCompletionItems 兼容解码:
// []CompletionItem | CompletionList | null
func decodeCompletionItems(raw json.RawMessage) ([]CompletionItem, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var list CompletionList
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		return list.Items, nil
	}

	var items []CompletionItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return items, nil
	}

	return nil, fmt.Errorf("decode completion: unsupported payload")
}

// decodeDocumentSymbols 兼容解码:
// []DocumentSymbol | []SymbolInformation | null
func decodeDocumentSymbols(raw json.RawMessage) ([]DocumentSymbol, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("decode document symbols: %w", err)
	}

	out := make([]DocumentSymbol, 0, len(arr))
	for _, item := range arr {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			return nil, fmt.Errorf("decode document symbol item: %w", err)
		}

		if _, ok := obj["location"]; ok {
			var legacy SymbolInformation
			if err := json.Unmarshal(item, &legacy); err != nil {
				return nil, fmt.Errorf("decode legacy document symbol: %w", err)
			}
			out = append(out, DocumentSymbol{
				Name:           legacy.Name,
				Kind:           legacy.Kind,
				Range:          legacy.Location.Range,
				SelectionRange: legacy.Location.Range,
			})
			continue
		}

		var symbol DocumentSymbol
		if err := json.Unmarshal(item, &symbol); err != nil {
			return nil, fmt.Errorf("decode document symbol: %w", err)
		}
		out = append(out, symbol)
	}

	return out, nil
}

// decodePrepareCallHierarchyItems 兼容解码:
// []CallHierarchyItem | null
func decodePrepareCallHierarchyItems(raw json.RawMessage) ([]CallHierarchyItem, error) {
	if isNullRaw(raw) {
		return nil, nil
	}
	var items []CallHierarchyItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode prepareCallHierarchy: %w", err)
	}
	return items, nil
}

// decodePrepareTypeHierarchyItems 兼容解码:
// []TypeHierarchyItem | null
func decodePrepareTypeHierarchyItems(raw json.RawMessage) ([]TypeHierarchyItem, error) {
	if isNullRaw(raw) {
		return nil, nil
	}
	var items []TypeHierarchyItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode prepareTypeHierarchy: %w", err)
	}
	return items, nil
}

func decodeSemanticTokensLegend(provider any) *SemanticTokensLegend {
	if provider == nil {
		return nil
	}
	if isBool, ok := provider.(bool); ok && isBool {
		return nil
	}

	raw, err := json.Marshal(provider)
	if err != nil {
		return nil
	}

	var options SemanticTokensOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil
	}
	if len(options.Legend.TokenTypes) == 0 && len(options.Legend.TokenModifiers) == 0 {
		return nil
	}
	return &options.Legend
}
