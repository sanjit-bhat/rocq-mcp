package main

// tools.go — MCP tool registration wiring each tool name to its handler.

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
func registerTools(server *mcp.Server, sm *stateManager) {
	// Tier 1: Core proof interaction.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_open",
		Description: "Open a .v file in the Rocq proof checker. Must be called before any other operations on the file.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		if err := sm.openDoc(args.File); err != nil {
			return errResult(err), nil, nil
		}
		return textResult("Opened " + args.File), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_close",
		Description: "Close a .v file and release its resources.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		if err := sm.closeDoc(args.File); err != nil {
			return errResult(err), nil, nil
		}
		return textResult("Closed " + args.File), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_sync",
		Description: "Re-read a .v file from disk after editing it. Required after using Edit/Write tools.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		if err := sm.syncDoc(args.File); err != nil {
			return errResult(err), nil, nil
		}
		return textResult("Synced " + args.File), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_check",
		Description: "Check the file up to a given position. Returns proof goals and diagnostics (errors/warnings).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args checkArg) (*mcp.CallToolResult, any, error) {
		return doCheck(sm, args.File, args.Line, args.Col)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_check_all",
		Description: "Check the entire file. Returns proof goals (if any remain) and all diagnostics.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		return doCheckAll(sm, args.File)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_step_forward",
		Description: "Step forward one sentence in the proof. Returns updated proof goals.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		return doStep(sm, args.File, "prover/stepForward")
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_step_backward",
		Description: "Step backward one sentence in the proof. Returns updated proof goals.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		return doStep(sm, args.File, "prover/stepBackward")
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_get_proof_state",
		Description: "Get the full current proof state with all goals and hypotheses. Use this when you need the complete context rather than the delta returned by step/check.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args fileArg) (*mcp.CallToolResult, any, error) {
		sm.mu.Lock()
		doc, err := sm.getDoc(args.File)
		sm.mu.Unlock()
		if err != nil {
			return errResult(err), nil, nil
		}
		if doc.ProofView == nil {
			return textResult("No proof state available. Run rocq_check or rocq_step_forward first."), nil, nil
		}
		return formatFullResults(doc.ProofView, doc.Diagnostics), nil, nil
	})

	// Tier 2: Query tools.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_about",
		Description: "Show information about an identifier (type, module, etc). Like Rocq's 'About' command.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArg) (*mcp.CallToolResult, any, error) {
		return doQuery(sm, args.File, "prover/about", args.Pattern)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_check_type",
		Description: "Check the type of an expression. Like Rocq's 'Check' command.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArg) (*mcp.CallToolResult, any, error) {
		return doQuery(sm, args.File, "prover/check", args.Pattern)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_locate",
		Description: "Locate the defining module of an identifier. Like Rocq's 'Locate' command.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArg) (*mcp.CallToolResult, any, error) {
		return doQuery(sm, args.File, "prover/locate", args.Pattern)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_print",
		Description: "Print the full definition of an identifier. Like Rocq's 'Print' command.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryArg) (*mcp.CallToolResult, any, error) {
		return doQuery(sm, args.File, "prover/print", args.Pattern)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rocq_search",
		Description: "Search for lemmas matching a pattern. Like Rocq's 'Search' command. Results may be large; use specific patterns.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArg) (*mcp.CallToolResult, any, error) {
		return doSearch(sm, args.File, args.Pattern)
	})
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: err.Error()},
		},
	}
}
