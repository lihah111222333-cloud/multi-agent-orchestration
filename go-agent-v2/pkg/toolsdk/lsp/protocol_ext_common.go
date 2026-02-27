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

func decodeRawArray(raw json.RawMessage, errPrefix string) ([]json.RawMessage, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return arr, nil
}

func decodeNullableSlice[T any](raw json.RawMessage, errPrefix string) ([]T, error) {
	if isNullRaw(raw) {
		return nil, nil
	}
	var items []T
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return items, nil
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
	arr, err := decodeRawArray(raw, "decode workspace symbols")
	if err != nil {
		return nil, err
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
	arr, err := decodeRawArray(raw, "decode code actions")
	if err != nil {
		return nil, err
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
	arr, err := decodeRawArray(raw, "decode document symbols")
	if err != nil {
		return nil, err
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
	return decodeNullableSlice[CallHierarchyItem](raw, "decode prepareCallHierarchy")
}

// decodePrepareTypeHierarchyItems 兼容解码:
// []TypeHierarchyItem | null
func decodePrepareTypeHierarchyItems(raw json.RawMessage) ([]TypeHierarchyItem, error) {
	return decodeNullableSlice[TypeHierarchyItem](raw, "decode prepareTypeHierarchy")
}

func decodeSemanticTokensLegend(provider any) *SemanticTokensLegend {
	if provider == nil || provider == true {
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

type itemRequest[T any] struct {
	Item T `json:"item"`
}

// PrepareCallHierarchyParams textDocument/prepareCallHierarchy 请求参数。
type PrepareCallHierarchyParams = TextDocumentPositionParams

// CallHierarchyIncomingCallsParams callHierarchy/incomingCalls 请求参数。
type CallHierarchyIncomingCallsParams = itemRequest[CallHierarchyItem]

// CallHierarchyOutgoingCallsParams callHierarchy/outgoingCalls 请求参数。
type CallHierarchyOutgoingCallsParams = itemRequest[CallHierarchyItem]

// CallHierarchyIncomingCall incoming 调用边。
type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

// CallHierarchyOutgoingCall outgoing 调用边。
type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}

// PrepareTypeHierarchyParams textDocument/prepareTypeHierarchy 请求参数。
type PrepareTypeHierarchyParams = TextDocumentPositionParams

// TypeHierarchySupertypesParams typeHierarchy/supertypes 请求参数。
type TypeHierarchySupertypesParams = itemRequest[TypeHierarchyItem]

// TypeHierarchySubtypesParams typeHierarchy/subtypes 请求参数。
type TypeHierarchySubtypesParams = itemRequest[TypeHierarchyItem]

// CallHierarchyResult 是稳定字段输出结构。
type CallHierarchyResult struct {
	Item     CallHierarchyItem           `json:"item"`
	Incoming []CallHierarchyIncomingCall `json:"incoming,omitempty"`
	Outgoing []CallHierarchyOutgoingCall `json:"outgoing,omitempty"`
}

// TypeHierarchyResult 是稳定字段输出结构。
type TypeHierarchyResult struct {
	Item       TypeHierarchyItem   `json:"item"`
	Supertypes []TypeHierarchyItem `json:"supertypes,omitempty"`
	Subtypes   []TypeHierarchyItem `json:"subtypes,omitempty"`
}

// XRefResultLimit 是 XRef 类工具的返回上限。
const XRefResultLimit = 50

// SignatureHelpParams textDocument/signatureHelp 请求参数。
type SignatureHelpParams = TextDocumentPositionParams

// SignatureHelpResult 是容错后的稳定签名帮助结构。
type SignatureHelpResult struct {
	Signatures      []SignatureInformationResult `json:"signatures,omitempty"`
	ActiveSignature *int                         `json:"activeSignature,omitempty"`
	ActiveParameter *int                         `json:"activeParameter,omitempty"`
}

// SignatureInformationResult 是单个签名信息。
type SignatureInformationResult struct {
	Label             string                       `json:"label"`
	Documentation     string                       `json:"documentation,omitempty"`
	DocumentationKind string                       `json:"documentationKind,omitempty"`
	Parameters        []ParameterInformationResult `json:"parameters,omitempty"`
}

// ParameterInformationResult 是签名参数信息。
type ParameterInformationResult struct {
	Label             string `json:"label,omitempty"`
	LabelOffsets      []int  `json:"labelOffsets,omitempty"`
	Documentation     string `json:"documentation,omitempty"`
	DocumentationKind string `json:"documentationKind,omitempty"`
}

// DocumentFormattingParams textDocument/formatting 请求参数。
type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

// FormattingOptions 格式化选项。
type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

// decodeSignatureHelp 兼容解码 signatureHelp:
// SignatureHelp | null，其中 documentation 与 label 支持联合类型容错。
func decodeSignatureHelp(raw json.RawMessage) (*SignatureHelpResult, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("decode signatureHelp: %w", err)
	}

	result := &SignatureHelpResult{}
	if signaturesRaw, ok := root["signatures"]; ok {
		var signaturesItems []json.RawMessage
		if err := json.Unmarshal(signaturesRaw, &signaturesItems); err != nil {
			return nil, fmt.Errorf("decode signatureHelp signatures: %w", err)
		}
		signatures := make([]SignatureInformationResult, 0, len(signaturesItems))
		for _, item := range signaturesItems {
			signature, err := decodeSignatureInformation(item)
			if err != nil {
				return nil, err
			}
			signatures = append(signatures, signature)
		}
		result.Signatures = signatures
	}
	if activeSignatureRaw, ok := root["activeSignature"]; ok {
		var value int
		if err := json.Unmarshal(activeSignatureRaw, &value); err == nil {
			result.ActiveSignature = &value
		}
	}
	if activeParameterRaw, ok := root["activeParameter"]; ok {
		var value int
		if err := json.Unmarshal(activeParameterRaw, &value); err == nil {
			result.ActiveParameter = &value
		}
	}

	return result, nil
}

func decodeSignatureInformation(raw json.RawMessage) (SignatureInformationResult, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return SignatureInformationResult{}, fmt.Errorf("decode signature info: %w", err)
	}

	var result SignatureInformationResult
	if labelRaw, ok := obj["label"]; ok {
		if err := json.Unmarshal(labelRaw, &result.Label); err != nil {
			result.Label = string(bytes.TrimSpace(labelRaw))
		}
	}
	if documentationRaw, ok := obj["documentation"]; ok {
		result.Documentation, result.DocumentationKind = decodeStringOrMarkup(documentationRaw)
	}
	if parametersRaw, ok := obj["parameters"]; ok {
		var parameterItems []json.RawMessage
		if err := json.Unmarshal(parametersRaw, &parameterItems); err != nil {
			return SignatureInformationResult{}, fmt.Errorf("decode signature parameters: %w", err)
		}

		parameters := make([]ParameterInformationResult, 0, len(parameterItems))
		for _, item := range parameterItems {
			parameter, err := decodeParameterInformation(item)
			if err != nil {
				return SignatureInformationResult{}, err
			}
			parameters = append(parameters, parameter)
		}
		result.Parameters = parameters
	}
	return result, nil
}

func decodeParameterInformation(raw json.RawMessage) (ParameterInformationResult, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ParameterInformationResult{}, fmt.Errorf("decode signature parameter: %w", err)
	}

	var result ParameterInformationResult
	if labelRaw, ok := obj["label"]; ok {
		result.Label, result.LabelOffsets = decodeParameterLabel(labelRaw)
	}
	if documentationRaw, ok := obj["documentation"]; ok {
		result.Documentation, result.DocumentationKind = decodeStringOrMarkup(documentationRaw)
	}

	return result, nil
}

func decodeStringOrMarkup(raw json.RawMessage) (string, string) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, "plaintext"
	}

	var markup struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &markup); err == nil {
		return markup.Value, markup.Kind
	}

	return "", ""
}

