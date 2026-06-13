---
name: update-arch-doc
description: Regenerate docs/architecture.md to match the current vsrocq Go wrapper implementation. Use after any structural change to client.go, codec.go, or protocol.go.
---

Regenerate `docs/architecture.md` so it accurately reflects the current implementation.

## Steps

1. **Read the current sources**:
   - `vsrocq/client.go` — `Client` struct fields, `Start`, `Shutdown`, `readLoop`, `handleNotification`, `notify`, all public methods
   - `vsrocq/codec.go` — `lspWriter`, `readLSPFrame`, `unwrapArrayParams`
   - `vsrocq/protocol.go` — notification channel types, request/result types
   - `docs/architecture.md` — the existing doc (to understand what needs updating)

2. **Identify what has changed** by comparing the sources to the existing doc. Look for:
   - Struct fields added, removed, or renamed (especially notification channels)
   - New or removed public methods
   - Changes to the transport layer (framing, params rewriting, pipe bridge)
   - Changes to thread-safety guarantees or the notify() path
   - Changes to the key design notes table

3. **Rewrite `docs/architecture.md`** with three Mermaid diagrams and a key design notes table that match the current code:

   ### Component overview diagram
   A `graph TD` showing:
   - The Go process box containing the `vsrocq.Client` subgraph with: `rpc.Client`, `lspWriter`, `readLoop goroutine`, and the notification channels (list all 7 by name)
   - The `vsrocqtop` process box with: `Sel event loop`, `handle_lsp_event / dispatch_request`, `DocumentManager`
   - All data-flow edges with accurate labels (Content-Length framing, JSON-RPC format, pipe bridge, non-blocking channel sends)

   ### Request / response sequence diagram
   A `sequenceDiagram` with participants: `Caller`, `rpc.Client`, `lspWriter`, `vsrocqtop`, `readLoop`. Trace one `CallContext` call from the Go caller through to the unmarshalled result, including the params-array-unwrap step and the io.Pipe forwarding.

   ### Notification flow sequence diagram
   A `sequenceDiagram` with participants: `vsrocqtop`, `readLoop`, `Channels`. Trace one server notification from stdout through `readLSPFrame`, the peek/classify step, `handleNotification`, and the non-blocking channel send.

   ### Key design notes table
   Two columns: **Concern** | **Mechanism**. Cover:
   - Request–response correlation
   - Context cancellation
   - LSP Content-Length framing
   - Params format mismatch (`unwrapArrayParams`)
   - Notification dispatch (classify by `method≠"" && id==nil`)
   - Thread safety (who serialises stdin writes)
   - Fire-and-forget notifications (`notify()` path)
   - `workers: int option` invariant (`omitempty` absence)

4. **Write the updated file** to `docs/architecture.md`. Keep the same four-section structure (component overview, request/response flow, notification flow, key design notes).
