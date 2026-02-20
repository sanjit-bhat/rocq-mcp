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

## Output format

Step operations (`rocq_step_forward`, `rocq_step_backward`) show **diffs** when
the focused goal hasn't changed (e.g., after `rewrite`, `simpl`, `intro`), and
**full context** when the goal changes (e.g., after `split`, `assert`, solving a
sub-goal). Check operations (`rocq_check`, `rocq_check_all`) always show full
context. Use `rocq_get_proof_state` to get the full state at any time.

## Installation

### Prerequisites

Install `vsrocqtop`:
```
opam install vsrocq-language-server
```

### Install

```
go install github.com/sanjit/rocq-mcp@latest
```

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

### Add the workflow skill

Copy `.claude/skills/rocq-build/` into your project. This teaches the agent the open/check/edit/sync workflow.

### Example project

See [pav-proof](https://github.com/sanjit-bhat/pav-proof/tree/qed-serv) for a working setup.