func decodeParameterLabel(raw json.RawMessage) (string, []int) {
	var label string
	if err := json.Unmarshal(raw, &label); err == nil {
		return label, nil
	}

	var offsets []int
	if err := json.Unmarshal(raw, &offsets); err == nil && len(offsets) == 2 {
		return "", offsets
	}

	return string(bytes.TrimSpace(raw)), nil
}

// decodeTextEdits 兼容解码 formatting 返回:
// []TextEdit | null
func decodeTextEdits(raw json.RawMessage) ([]TextEdit, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var edits []TextEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil, fmt.Errorf("decode text edits: %w", err)
	}
	return edits, nil
}

// SemanticTokenResultLimit 是 semantic_tokens decoded 输出上限。
const SemanticTokenResultLimit = 200

// SemanticTokensParams textDocument/semanticTokens/full 请求参数。
type SemanticTokensParams = DocumentSymbolParams

// SemanticTokens textDocument/semanticTokens/full 原始返回。
type SemanticTokens struct {
	ResultID string `json:"resultId,omitempty"`
	Data     []int  `json:"data"`
}

// DecodedSemanticToken 是相对编码展开后的语义 token。
type DecodedSemanticToken struct {
	Line           int      `json:"line"`
	StartCharacter int      `json:"startCharacter"`
	Length         int      `json:"length"`
	TokenType      string   `json:"tokenType"`
	TokenModifiers []string `json:"tokenModifiers,omitempty"`
}

