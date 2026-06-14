# vsrocq LSP Protocol

## Client → Server

### Requests (client expects a response)

| Method | Description |
|---|---|
| `initialize` | Start session, negotiate capabilities |
| `shutdown` | Shut down the server |
| `textDocument/completion` | Get completion items at position |
| `textDocument/definition` | Jump to definition |
| `textDocument/hover` | Hover info at position |
| `textDocument/documentSymbol` | List document symbols |
| `prover/resetRocq` | Reset Rocq state for a document |
| `prover/search` | Search for theorems/definitions |
| `prover/about` | Print info about a term |
| `prover/check` | Type-check an expression |
| `prover/locate` | Locate a name |
| `prover/print` | Print a definition |
| `prover/documentState` | Dump internal document state (debug) |
| `prover/documentProofs` | List all proofs in a document |

### Notifications (client sends, no response)

| Method | Description |
|---|---|
| `textDocument/didOpen` | Document opened |
| `textDocument/didChange` | Document text changed |
| `textDocument/didClose` | Document closed |
| `textDocument/didSave` | Document saved |
| `workspace/didChangeConfiguration` | Settings changed |
| `initialized` | Client finished initializing |
| `exit` | Exit the process |
| `prover/interpretToPoint` | Execute up to cursor position |
| `prover/interpretToEnd` | Execute to end of document |
| `prover/stepForward` | Execute one sentence forward |
| `prover/stepBackward` | Step back one sentence |

## Server → Client

### Notifications (server pushes, unsolicited)

| Method | Caused by |
|---|---|
| `textDocument/publishDiagnostics` | Any change to document or execution state |
| `prover/updateHighlights` | Any change to document or execution state (always sent with publishDiagnostics) |
| `prover/proofView` | Navigation commands; background execution reaching the observed sentence; blocking errors |
| `prover/moveCursor` | `stepForward`, `stepBackward`; blocking errors in Manual mode |
| `prover/blockOnError` | Any navigation in Manual mode when `block_on_first_error=true` and execution fails |
| `prover/searchResult` | `prover/search` — fires repeatedly until the search is exhausted |
| `prover/debugMessage` | Any Rocq execution activity |
