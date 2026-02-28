package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type (
	LocationLink struct {
		OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
		TargetURI            string `json:"targetUri"`
		TargetRange          Range  `json:"targetRange"`
		TargetSelectionRange Range  `json:"targetSelectionRange"`
	}
	Command struct {
		Title     string `json:"title"`
		Command   string `json:"command"`
		Arguments []any  `json:"arguments,omitempty"`
	}
	SymbolInformation struct {
		Name          string     `json:"name"`
		Kind          SymbolKind `json:"kind"`
		Location      Location   `json:"location"`
		ContainerName string     `json:"containerName,omitempty"`
	}
	WorkspaceSymbolLocation struct {
		URI string `json:"uri"`
	}
	WorkspaceSymbol struct {
		Name          string `json:"name"`
		Kind          int    `json:"kind"`
		Location      any    `json:"location,omitempty"` // Location | WorkspaceSymbolLocation
		ContainerName string `json:"containerName,omitempty"`
		Data          any    `json:"data,omitempty"`
	}
	WorkspaceSymbolParams struct {
		Query string `json:"query"`
	}
	CodeAction struct {
		Title       string         `json:"title"`
		Kind        string         `json:"kind,omitempty"`
		Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
		Edit        *WorkspaceEdit `json:"edit,omitempty"`
		Command     *Command       `json:"command,omitempty"`
		Data        any            `json:"data,omitempty"`
	}
	CodeActionContext struct {
		Diagnostics []Diagnostic `json:"diagnostics"`
		Only        []string     `json:"only,omitempty"`
		TriggerKind int          `json:"triggerKind,omitempty"`
	}
	CodeActionParams struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Range        Range                  `json:"range"`
		Context      CodeActionContext      `json:"context"`
	}
	CallHierarchyItem struct {
		Name           string `json:"name"`
		Kind           int    `json:"kind"`
		URI            string `json:"uri"`
		Range          Range  `json:"range"`
		SelectionRange Range  `json:"selectionRange"`
		Data           any    `json:"data,omitempty"`
	}
	TypeHierarchyItem struct {
		Name           string `json:"name"`
		Kind           int    `json:"kind"`
		URI            string `json:"uri"`
		Range          Range  `json:"range"`
		SelectionRange Range  `json:"selectionRange"`
		Data           any    `json:"data,omitempty"`
	}
	SemanticTokensLegend struct {
		TokenTypes     []string `json:"tokenTypes"`
		TokenModifiers []string `json:"tokenModifiers"`
	}
	SemanticTokensOptions struct {
		Legend SemanticTokensLegend `json:"legend"`
	}
	LocationResult struct {
		Location     *Location     `json:"location,omitempty"`
		LocationLink *LocationLink `json:"locationLink,omitempty"`
		Canonical    *Location     `json:"canonical,omitempty"`
	}
	WorkspaceSymbolResult struct {
		SymbolInformation *SymbolInformation `json:"symbolInformation,omitempty"`
		WorkspaceSymbol   *WorkspaceSymbol   `json:"workspaceSymbol,omitempty"`
	}
	CodeActionResult struct {
		CodeAction *CodeAction `json:"codeAction,omitempty"`
		Command    *Command    `json:"command,omitempty"`
	}
)

func (r LocationResult) PrimaryLocation() *Location {
	if r.Location != nil {
		return r.Location
	}
	return r.Canonical
}

func isNullRaw(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null"
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

func decodeArrayLike[T any](
	raw json.RawMessage,
	errPrefix string,
	allowSingle bool,
	decodeOne func(json.RawMessage) (T, error),
) ([]T, error) {
	if isNullRaw(raw) {
		return nil, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]T, 0, len(arr))
		for _, item := range arr {
			decoded, err := decodeOne(item)
			if err != nil {
				return nil, err
			}
			out = append(out, decoded)
		}
		return out, nil
	} else if !allowSingle {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	one, err := decodeOne(raw)
	if err != nil {
		return nil, err
	}
	return []T{one}, nil
}

func decodeRawObject(item json.RawMessage, errPrefix string) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(item, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return obj, nil
}

