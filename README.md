# rocq-mcp

An MCP server that connects AI agents to the [Rocq](https://rocq-prover.org/) proof assistant.
It wraps `vsrocqtop` (the Rocq LSP server) and exposes proof-checking tools over MCP,
so an agent can open `.v` files, step through proofs, inspect goals, and fix errors interactively.

## Tools

| Tool | Description |
|------|-------------|
| `rocq_open` | Open a `.v` file in the proof checker |
| `rocq_close` | Close a file and release resources |
| `rocq_sync` | Re-read a file from disk after editing |
| `rocq_check` | Check up to a position; returns goals and diagnostics |
| `rocq_check_all` | Check the entire file |
| `rocq_step_forward` | Step forward one sentence |
| `rocq_step_backward` | Step backward one sentence |
| `rocq_get_proof_state` | Get the full proof state (all goals and hypotheses) |

## Installation

### Prerequisites

Install `vsrocqtop`:
```
opam install vsrocq-language-server
```

### Build

```
go build -o rocq-mcp .
```

Put `rocq-mcp` somewhere on your `$PATH` (or reference it by absolute path).

## Usage

### Configure your project

Add a `.mcp.json` to your Rocq project root:

```json
{
  "mcpServers": {
    "rocq": {
      "command": "./etc/run-rocq-mcp.sh"
    }
  }
}
```

Create `etc/run-rocq-mcp.sh`:

```bash
#!/usr/bin/env bash
ARGS=$(sed -E -e '/^#/d' -e "s/'([^']*)'//g" -e 's/-arg //g' _RocqProject)
exec rocq-mcp $ARGS
```

This reads your `_RocqProject` file and passes the flags (load paths, warnings, etc.) through to `vsrocqtop`.

### Allow MCP tools in Claude Code

In `.claude/settings.local.json`:

```json
{
  "permissions": {
    "allow": [
      "mcp__rocq"
    ]
  },
  "enabledMcpjsonServers": [
    "rocq"
  ]
}
```

### Workflow

1. Build dependencies for your `.v` file first (e.g. `make -j10 path/to/file.vos`)
2. `rocq_open` the file
3. `rocq_check_all` to see current errors and proof goals
4. Edit the file to fix errors or fill in proofs
5. `rocq_sync` after every edit (required — the checker reads from disk)
6. `rocq_check_all` again to see if errors are resolved
7. Repeat 4–6 until clean

Tips:
- Use `rocq_check` at a specific line to see proof goals mid-proof
- Use `rocq_step_forward` / `rocq_step_backward` for fine-grained stepping
- Use `rocq_get_proof_state` when you need the full context (all hypotheses and goals)
