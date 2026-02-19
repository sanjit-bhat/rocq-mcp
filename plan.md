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
- [x] rocq_reset / rocq_document_proofs

## Phase 8: Git-diff based proof view diffing
- [x] Add renderProofText helper (renders ProofView as plain text for diffing)
- [x] Add diffText helper (shells out to `git diff --no-index --histogram`)
- [x] Rewrite formatDeltaResults to use line-level git diff
- [x] Remove set-based writeHypothesesDiff
- [x] Update tests for new diff format (including multi-line hypothesis test)

## Phase 8.5: Pre-render proof text, focused goal only
- [x] Flatten ProofView: remove ProofGoal, store GoalCount/GoalID/GoalText/Messages
- [x] Pre-render focused goal (Goals[0]) to text in parseProofView
- [x] Add renderGoalText helper, remove renderProofText
- [x] Simplify formatDeltaResults: diff GoalText, "New focused goal" annotation on ID change
- [x] Simplify formatFullResults: show focused goal + "N goals remaining"
- [x] Update format_test.go for new ProofView shape
- [x] Add testdata/split_goals.v and TestSplitGoals integration test

TODOs:
[ ] strengthen `split_goals.v` to test more complex goal flow.
    running a tactic on all (multiple) goals in focus.
    multi-level nested goals.
[ ] update CI to use `just`.
    installing `just` is a bit annoying. maybe use brew. maybe raw install.
[ ] update readme to explicitly mention goal diff'ing feature.
    this is an important optimization for LLM context management,
    and there's probably lots of room for improvement.
