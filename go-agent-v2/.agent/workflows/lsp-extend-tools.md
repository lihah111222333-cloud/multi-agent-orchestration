# LSP Extend Tools (Merged Model)

## Principle
All LSP capabilities are exposed through merged tools, not split per-method tools.

## Merged Actions
- `lsp_file`: open/change
- `lsp_inspect`: hover/diagnostics/signature_help
- `lsp_xref`: definition/references/implementation/type_definition/workspace_symbol
- `lsp_grep`: text_search/ast_search
- `lsp_structure`: document_symbol/call_hierarchy/type_hierarchy/semantic_tokens/folding_range
- `lsp_edit`: rename/code_action/format
- `lsp_completion`: completion lookup

## Constraints
- No legacy aliases.
- No duplicate schema names.
- Prompt references must use merged names only.
