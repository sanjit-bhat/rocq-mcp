# Implementation Plan

## Phase 1: Foundation
- [ ] Initialize Go module, add MCP SDK dependency
- [ ] Implement LSP JSON-RPC codec (Content-Length framing, encode/decode)
- [ ] Unit test: codec round-trip (serialize → deserialize)

## Phase 2: vsrocq Client
- [ ] Implement vsrocqtop subprocess management (spawn, pipes, shutdown)
- [ ] LSP initialization handshake (initialize, initialized, didChangeConfiguration)
- [ ] Async notification dispatcher (route proofView, diagnostics, etc.)
- [ ] Integration test: spawn vsrocqtop, initialize, shut down cleanly

## Phase 3: Document Management
- [ ] Per-document state (version counter, diagnostics, proofView)
- [ ] didOpen / didClose / didChange
- [ ] Sync from disk (re-read file, send didChange)
- [ ] Integration test: open a trivial .v file, get no errors

## Phase 4: Core Proof Tools (Tier 1 MCP)
- [ ] MCP server setup with tool registration
- [ ] rocq_open / rocq_close / rocq_sync tools
- [ ] rocq_check (interpretToPoint → wait for proofView + diagnostics)
- [ ] rocq_check_all (interpretToEnd)
- [ ] rocq_step_forward / rocq_step_backward
- [ ] Integration test: open file mid-proof, check, verify goals returned
- [ ] Integration test: file with error returns diagnostics
- [ ] End-to-end test: talk MCP over stdio, call tools, verify results

## Phase 5: Query Tools (Tier 2)
- [ ] rocq_about / rocq_check_type / rocq_locate / rocq_print / rocq_search

## Phase 6: Diagnostics & State (Tier 3)
- [ ] rocq_reset / rocq_document_state / rocq_document_proofs

## MVP milestone: Phase 1–4 complete, can interactively prove a .v file via MCP.
