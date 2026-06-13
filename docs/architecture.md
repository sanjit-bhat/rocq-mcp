# vsrocq client architecture

## Component overview

```mermaid
graph TD
    subgraph GoProcess["Go process"]
        Caller["caller goroutine\nc.DidOpen / CallContext / …"]
        subgraph Client["vsrocq.Client"]
            RPC["go-ethereum rpc.Client\n(CallContext, Notify, response correlation,\ncontext cancellation)"]
            Writer["lspWriter\n• strips trailing newline\n• unwraps [{}] → {} params\n• prepends Content-Length header"]
            ReadLoop["readLoop goroutine\n(dedicated, single reader)"]
            Handlers["notification channels\nHighlights / Diagnostics\nProofView / … (buffered, closed on exit)"]
        end
    end

    subgraph OCaml["vsrocqtop process  (OCaml)"]
        Sel["Sel event loop\nSel.On.httpcle stdin"]
        Dispatch["handle_lsp_event\n→ dispatch_request\n→ do_initialize / prover/* handlers"]
        DocMgr["DocumentManager"]
    end

    Caller -->|"CallContext(ctx, &result, method, params)"| RPC
    RPC -->|"JSON-RPC request\n{id, method, params:[{…}]}"| Writer
    Writer -->|"Content-Length: N\\r\\n\\r\\n\n{id, method, params:{…}}"| StdinPipe[["stdin pipe"]]
    StdinPipe --> Sel
    Sel --> Dispatch
    Dispatch <--> DocMgr
    Dispatch -->|"Content-Length: N\\r\\n\\r\\n\n{id, result:{…}}"| StdoutPipe[["stdout pipe"]]
    StdoutPipe --> ReadLoop

    ReadLoop -->|"response (has id, no method)\nraw JSON bytes"| Pipe[["io.Pipe\nbridge"]]
    Pipe -->|"json.Decoder.Decode"| RPC
    RPC -->|"unmarshal result, unblock caller"| Caller

    ReadLoop -->|"notification (method, no id)\nnon-blocking send"| Handlers
```

## Request / response flow

```mermaid
sequenceDiagram
    participant Caller
    participant rpc.Client
    participant lspWriter
    participant vsrocqtop
    participant readLoop

    Caller->>rpc.Client: CallContext(ctx, &result, "prover/about", params)
    rpc.Client->>rpc.Client: assign id=N, register pending[N]
    rpc.Client->>lspWriter: Write({"id":N,"method":"prover/about","params":[{…}]}\n)
    lspWriter->>lspWriter: strip \n, unwrap params array → object
    lspWriter->>vsrocqtop: Content-Length: M\r\n\r\n{"id":N,"method":"prover/about","params":{…}}
    vsrocqtop->>vsrocqtop: handle_lsp_event → dispatch_request
    vsrocqtop->>readLoop: Content-Length: K\r\n\r\n{"id":N,"result":{…}}
    readLoop->>readLoop: readLSPFrame → peek: has id, no method → response
    readLoop->>rpc.Client: io.Pipe ← raw JSON bytes
    rpc.Client->>rpc.Client: json.Decoder.Decode → match id=N
    rpc.Client->>Caller: unmarshal result, return nil
```

## Notification flow

```mermaid
sequenceDiagram
    participant vsrocqtop
    participant readLoop
    participant Channels

    vsrocqtop->>readLoop: Content-Length: N\r\n\r\n{"method":"prover/updateHighlights","params":{…}}
    readLoop->>readLoop: readLSPFrame → peek: method set, id nil → notification
    readLoop->>Channels: handleNotification("prover/updateHighlights", body)
    Channels->>Channels: json.Unmarshal params → HighlightsParams
    Channels->>Channels: select { case c.Highlights <- &p: default: }
```

## Key design notes

| Concern | Mechanism |
|---|---|
| Request–response correlation | `go-ethereum/rpc.Client` (replaces hand-rolled pending map + channels) |
| Context cancellation | `rpc.CallContext` honours `ctx.Done()` natively |
| LSP Content-Length framing | `readLSPFrame` (read side) + `lspWriter` (write side) |
| Params format mismatch | `unwrapArrayParams`: go-ethereum serialises args as `[{…}]`; lspWriter rewrites to `{…}` before sending |
| Notification dispatch | `readLoop` classifies by `(method≠"", id==nil)` and calls handlers directly; responses go to the pipe |
| Thread safety | `go-ethereum/rpc.Client` serialises all stdin writes internally; no mutex needed in `lspWriter` |
| Fire-and-forget notifications | `notify()` calls `rpc.Client.Notify()` — goes through the same codec path as requests |
| `workers: int option` invariant | `ProofOptions.Workers *int` has no `omitempty`; Go sends `null` which OCaml decodes as `None` |
