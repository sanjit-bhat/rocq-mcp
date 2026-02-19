# Implementation Plan

## Phase 1: Foundation
- [x] Initialize Go module, add MCP SDK dependency
- [x] Implement LSP JSON-RPC codec (Content-Length framing, encode/decode)
- [x] Unit test: codec round-trip (serialize → deserialize)

## Phase 2: vsrocq Client
- [x] Implement vsrocqtop subprocess management (spawn, pipes, shutdown)
- [x] LSP initialization handshake (initialize, initialized, didChangeConfiguration)
- [x] Async notification dispatcher (route proofView, diagnostics, etc.)
- [x] Handle workspace/configuration server→client requests
- [x] Integration test: spawn vsrocqtop, initialize, shut down cleanly

## Phase 3: Document Management
- [x] Per-document state (version counter, diagnostics, proofView)
- [x] didOpen / didClose / didChange
- [x] Sync from disk (re-read file, send didChange)
- [x] Integration test: open a trivial .v file, get no errors

## Phase 4: Core Proof Tools (Tier 1 MCP)
- [x] MCP server setup with tool registration
- [x] rocq_open / rocq_close / rocq_sync tools
- [x] rocq_check (interpretToPoint → wait for proofView + diagnostics)
- [x] rocq_check_all (interpretToEnd)
- [x] rocq_step_forward / rocq_step_backward
- [x] Ppcmd pretty-printer rendering for proof goals
- [x] Integration test: open file mid-proof, check, verify goals returned
- [x] Integration test: file with error returns diagnostics
- [x] End-to-end test: talk MCP over stdio, call tools, verify results

## Phase 5: Proof View Deltas
- [x] Track previous ProofView per document in docState
- [x] Diff current vs previous ProofView in formatResults
- [x] Default: return only focused goal (goal 1) with full detail
- [x] Default: return deltas (new/removed hypotheses, changed conclusion)
- [x] Summarize non-focused goals as count + conclusions only
- [x] rocq_get_proof_state tool: return full proof state on demand
- [x] Tests: verify delta output, verify full state tool

## Refactor: File Organization
- [x] Extract shared types to `types.go`
- [x] Extract proof operations to `proof.go`
- [x] Extract formatting/rendering to `format.go`
- [x] Slim `state.go` to stateManager + document lifecycle
- [x] Slim `tools.go` to MCP registration + helpers

## Phase 6: Query Tools (Tier 2)
- [x] rocq_about / rocq_check_type / rocq_locate / rocq_print / rocq_search

## Phase 7: Diagnostics & State (Tier 3)
- [ ] rocq_reset / rocq_document_state / rocq_document_proofs

## MVP milestone: Phase 1–4 complete, can interactively prove a .v file via MCP.
