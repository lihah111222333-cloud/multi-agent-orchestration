package tooladapter

import "encoding/json"

func (f *fakeLSPProvider) LSPFile(_ json.RawMessage) string {
	return "{\"ok\":true,\"tool\":\"lsp_file\"}"
}

func (f *fakeLSPProvider) LSPInspect(_ json.RawMessage) string {
	return "{\"ok\":true,\"tool\":\"lsp_inspect\"}"
}

func (f *fakeLSPProvider) LSPXRef(_ json.RawMessage) string {
	return "{\"ok\":true,\"tool\":\"lsp_xref\"}"
}

func (f *fakeLSPProvider) LSPGrep(_ json.RawMessage) string {
	return "{\"ok\":true,\"tool\":\"lsp_grep\"}"
}

func (f *fakeLSPProvider) LSPStructure(_ json.RawMessage) string {
	return "{\"ok\":true,\"tool\":\"lsp_structure\"}"
}

func (f *fakeLSPProvider) LSPEdit(_ json.RawMessage) string {
	return "{\"ok\":true,\"tool\":\"lsp_edit\"}"
}
