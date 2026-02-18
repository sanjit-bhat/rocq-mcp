# Design: Rocq MCP Server

## Context

Claude currently has no way to interactively type-check Rocq proofs or inspect proof state.
The only option is building entire files, which is slow and gives no proof context.
This MCP server wraps the vsrocq language server to provide interactive proof tools.

## Architecture

```
Claude Code
  └─ [stdio/MCP] ─── rocq-mcp (Go binary)
                        └─ [stdio/JSON-RPC LSP] ─── vsrocqtop
```

Claude Code spawns rocq-mcp as an MCP server. rocq-mcp spawns vsrocqtop as a child process
and speaks LSP JSON-RPC over its stdin/stdout.

## vsrocq Protocol

vsrocq uses a **push model**:
- Client sends `prover/interpretToPoint` (notification) to check up to a position
- Server pushes `prover/proofView` (notification) with goals
- Server pushes standard LSP `textDocument/publishDiagnostics` with errors

The MCP server bridges this async model: send interpretToPoint, block on a channel
until proofView/diagnostics arrive, return the result.

Other useful commands: `prover/stepForward`, `prover/stepBackward`, `prover/interpretToEnd`.

## MCP Tools

Tools are organized into tiers. Tier 1 is the core proof interaction loop — implement
these first. Tier 2 adds query commands for exploring definitions and types. Tier 3
covers diagnostics and state inspection.

### Tier 1: Core Proof Interaction

**`rocq_open(file: string)`**
Open a .v file in vsrocq via `textDocument/didOpen`. Returns confirmation.

**`rocq_close(file: string)`**
Send `textDocument/didClose`. Clean up state.

**`rocq_sync(file: string)`**
Re-read the file from disk and send `textDocument/didChange` to vsrocq.
Required after Claude edits a file with Edit/Write tools.

**`rocq_check(file: string, line: int, col: int)`**
Send `prover/interpretToPoint` for the given position. Wait for `prover/proofView`
and `textDocument/publishDiagnostics`. Return proof goals + any errors/warnings.

**`rocq_check_all(file: string)`**
Send `prover/interpretToEnd`. Wait for final `prover/proofView` and diagnostics.
Return proof goals (if any remain) + all errors/warnings. Useful for checking
an entire file after edits.

**`rocq_step_forward(file: string)` / `rocq_step_backward(file: string)`**
Send `prover/stepForward` or `prover/stepBackward`. Return updated proof goals.

### Tier 2: Query Commands

These wrap vsrocq's query requests. All take `file`, `line`, `col`, and `pattern`
(a Rocq identifier or expression). The position provides the proof context for
name resolution. They return pretty-printed text from Rocq.

**`rocq_about(file: string, line: int, col: int, pattern: string)`**
`prover/about` — Get information about a name (type, where it's defined, its implicit
arguments). Equivalent to `About foo` in Rocq.

**`rocq_check_type(file: string, line: int, col: int, pattern: string)`**
`prover/check` — Type-check an expression and return its type. Equivalent to `Check foo`.

**`rocq_locate(file: string, line: int, col: int, pattern: string)`**
`prover/locate` — Find the fully qualified name and module path of an identifier.
Equivalent to `Locate foo`.

**`rocq_print(file: string, line: int, col: int, pattern: string)`**
`prover/print` — Print the full definition of a name. Equivalent to `Print foo`.

**`rocq_search(file: string, line: int, col: int, pattern: string)`**
`prover/search` — Search for lemmas matching a pattern. This is async in the vsrocq
protocol: the request returns immediately and results arrive via `prover/searchResult`
notifications. The MCP tool collects results for a bounded time and returns them.

### Tier 3: Diagnostics & State

**`rocq_reset(file: string)`**
`prover/resetRocq` — Reset the Rocq prover state for a document. Useful when the
prover gets into a bad state.

**`rocq_document_state(file: string)`**
`prover/documentState` — Return internal vsrocq document state as a string.
Useful for debugging.

**`rocq_document_proofs(file: string)`**
`prover/documentProofs` — Return the list of proof blocks in the document with their
ranges. Useful for navigating a file and understanding proof structure.

### vsrocq Server → Client Notifications (handled internally)

These are not exposed as MCP tools but are consumed by the MCP server internally:

- `prover/proofView` — proof goals + messages, delivered to waiting `rocq_check`/step calls
- `prover/updateHighlights` — processing progress, could be used for status feedback
- `prover/moveCursor` — cursor movement requests, not applicable in CLI context
- `prover/blockOnError` — error-blocking ranges, folded into diagnostics reporting
- `prover/debugMessage` — logged to stderr for debugging
- `prover/searchResult` — collected and returned by `rocq_search`

## State Management

The MCP server maintains:
- The vsrocqtop subprocess (spawned on first `rocq_open`, or at startup)
- Per-file: document version counter, last known diagnostics, last known proofView
- A channel/mutex per file to bridge async notifications to sync tool responses

## Configuration

In `.claude/settings.json`:
```json
{
  "mcpServers": {
    "rocq": {
      "command": "rocq-mcp",
      "args": ["-Q", "/path/to/project,ProjectName"],
      "env": {}
    }
  }
}
```

The rocq-mcp binary passes through Rocq load path flags (`-Q`, `-R`) to vsrocqtop.

## LSP Initialization

On startup, rocq-mcp:
1. Spawns `vsrocqtop` with appropriate flags
2. Sends `initialize` request with workspace root
3. Sends `initialized` notification
4. Sends `workspace/didChangeConfiguration` with settings (e.g. `vsrocq.proof.mode: "Manual"`)

Manual mode is important — we don't want vsrocq auto-checking on every change,
only when Claude explicitly asks via `rocq_check`.

## Implementation Language

Go, using:
- `github.com/modelcontextprotocol/go-sdk` (official MCP SDK) for the MCP server
- `os/exec` + stdin/stdout pipes for vsrocqtop subprocess
- Manual JSON-RPC encoding/decoding for LSP (or a lightweight LSP client library)

## File Structure

```
rocq-mcp/
  main.go          — entry point, arg parsing, MCP server setup
  lsp.go           — LSP JSON-RPC client (encode/decode, send/receive)
  vsrocq.go        — vsrocq-specific protocol (interpretToPoint, proofView handling)
  tools.go         — MCP tool definitions and handlers
  state.go         — per-document state tracking
  main_test.go     — integration tests
```

## Testing Plan

**Unit tests (no vsrocq):**
- LSP JSON-RPC codec: serialize/deserialize messages, Content-Length framing
- Document version tracking: increments, sync from disk

**Integration tests (with vsrocq):**
- Spawn vsrocqtop, initialize, open a trivial .v file, get no diagnostics
- Open a file with an error, get diagnostics
- Open a file mid-proof, interpretToPoint, receive proofView with expected goals
- Edit file (sync), re-check, verify updated results

**End-to-end test:**
- Spawn rocq-mcp binary, talk MCP over stdio pipes
- Call rocq_open, rocq_check, verify proof goals returned correctly

## Open Questions

1. How to determine the vsrocqtop binary path — hardcode, PATH lookup, or flag?
2. Should we support multiple vsrocqtop instances (one per project) or one shared instance?
3. Timeout behavior: how long to wait for proofView after interpretToPoint?
