package lsp

// PrepareCallHierarchyParams textDocument/prepareCallHierarchy 请求参数。
type PrepareCallHierarchyParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// CallHierarchyIncomingCallsParams callHierarchy/incomingCalls 请求参数。
type CallHierarchyIncomingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyOutgoingCallsParams callHierarchy/outgoingCalls 请求参数。
type CallHierarchyOutgoingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

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
type PrepareTypeHierarchyParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// TypeHierarchySupertypesParams typeHierarchy/supertypes 请求参数。
type TypeHierarchySupertypesParams struct {
	Item TypeHierarchyItem `json:"item"`
}

// TypeHierarchySubtypesParams typeHierarchy/subtypes 请求参数。
type TypeHierarchySubtypesParams struct {
	Item TypeHierarchyItem `json:"item"`
}

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
