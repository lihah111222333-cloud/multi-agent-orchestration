package lsp

import "encoding/json"

type (
	Request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	Response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *int            `json:"id,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *ResponseError  `json:"error,omitempty"`
	}
	ResponseError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	Notification struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	Position struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	}
	Range struct {
		Start Position `json:"start"`
		End   Position `json:"end"`
	}
	Location struct {
		URI   string `json:"uri"`
		Range Range  `json:"range"`
	}
	TextDocumentIdentifier struct {
		URI string `json:"uri"`
	}
	TextDocumentItem struct {
		URI        string `json:"uri"`
		LanguageID string `json:"languageId"`
		Version    int    `json:"version"`
		Text       string `json:"text"`
	}
	TextDocumentPositionParams struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}
)

type (
	InitializeParams struct {
		ProcessID    int                `json:"processId"`
		RootURI      string             `json:"rootUri"`
		Capabilities ClientCapabilities `json:"capabilities"`
	}
	ClientCapabilities struct {
		TextDocument *TextDocumentClientCapabilities `json:"textDocument,omitempty"`
	}
	DynamicRegistrationCapability struct {
		DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	}
	TextDocumentClientCapabilities struct {
		PublishDiagnostics *PublishDiagnosticsCapability `json:"publishDiagnostics,omitempty"`
		Hover              *HoverCapability              `json:"hover,omitempty"`
		Completion         *CompletionClientCapability   `json:"completion,omitempty"`
		Rename             *RenameClientCapability       `json:"rename,omitempty"`
		CallHierarchy      *CallHierarchyCapability      `json:"callHierarchy,omitempty"`
		TypeHierarchy      *TypeHierarchyCapability      `json:"typeHierarchy,omitempty"`
		CodeAction         *CodeActionCapability         `json:"codeAction,omitempty"`
		SignatureHelp      *SignatureHelpCapability      `json:"signatureHelp,omitempty"`
		Formatting         *FormattingCapability         `json:"formatting,omitempty"`
		FoldingRange       *FoldingRangeCapability       `json:"foldingRange,omitempty"`
		SemanticTokens     *SemanticTokensCapability     `json:"semanticTokens,omitempty"`
	}
	PublishDiagnosticsCapability struct {
		RelatedInformation bool `json:"relatedInformation,omitempty"`
	}
	HoverCapability struct {
		ContentFormat []string `json:"contentFormat,omitempty"`
	}
	RenameClientCapability struct {
		PrepareSupport bool `json:"prepareSupport,omitempty"`
	}
	SemanticTokensCapability struct {
		DynamicRegistration bool                              `json:"dynamicRegistration,omitempty"`
		Requests            *SemanticTokensRequestsCapability `json:"requests,omitempty"`
		TokenTypes          []string                          `json:"tokenTypes,omitempty"`
		TokenModifiers      []string                          `json:"tokenModifiers,omitempty"`
		Formats             []string                          `json:"formats,omitempty"`
	}
	SemanticTokensRequestsCapability struct {
		Range any `json:"range,omitempty"` // bool | object
		Full  any `json:"full,omitempty"`  // bool | object
	}
	SemanticTokensFullRequestsCapability struct {
		Delta bool `json:"delta,omitempty"`
	}
	InitializeResult struct {
		Capabilities ServerCapabilities `json:"capabilities"`
	}
	ServerCapabilities struct {
		TextDocumentSync           any `json:"textDocumentSync,omitempty"`
		HoverProvider              any `json:"hoverProvider,omitempty"`
		DefinitionProvider         any `json:"definitionProvider,omitempty"`
		ReferencesProvider         any `json:"referencesProvider,omitempty"`
		DocumentSymbolProvider     any `json:"documentSymbolProvider,omitempty"`
		RenameProvider             any `json:"renameProvider,omitempty"` // bool 或 RenameOptions
		DiagnosticProvider         any `json:"diagnosticProvider,omitempty"`
		CompletionProvider         any `json:"completionProvider,omitempty"`
		WorkspaceSymbolProvider    any `json:"workspaceSymbolProvider,omitempty"`
		ImplementationProvider     any `json:"implementationProvider,omitempty"`
		TypeDefinitionProvider     any `json:"typeDefinitionProvider,omitempty"`
		CallHierarchyProvider      any `json:"callHierarchyProvider,omitempty"`
		TypeHierarchyProvider      any `json:"typeHierarchyProvider,omitempty"`
		CodeActionProvider         any `json:"codeActionProvider,omitempty"`
		SignatureHelpProvider      any `json:"signatureHelpProvider,omitempty"`
		DocumentFormattingProvider any `json:"documentFormattingProvider,omitempty"`
		FoldingRangeProvider       any `json:"foldingRangeProvider,omitempty"`
		SemanticTokensProvider     any `json:"semanticTokensProvider,omitempty"`
	}
)

type (
	CompletionClientCapability = DynamicRegistrationCapability
	CallHierarchyCapability    = DynamicRegistrationCapability
	TypeHierarchyCapability    = DynamicRegistrationCapability
	CodeActionCapability       = DynamicRegistrationCapability
	SignatureHelpCapability    = DynamicRegistrationCapability
	FormattingCapability       = DynamicRegistrationCapability
	FoldingRangeCapability     = DynamicRegistrationCapability
)

type (
	DidOpenTextDocumentParams struct {
		TextDocument TextDocumentItem `json:"textDocument"`
	}
	DidCloseTextDocumentParams struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}
	DidChangeTextDocumentParams struct {
		TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
		ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
	}
	VersionedTextDocumentIdentifier struct {
		URI     string `json:"uri"`
		Version int    `json:"version"`
	}
	TextDocumentContentChangeEvent struct {
		Text string `json:"text"` // 全量替换
	}
)

type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

func (s DiagnosticSeverity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInformation:
		return "info"
	case SeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
	Code     any                `json:"code,omitempty"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"` // "plaintext" | "markdown"
	Value string `json:"value"`
}

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type SymbolKind int