func decodeLocationsLike(raw json.RawMessage) ([]LocationResult, error) {
	return decodeArrayLike(raw, "decode location-like", true, decodeLocationLikeOne)
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

func decodeWorkspaceSymbolOne(item json.RawMessage) (WorkspaceSymbolResult, error) {
	obj, err := decodeRawObject(item, "decode workspace symbol item")
	if err != nil {
		return WorkspaceSymbolResult{}, err
	}

	if locationRaw := obj["location"]; len(locationRaw) > 0 {
		var locObj map[string]json.RawMessage
		if err := json.Unmarshal(locationRaw, &locObj); err == nil && locObj["range"] != nil {
			var legacy SymbolInformation
			if err := json.Unmarshal(item, &legacy); err != nil {
				return WorkspaceSymbolResult{}, fmt.Errorf("decode SymbolInformation: %w", err)
			}
			return WorkspaceSymbolResult{SymbolInformation: &legacy}, nil
		}
	}

	var modern WorkspaceSymbol
	if err := json.Unmarshal(item, &modern); err != nil {
		return WorkspaceSymbolResult{}, fmt.Errorf("decode WorkspaceSymbol: %w", err)
	}
	return WorkspaceSymbolResult{WorkspaceSymbol: &modern}, nil
}

func decodeCodeActions(raw json.RawMessage) ([]CodeActionResult, error) {
	return decodeArrayLike(raw, "decode code actions", false, decodeCodeActionOne)
}

func decodeCodeActionOne(item json.RawMessage) (CodeActionResult, error) {
	obj, err := decodeRawObject(item, "decode code action item")
	if err != nil {
		return CodeActionResult{}, err
	}
	return decodeCodeActionOrCommand(item, obj)
}

func decodeCodeActionOrCommand(item json.RawMessage, obj map[string]json.RawMessage) (CodeActionResult, error) {
	var cmd Command
	if !isCodeActionLike(obj) {
		if err := json.Unmarshal(item, &cmd); err == nil && strings.TrimSpace(cmd.Command) != "" {
			return CodeActionResult{Command: &cmd}, nil
		}
	}

	var action CodeAction
	if err := json.Unmarshal(item, &action); err == nil {
		return CodeActionResult{CodeAction: &action}, nil
	}

	if err := json.Unmarshal(item, &cmd); err == nil {
		return CodeActionResult{Command: &cmd}, nil
	}

	return CodeActionResult{}, fmt.Errorf("decode code action item: unsupported payload")
}

func isCodeActionLike(obj map[string]json.RawMessage) bool {
	if obj["kind"] != nil || obj["edit"] != nil || obj["diagnostics"] != nil ||
		obj["isPreferred"] != nil || obj["disabled"] != nil || obj["data"] != nil {
		return true
	}
	if commandRaw, hasCommand := obj["command"]; hasCommand {
		var commandName string
		if err := json.Unmarshal(commandRaw, &commandName); err == nil {
			return false
		}
		return true
	}
	return false
}

func decodeCompletionItems(raw json.RawMessage) ([]CompletionItem, error) {
	var list CompletionList
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		return list.Items, nil
	}

	items, err := decodeNullableSlice[CompletionItem](raw, "decode completion")
	if err == nil {
		return items, nil
	}

	return nil, fmt.Errorf("decode completion: unsupported payload")
}

func decodeDocumentSymbolOne(item json.RawMessage) (DocumentSymbol, error) {
	obj, err := decodeRawObject(item, "decode document symbol item")
	if err != nil {
		return DocumentSymbol{}, err
	}

	if obj["location"] != nil {
		var legacy SymbolInformation
		if err := json.Unmarshal(item, &legacy); err != nil {
			return DocumentSymbol{}, fmt.Errorf("decode legacy document symbol: %w", err)
		}
		return DocumentSymbol{
			Name:           legacy.Name,
			Kind:           legacy.Kind,
			Range:          legacy.Location.Range,
			SelectionRange: legacy.Location.Range,
		}, nil
	}

	var symbol DocumentSymbol
	if err := json.Unmarshal(item, &symbol); err != nil {
		return DocumentSymbol{}, fmt.Errorf("decode document symbol: %w", err)
	}
	return symbol, nil
}

func decodePrepareCallHierarchyItems(raw json.RawMessage) ([]CallHierarchyItem, error) {
	return decodeNullableSlice[CallHierarchyItem](raw, "decode prepareCallHierarchy")
}

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

