# LSP Enhance Consolidate (P2 Final)

## Goal
Consolidate LSP dynamic tools to a production-ready merged interface with guardrails and regression checks.

## Final Tool Surface
- `lsp_file`
- `lsp_inspect`
- `lsp_xref`
- `lsp_grep`
- `lsp_structure`
- `lsp_edit`
- `lsp_completion`

## Runtime Rules
- No compatibility aliases.
- Unknown names must return `UNKNOWN_TOOL`.
- Runtime handlers may stay registered when no language server is available.
- Schema exposure in unavailable mode must include only `lsp_grep`.

## Diff Capture Rule
- Capture only when:
  - tool is `lsp_file`
  - `action=change`
  - `persist_to_disk=true`

## Guardrails
- P0 baseline checks for schema/runtime/prompt references.
- P1 adds search capabilities inside merged contract.
- P2 enforces merged schema and no legacy references.

## Verification
- `LSP_P0_MODE=post go test ./internal/tooladapter -run TestP0Post -count=1`
- `LSP_P0_MODE=post go test ./internal/apiserver -run TestP0Post -count=1`
- `go test ./internal/lsp`
- `go test ./internal/tools`
- `go test ./internal/tooladapter`
