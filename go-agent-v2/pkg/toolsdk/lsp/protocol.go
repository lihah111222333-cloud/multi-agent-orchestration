// Package lsp 提供零依赖 LSP 客户端，通过 JSON-RPC 2.0 over stdio 与语言服务器通信。
//
// 实现 LSP 3.17 协议子集:
//   - lifecycle: initialize / initialized / shutdown / exit
//   - document sync: didOpen / didClose / didChange
//   - diagnostics: textDocument/publishDiagnostics (server→client notification)
//   - hover: textDocument/hover (请求/响应)
//   - definition: textDocument/definition (跳转定义)
//   - references: textDocument/references (查找引用)
//   - documentSymbol: textDocument/documentSymbol (文件大纲)
//   - completion: textDocument/completion (代码补全)
//   - rename: textDocument/rename (重命名)
package lsp

import "encoding/json"

// ========================================
// JSON-RPC 2.0 基础类型
// ========================================

// Request JSON-RPC 2.0 请求。
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response JSON-RPC 2.0 响应。
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError JSON-RPC 2.0 错误。
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Notification JSON-RPC 2.0 通知 (无 ID)。
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ========================================
// LSP 基础类型
// ========================================

// Position 文档位置 (0-indexed)。
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range 文档范围。
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location 文件位置。
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier 文档标识。
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentItem 文档完整内容。
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentPositionParams 文档位置参数 (hover / definition)。
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// ========================================
// Initialize
// ========================================

// InitializeParams initialize 请求参数。
type InitializeParams struct {
	ProcessID    int                `json:"processId"`
	RootURI      string             `json:"rootUri"`
	Capabilities ClientCapabilities `json:"capabilities"`
}

// ClientCapabilities 客户端能力声明。
type ClientCapabilities struct {
	TextDocument *TextDocumentClientCapabilities `json:"textDocument,omitempty"`
}

// TextDocumentClientCapabilities 文档级能力。
type TextDocumentClientCapabilities struct {
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

// PublishDiagnosticsCapability 诊断能力。
type PublishDiagnosticsCapability struct {
	RelatedInformation bool `json:"relatedInformation,omitempty"`
}

// HoverCapability hover 能力。
type HoverCapability struct {
	ContentFormat []string `json:"contentFormat,omitempty"`
}

// CompletionClientCapability completion 能力。
type CompletionClientCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// RenameClientCapability rename 能力。
type RenameClientCapability struct {
	PrepareSupport bool `json:"prepareSupport,omitempty"`
}

// CallHierarchyCapability call hierarchy 能力。
type CallHierarchyCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// TypeHierarchyCapability type hierarchy 能力。
type TypeHierarchyCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// CodeActionCapability code action 能力。
type CodeActionCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// SignatureHelpCapability signature help 能力。
type SignatureHelpCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// FormattingCapability formatting 能力。
type FormattingCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// FoldingRangeCapability folding range 能力。
type FoldingRangeCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// SemanticTokensCapability semantic tokens 能力。
type SemanticTokensCapability struct {
	DynamicRegistration bool                              `json:"dynamicRegistration,omitempty"`
	Requests            *SemanticTokensRequestsCapability `json:"requests,omitempty"`
	TokenTypes          []string                          `json:"tokenTypes,omitempty"`
	TokenModifiers      []string                          `json:"tokenModifiers,omitempty"`
	Formats             []string                          `json:"formats,omitempty"`
}

// SemanticTokensRequestsCapability semantic tokens 请求能力。
// range/full 按 LSP 3.17 支持 bool 或对象联合类型。
type SemanticTokensRequestsCapability struct {
	Range any `json:"range,omitempty"` // bool | object
	Full  any `json:"full,omitempty"`  // bool | object
}

// SemanticTokensFullRequestsCapability semantic tokens full 请求能力。
type SemanticTokensFullRequestsCapability struct {
	Delta bool `json:"delta,omitempty"`
}

// InitializeResult initialize 响应结果。
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities 服务端能力。
type ServerCapabilities struct {
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

// ========================================
// Document Sync
// ========================================

// DidOpenTextDocumentParams textDocument/didOpen 通知参数。
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidCloseTextDocumentParams textDocument/didClose 通知参数。
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidChangeTextDocumentParams textDocument/didChange 通知参数。
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// VersionedTextDocumentIdentifier 带版本的文档标识。
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentContentChangeEvent 内容变更事件 (全量替换模式)。
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"` // 全量替换
}

// ========================================
// Diagnostics (server → client notification)
// ========================================

// DiagnosticSeverity 诊断严重级别。
type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

// String 返回级别名称。
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

// Diagnostic 单条诊断信息。
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
	Code     any                `json:"code,omitempty"`
}

// PublishDiagnosticsParams textDocument/publishDiagnostics 通知参数。
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ========================================
// Hover
// ========================================

// HoverParams textDocument/hover 请求参数。
type HoverParams = TextDocumentPositionParams

// HoverResult textDocument/hover 响应结果。
type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// MarkupContent 标记内容。
type MarkupContent struct {
	Kind  string `json:"kind"` // "plaintext" | "markdown"
	Value string `json:"value"`
}

// ========================================
// Definition (textDocument/definition)
// ========================================

// DefinitionParams textDocument/definition 请求参数。
type DefinitionParams = TextDocumentPositionParams

// ========================================
// References (textDocument/references)
// ========================================

// ReferenceParams textDocument/references 请求参数。
type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

// ReferenceContext references 上下文选项。
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ========================================
// Document Symbol (textDocument/documentSymbol)
// ========================================

// DocumentSymbolParams textDocument/documentSymbol 请求参数。
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DocumentSymbol 文档符号 (大纲节点, 支持嵌套)。
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolKind LSP 符号类型枚举。
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

// symbolKindNames 符号类型名称表。
var symbolKindNames = map[SymbolKind]string{
	SKFile: "file", SKModule: "module", SKNamespace: "namespace",
	SKPackage: "package", SKClass: "class", SKMethod: "method",
	SKProperty: "property", SKField: "field", SKConstructor: "constructor",
	SKEnum: "enum", SKInterface: "interface", SKFunction: "function",
	SKVariable: "variable", SKConstant: "constant", SKStruct: "struct",
	SKEvent: "event", SKOperator: "operator", SKTypeParameter: "type_parameter",
	SKString: "string", SKNumber: "number", SKBoolean: "boolean",
	SKArray: "array", SKObject: "object", SKKey: "key",
	SKNull: "null", SKEnumMember: "enum_member",
}

// String 返回符号类型名称。
func (k SymbolKind) String() string {
	if n, ok := symbolKindNames[k]; ok {
		return n
	}
	return "unknown"
}

// ========================================
// Completion (textDocument/completion)
// ========================================

// CompletionParams 补全请求参数。
type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// CompletionItem 补全项。
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Documentation any    `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

// CompletionList 补全列表。
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// ========================================
// Rename (textDocument/rename)
// ========================================

// RenameParams textDocument/rename 请求参数。
type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

// WorkspaceEdit 工作区编辑 (rename 返回值)。
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`         // URI → edits
	DocumentChanges []TextDocumentEdit    `json:"documentChanges,omitempty"` // 更常见于 gopls
}

// TextEdit 单条文本编辑。
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// TextDocumentEdit 文档级编辑集合。
type TextDocumentEdit struct {
	TextDocument OptionalVersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                              `json:"edits"`
}

// OptionalVersionedTextDocumentIdentifier 支持 version 为 null 或缺失。
type OptionalVersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int   `json:"version,omitempty"`
}