type (
	PrepareCallHierarchyParams       = TextDocumentPositionParams
	CallHierarchyIncomingCallsParams = itemRequest[CallHierarchyItem]
	CallHierarchyOutgoingCallsParams = itemRequest[CallHierarchyItem]
)

type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}

type (
	PrepareTypeHierarchyParams    = TextDocumentPositionParams
	TypeHierarchySupertypesParams = itemRequest[TypeHierarchyItem]
	TypeHierarchySubtypesParams   = itemRequest[TypeHierarchyItem]
)

type CallHierarchyResult struct {
	Item     CallHierarchyItem           `json:"item"`
	Incoming []CallHierarchyIncomingCall `json:"incoming,omitempty"`
	Outgoing []CallHierarchyOutgoingCall `json:"outgoing,omitempty"`
}

type TypeHierarchyResult struct {
	Item       TypeHierarchyItem   `json:"item"`
	Supertypes []TypeHierarchyItem `json:"supertypes,omitempty"`
	Subtypes   []TypeHierarchyItem `json:"subtypes,omitempty"`
}

const XRefResultLimit = 50

type SignatureHelpParams = TextDocumentPositionParams

type SignatureHelpResult struct {
	Signatures      []SignatureInformationResult `json:"signatures,omitempty"`
	ActiveSignature *int                         `json:"activeSignature,omitempty"`
	ActiveParameter *int                         `json:"activeParameter,omitempty"`
}

type SignatureInformationResult struct {
	Label             string                       `json:"label"`
	Documentation     string                       `json:"documentation,omitempty"`
	DocumentationKind string                       `json:"documentationKind,omitempty"`
	Parameters        []ParameterInformationResult `json:"parameters,omitempty"`
}

type ParameterInformationResult struct {
	Label             string `json:"label,omitempty"`
	LabelOffsets      []int  `json:"labelOffsets,omitempty"`
	Documentation     string `json:"documentation,omitempty"`
	DocumentationKind string `json:"documentationKind,omitempty"`
}

type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

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
		signatures, err := decodeArrayLike(signaturesRaw, "decode signatureHelp signatures", false, decodeSignatureInformation)
		if err != nil {
			return nil, err
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
	obj, err := decodeRawObject(raw, "decode signature info")
	if err != nil {
		return SignatureInformationResult{}, err
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
		parameters, err := decodeArrayLike(parametersRaw, "decode signature parameters", false, decodeParameterInformation)
		if err != nil {
			return SignatureInformationResult{}, err
		}
		result.Parameters = parameters
	}
	return result, nil
}

func decodeParameterInformation(raw json.RawMessage) (ParameterInformationResult, error) {
	obj, err := decodeRawObject(raw, "decode signature parameter")
	if err != nil {
		return ParameterInformationResult{}, err
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

func decodeTextEdits(raw json.RawMessage) ([]TextEdit, error) {
	return decodeNullableSlice[TextEdit](raw, "decode text edits")
}

const SemanticTokenResultLimit = 200

type SemanticTokensParams = DocumentSymbolParams

type SemanticTokens struct {
	ResultID string `json:"resultId,omitempty"`
	Data     []int  `json:"data"`
}

type DecodedSemanticToken struct {
	Line           int      `json:"line"`
	StartCharacter int      `json:"startCharacter"`
	Length         int      `json:"length"`
	TokenType      string   `json:"tokenType"`
	TokenModifiers []string `json:"tokenModifiers,omitempty"`
}

type SemanticTokensResult struct {
	ResultID string                 `json:"resultId,omitempty"`
	Data     []int                  `json:"data,omitempty"`
	Decoded  []DecodedSemanticToken `json:"decoded,omitempty"`
}

type FoldingRangeParams = DocumentSymbolParams

type FoldingRange struct {
	StartLine      int    `json:"startLine"`
	StartCharacter *int   `json:"startCharacter,omitempty"`
	EndLine        int    `json:"endLine"`
	EndCharacter   *int   `json:"endCharacter,omitempty"`
	Kind           string `json:"kind,omitempty"`
	CollapsedText  string `json:"collapsedText,omitempty"`
}

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

	out := make([]DecodedSemanticToken, 0, len(data)/5)
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
	if bits == 0 {
		return nil
	}

	var out []string
	for i, name := range modifierNames {
		if bits&(1<<i) != 0 {
			out = append(out, name)
		}
	}
	return out
}

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
