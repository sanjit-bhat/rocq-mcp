package main

// tools.go — MCP tool registration wiring each tool name to its handler.

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sanjit/rocq-mcp/internal/rocq"
)

// Tool argument types.

type fileArg struct {
	File string `json:"file" jsonschema:"path to the .v file"`
}

type checkArg struct {
	File string `json:"file" jsonschema:"path to the .v file"`
	Line int    `json:"line" jsonschema:"0-indexed line number"`
	Col  int    `json:"col" jsonschema:"0-indexed column number"`
}

type queryArg struct {
	File    string `json:"file" jsonschema:"path to the .v file"`
	Pattern string `json:"pattern" jsonschema:"the identifier or expression to query"`
}

type searchArg struct {
	File    string `json:"file" jsonschema:"path to the .v file"`
	Pattern string `json:"pattern" jsonschema:"search pattern (e.g. 'nat -> nat', '_ + _ = _ + _')"`
}

// registerTools registers all MCP tools on the server.
func registerTools(server *mcp.Server, sm *rocq.StateManager) {
	// Tier 1: Core proof interaction.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_open",
		Description: "Open a .v file in the Rocq proof checker. Must be called before any other operations on the file.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		if err := sm.OpenDoc(args.File); err != nil {
			return rocq.ErrResult(err), nil, nil
		}
		return rocq.TextResult("Opened " + args.File), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_close",
		Description: "Close a .v file and release its resources.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		if err := sm.CloseDoc(args.File); err != nil {
			return rocq.ErrResult(err), nil, nil
		}
		return rocq.TextResult("Closed " + args.File), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_sync",
		Description: "Re-read a .v file from disk after editing it. Required after using Edit/Write tools.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		if err := sm.SyncDoc(args.File); err != nil {
			return rocq.ErrResult(err), nil, nil
		}
		return rocq.TextResult("Synced " + args.File), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_check",
		Description: "Check the file up to a given position. Returns proof goals and diagnostics (errors/warnings).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args checkArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoCheck(sm, args.File, args.Line, args.Col)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_check_all",
		Description: "Check the entire file. Returns proof goals (if any remain) and all diagnostics.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoCheckAll(sm, args.File)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_step_forward",
		Description: "Step forward one sentence in the proof. Returns updated proof goals.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoStep(sm, args.File, "prover/stepForward")
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_step_backward",
		Description: "Step backward one sentence in the proof. Returns updated proof goals.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoStep(sm, args.File, "prover/stepBackward")
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_get_proof_state",
		Description: "Get the full current proof state with all goals and hypotheses. Use this when you need the complete context rather than the delta returned by step/check.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		sm.Mu.Lock()
		doc, err := sm.GetDoc(args.File)
		sm.Mu.Unlock()
		if err != nil {
			return rocq.ErrResult(err), nil, nil
		}
		if doc.ProofView == nil {
			return rocq.TextResult("No proof state available. Run rocq_check or rocq_step_forward first."), nil, nil
		}
		return rocq.FormatFullResults(doc.ProofView, doc.Diagnostics), nil, nil
	})

	// Tier 2: Query tools.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_about",
		Description: "Show information about an identifier (type, module, etc). Like Rocq's 'About' command.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoQuery(sm, args.File, "prover/about", args.Pattern)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_check_type",
		Description: "Check the type of an expression. Like Rocq's 'Check' command.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoQuery(sm, args.File, "prover/check", args.Pattern)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_locate",
		Description: "Locate the defining module of an identifier. Like Rocq's 'Locate' command.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoQuery(sm, args.File, "prover/locate", args.Pattern)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_print",
		Description: "Print the full definition of an identifier. Like Rocq's 'Print' command.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoQuery(sm, args.File, "prover/print", args.Pattern)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_search",
		Description: "Search for lemmas matching a pattern. Like Rocq's 'Search' command. Results may be large; use specific patterns.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoSearch(sm, args.File, args.Pattern)
	})

	// Tier 3: Diagnostics & state.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_reset",
		Description: "Reset the Rocq prover state for a file. Use when the prover is in a bad state.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoReset(sm, args.File)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_document_proofs",
		Description: "List all proof blocks in a file with their statements, tactics, and line ranges. Useful for navigating and understanding proof structure.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		return rocq.DoDocumentProofs(sm, args.File)
	})
}
