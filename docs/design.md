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

**`rocq_open(file: string)`**
Open a .v file in vsrocq via `textDocument/didOpen`. Returns confirmation.

**`rocq_check(file: string, line: int, col: int)`**
Send `prover/interpretToPoint` for the given position. Wait for `prover/proofView`
and `textDocument/publishDiagnostics`. Return proof goals + any errors/warnings.

**`rocq_step_forward(file: string)` / `rocq_step_backward(file: string)`**
Step one sentence forward/backward. Return updated proof goals.

**`rocq_close(file: string)`**
Send `textDocument/didClose`. Clean up state.

Note: file edits are handled by Claude's existing Edit/Write tools. The MCP server
sends `textDocument/didChange` notifications to vsrocq when files change.
This requires the MCP server to detect file changes — either:
- (a) Claude calls a `rocq_sync(file)` tool after editing, or
- (b) the MCP server watches the file with fsnotify.
Option (a) is simpler and more explicit.

**`rocq_sync(file: string)`**
Re-read the file from disk and send `textDocument/didChange` to vsrocq.

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