// SemanticTokensResult 是稳定输出结构，包含原始与解码结果。
type SemanticTokensResult struct {
	ResultID string                 `json:"resultId,omitempty"`
	Data     []int                  `json:"data,omitempty"`
	Decoded  []DecodedSemanticToken `json:"decoded,omitempty"`
}

// FoldingRangeParams textDocument/foldingRange 请求参数。
type FoldingRangeParams = DocumentSymbolParams

// FoldingRange 是折叠区间。
type FoldingRange struct {
	StartLine      int    `json:"startLine"`
	StartCharacter *int   `json:"startCharacter,omitempty"`
	EndLine        int    `json:"endLine"`
	EndCharacter   *int   `json:"endCharacter,omitempty"`
	Kind           string `json:"kind,omitempty"`
	CollapsedText  string `json:"collapsedText,omitempty"`
}

// decodeSemanticTokens 兼容解码:
// SemanticTokens | int[] | null
func decodeSemanticTokens(raw json.RawMessage) (*SemanticTokens, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var payload SemanticTokens
	if err := json.Unmarshal(raw, &payload); err == nil {
		return &payload, nil
	}

	var data []int
	if err := json.Unmarshal(raw, &data); err == nil {
		return &SemanticTokens{Data: data}, nil
	}

	return nil, fmt.Errorf("decode semantic tokens: unsupported payload")
}

func decodeSemanticTokenData(data []int, legend *SemanticTokensLegend, limit int) ([]DecodedSemanticToken, error) {
	if legend == nil {
		return nil, fmt.Errorf("semantic tokens legend unavailable")
	}
	if len(data)%5 != 0 {
		return nil, fmt.Errorf("semantic token data length must be multiple of 5")
	}
	if limit <= 0 {
		limit = SemanticTokenResultLimit
	}

	out := make([]DecodedSemanticToken, 0, minInt(len(data)/5, limit))
	state := semanticTokenDecodeState{}
	for i := 0; i+4 < len(data); i += 5 {
		token, err := decodeSemanticTokenChunk(data[i:i+5], &state, legend)
		if err != nil {
			return nil, err
		}
		out = append(out, token)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type semanticTokenDecodeState struct {
	line  int
	start int
}

func decodeSemanticTokenChunk(
	chunk []int,
	state *semanticTokenDecodeState,
	legend *SemanticTokensLegend,
) (DecodedSemanticToken, error) {
	deltaLine, deltaStart := chunk[0], chunk[1]
	length, tokenTypeIndex, modifierBits := chunk[2], chunk[3], chunk[4]
	if deltaLine < 0 || deltaStart < 0 || length < 0 || tokenTypeIndex < 0 || modifierBits < 0 {
		return DecodedSemanticToken{}, fmt.Errorf("semantic token data contains negative value")
	}
	if deltaLine == 0 {
		state.start += deltaStart
	} else {
		state.line += deltaLine
		state.start = deltaStart
	}
	return DecodedSemanticToken{
		Line:           state.line,
		StartCharacter: state.start,
		Length:         length,
		TokenType:      semanticTokenTypeName(tokenTypeIndex, legend.TokenTypes),
		TokenModifiers: decodeTokenModifiers(modifierBits, legend.TokenModifiers),
	}, nil
}

func semanticTokenTypeName(index int, tokenTypes []string) string {
	if index >= 0 && index < len(tokenTypes) {
		return tokenTypes[index]
	}
	return fmt.Sprintf("unknown(%d)", index)
}

func decodeTokenModifiers(bits int, modifierNames []string) []string {
	if bits == 0 || len(modifierNames) == 0 {
		return nil
	}

	out := make([]string, 0, len(modifierNames))
	for i, name := range modifierNames {
		if bits&(1<<i) != 0 {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// decodeFoldingRanges 兼容解码:
// []FoldingRange | null，并做空值与边界过滤。
func decodeFoldingRanges(raw json.RawMessage) ([]FoldingRange, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var ranges []FoldingRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return nil, fmt.Errorf("decode folding ranges: %w", err)
	}

	out := make([]FoldingRange, 0, len(ranges))
	for _, item := range ranges {
		if !validFoldingRange(item) {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func validFoldingRange(item FoldingRange) bool {
	if item.StartLine < 0 || item.EndLine < 0 || item.EndLine < item.StartLine {
		return false
	}
	if item.StartCharacter != nil && *item.StartCharacter < 0 {
		return false
	}
	if item.EndCharacter != nil && *item.EndCharacter < 0 {
		return false
	}
	if item.StartLine == item.EndLine && item.StartCharacter != nil && item.EndCharacter != nil && *item.EndCharacter < *item.StartCharacter {
		return false
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
