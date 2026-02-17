// Package lsp 提供零依赖 LSP 客户端，通过 JSON-RPC 2.0 over stdio 与语言服务器通信。
//
// 仅实现 LSP 3.17 协议子集:
//   - lifecycle: initialize / initialized / shutdown / exit
//   - document sync: didOpen / didClose / didChange
//   - diagnostics: textDocument/publishDiagnostics (server→client notification)
//   - hover: textDocument/hover (请求/响应)
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
	TextDocumentSync   any  `json:"textDocumentSync,omitempty"`
	HoverProvider      bool `json:"hoverProvider,omitempty"`
	DefinitionProvider bool `json:"definitionProvider,omitempty"`
	DiagnosticProvider any  `json:"diagnosticProvider,omitempty"`
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
