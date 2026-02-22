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
}

// PublishDiagnosticsCapability 诊断能力。
type PublishDiagnosticsCapability struct {
	RelatedInformation bool `json:"relatedInformation,omitempty"`
}

// HoverCapability hover 能力。
type HoverCapability struct {
	ContentFormat []string `json:"contentFormat,omitempty"`
}

// InitializeResult initialize 响应结果。
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities 服务端能力。
type ServerCapabilities struct {
	TextDocumentSync       any  `json:"textDocumentSync,omitempty"`
	HoverProvider          bool `json:"hoverProvider,omitempty"`
	DefinitionProvider     bool `json:"definitionProvider,omitempty"`
	ReferencesProvider     bool `json:"referencesProvider,omitempty"`
	DocumentSymbolProvider bool `json:"documentSymbolProvider,omitempty"`
	RenameProvider         any  `json:"renameProvider,omitempty"` // bool 或 RenameOptions
	DiagnosticProvider     any  `json:"diagnosticProvider,omitempty"`
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
	Changes map[string][]TextEdit `json:"changes,omitempty"` // URI → edits
}

// TextEdit 单条文本编辑。
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}