const (
	SKFile          SymbolKind = 1
	SKModule        SymbolKind = 2
	SKNamespace     SymbolKind = 3
	SKPackage       SymbolKind = 4
	SKClass         SymbolKind = 5
	SKMethod        SymbolKind = 6
	SKProperty      SymbolKind = 7
	SKField         SymbolKind = 8
	SKConstructor   SymbolKind = 9
	SKEnum          SymbolKind = 10
	SKInterface     SymbolKind = 11
	SKFunction      SymbolKind = 12
	SKVariable      SymbolKind = 13
	SKConstant      SymbolKind = 14
	SKString        SymbolKind = 15
	SKNumber        SymbolKind = 16
	SKBoolean       SymbolKind = 17
	SKArray         SymbolKind = 18
	SKObject        SymbolKind = 19
	SKKey           SymbolKind = 20
	SKNull          SymbolKind = 21
	SKEnumMember    SymbolKind = 22
	SKStruct        SymbolKind = 23
	SKEvent         SymbolKind = 24
	SKOperator      SymbolKind = 25
	SKTypeParameter SymbolKind = 26
)

var symbolKindNames = [...]string{
	"", "file", "module", "namespace", "package", "class", "method", "property", "field", "constructor",
	"enum", "interface", "function", "variable", "constant", "string", "number", "boolean", "array", "object",
	"key", "null", "enum_member", "struct", "event", "operator", "type_parameter",
}

func (k SymbolKind) String() string {
	i := int(k)
	if i >= 0 && i < len(symbolKindNames) && symbolKindNames[i] != "" {
		return symbolKindNames[i]
	}
	return "unknown"
}

type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Documentation any    `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type (
	HoverParams          = TextDocumentPositionParams
	DefinitionParams     = TextDocumentPositionParams
	DocumentSymbolParams = DidCloseTextDocumentParams
	CompletionParams     = TextDocumentPositionParams
)

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`         // URI → edits
	DocumentChanges []TextDocumentEdit    `json:"documentChanges,omitempty"` // 更常见于 gopls
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type TextDocumentEdit struct {
	TextDocument OptionalVersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                              `json:"edits"`
}

type OptionalVersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int   `json:"version,omitempty"`
}
