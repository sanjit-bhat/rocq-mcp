package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
}

const notifyTimeout = 10 * time.Second

// doCheck sends interpretToPoint and waits for proofView + diagnostics.
func doCheck(sm *stateManager, file string, line, col int) (*mcp.CallToolResult, any, error) {
	sm.mu.Lock()
	doc, err := sm.getDoc(file)
	if err != nil {
		sm.mu.Unlock()
		return errResult(err), nil, nil
	}
	// Drain channels before sending.
	drainChannels(doc)
	sm.mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
		"position":     map[string]any{"line": line, "character": col},
	}
	if err := sm.client.notify("prover/interpretToPoint", params); err != nil {
		return errResult(err), nil, nil
	}

	return collectResults(doc)
}

// doCheckAll sends interpretToEnd and waits for results.
func doCheckAll(sm *stateManager, file string) (*mcp.CallToolResult, any, error) {
	sm.mu.Lock()
	doc, err := sm.getDoc(file)
	if err != nil {
		sm.mu.Unlock()
		return errResult(err), nil, nil
	}
	drainChannels(doc)
	sm.mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.client.notify("prover/interpretToEnd", params); err != nil {
		return errResult(err), nil, nil
	}

	return collectResults(doc)
}

// doStep sends stepForward or stepBackward and waits for results.
func doStep(sm *stateManager, file string, method string) (*mcp.CallToolResult, any, error) {
	sm.mu.Lock()
	doc, err := sm.getDoc(file)
	if err != nil {
		sm.mu.Unlock()
		return errResult(err), nil, nil
	}
	drainChannels(doc)
	sm.mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.client.notify(method, params); err != nil {
		return errResult(err), nil, nil
	}

	return collectResults(doc)
}

// collectResults waits for proofView and diagnostics notifications.
func collectResults(doc *docState) (*mcp.CallToolResult, any, error) {
	var pv *ProofView
	var diags []Diagnostic

	// Wait for at least one notification, then collect any others that arrive quickly.
	timer := time.NewTimer(notifyTimeout)
	defer timer.Stop()

	gotProofView := false
	gotDiags := false

loop:
	for !gotProofView || !gotDiags {
		select {
		case pv = <-doc.proofViewCh:
			gotProofView = true
		case diags = <-doc.diagnosticCh:
			gotDiags = true
		case <-timer.C:
			// Use whatever we have so far.
			break loop
		}
		// After getting the first notification, give a short window for the second.
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(500 * time.Millisecond)
	}

	result := formatDeltaResults(doc.PrevProofView, pv, diags)
	doc.PrevProofView = pv
	if pv != nil {
		doc.ProofView = pv
	}
	if diags != nil {
		doc.Diagnostics = diags
	}
	return result, nil, nil
}

// formatDeltaResults formats proof state as a delta against the previous proof view.
// Focused goal (goal 1) is shown in full; non-focused goals are summarized.
// Hypotheses are diffed against the previous focused goal.
func formatDeltaResults(prev *ProofView, pv *ProofView, diags []Diagnostic) *mcp.CallToolResult {
	var sb strings.Builder

	if pv != nil && len(pv.Goals) > 0 {
		// Header with goal count and change.
		prevCount := 0
		if prev != nil {
			prevCount = len(prev.Goals)
		}
		if prevCount == 0 || prev == nil {
			fmt.Fprintf(&sb, "=== Proof Goals: %d ===\n", len(pv.Goals))
		} else {
			delta := len(pv.Goals) - prevCount
			if delta > 0 {
				fmt.Fprintf(&sb, "=== Proof Goals: %d (+%d) ===\n", len(pv.Goals), delta)
			} else if delta < 0 {
				fmt.Fprintf(&sb, "=== Proof Goals: %d (%d) ===\n", len(pv.Goals), delta)
			} else {
				fmt.Fprintf(&sb, "=== Proof Goals: %d ===\n", len(pv.Goals))
			}
		}

		// Focused goal (goal 1): full detail with hypothesis diff.
		g := pv.Goals[0]
		sb.WriteString("\nFocused Goal")
		if g.ID != "" {
			fmt.Fprintf(&sb, " (%s)", g.ID)
		}
		sb.WriteString(":\n")

		var prevGoal *ProofGoal
		if prev != nil && len(prev.Goals) > 0 {
			prevGoal = &prev.Goals[0]
		}
		writeHypothesesDiff(&sb, prevGoal, &g)
		sb.WriteString("  ────────────────────\n")
		fmt.Fprintf(&sb, "  %s\n", g.Goal)

		// Non-focused goals: just conclusion.
		for i := 1; i < len(pv.Goals); i++ {
			ng := pv.Goals[i]
			fmt.Fprintf(&sb, "\nGoal %d", i+1)
			if ng.ID != "" {
				fmt.Fprintf(&sb, " (%s)", ng.ID)
			}
			fmt.Fprintf(&sb, ": %s\n", ng.Goal)
		}
	}

	if pv != nil && len(pv.Messages) > 0 {
		sb.WriteString("\n=== Messages ===\n")
		for _, m := range pv.Messages {
			fmt.Fprintf(&sb, "%s\n", m)
		}
	}

	formatDiagnostics(&sb, diags)

	if sb.Len() == 0 {
		sb.WriteString("No goals or diagnostics.")
	}

	return textResult(sb.String())
}

// formatFullResults formats the complete proof state without deltas.
func formatFullResults(pv *ProofView, diags []Diagnostic) *mcp.CallToolResult {
	var sb strings.Builder

	if pv != nil && len(pv.Goals) > 0 {
		fmt.Fprintf(&sb, "=== Proof Goals: %d ===\n", len(pv.Goals))
		for i, g := range pv.Goals {
			if i > 0 {
				sb.WriteString("\n")
			}
			fmt.Fprintf(&sb, "Goal %d", i+1)
			if g.ID != "" {
				fmt.Fprintf(&sb, " (%s)", g.ID)
			}
			sb.WriteString(":\n")
			for _, h := range g.Hypotheses {
				fmt.Fprintf(&sb, "  %s\n", h)
			}
			sb.WriteString("  ────────────────────\n")
			fmt.Fprintf(&sb, "  %s\n", g.Goal)
		}
	}

	if pv != nil && len(pv.Messages) > 0 {
		sb.WriteString("\n=== Messages ===\n")
		for _, m := range pv.Messages {
			fmt.Fprintf(&sb, "%s\n", m)
		}
	}

	formatDiagnostics(&sb, diags)

	if sb.Len() == 0 {
		sb.WriteString("No goals or diagnostics.")
	}

	return textResult(sb.String())
}

// writeHypothesesDiff writes hypotheses for the focused goal, annotating additions/removals
// relative to the previous focused goal.
func writeHypothesesDiff(sb *strings.Builder, prev *ProofGoal, cur *ProofGoal) {
	if prev == nil {
		// No previous state — show all hypotheses as-is.
		for _, h := range cur.Hypotheses {
			fmt.Fprintf(sb, "  %s\n", h)
		}
		return
	}

	prevSet := make(map[string]bool, len(prev.Hypotheses))
	for _, h := range prev.Hypotheses {
		prevSet[h] = true
	}
	curSet := make(map[string]bool, len(cur.Hypotheses))
	for _, h := range cur.Hypotheses {
		curSet[h] = true
	}

	// Show removed hypotheses first.
	for _, h := range prev.Hypotheses {
		if !curSet[h] {
			fmt.Fprintf(sb, "  - %s\n", h)
		}
	}
	// Show current hypotheses, marking new ones.
	for _, h := range cur.Hypotheses {
		if !prevSet[h] {
			fmt.Fprintf(sb, "  + %s\n", h)
		} else {
			fmt.Fprintf(sb, "  %s\n", h)
		}
	}
}

// formatDiagnostics appends diagnostic output to a string builder.
func formatDiagnostics(sb *strings.Builder, diags []Diagnostic) {
	if len(diags) > 0 {
		sb.WriteString("\n=== Diagnostics ===\n")
		for _, d := range diags {
			severity := "info"
			switch d.Severity {
			case 1:
				severity = "error"
			case 2:
				severity = "warning"
			case 3:
				severity = "info"
			case 4:
				severity = "hint"
			}
			fmt.Fprintf(sb, "[%s] line %d:%d–%d:%d: %s\n",
				severity,
				d.Range.Start.Line+1, d.Range.Start.Character,
				d.Range.End.Line+1, d.Range.End.Character,
				d.Message)
		}
	}
}

func drainChannels(doc *docState) {
	for {
		select {
		case <-doc.proofViewCh:
		case <-doc.diagnosticCh:
		default:
			return
		}
	}
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

// parseProofView parses the vsrocq proofView notification params.
// vsrocq uses Ppcmd (pretty-printer command) trees for goals and hypotheses.
func parseProofView(params json.RawMessage) *ProofView {
	var raw struct {
		Proof struct {
			Goals          []rawGoal `json:"goals"`
			ShelvedGoals   []rawGoal `json:"shelvedGoals"`
			GivenUpGoals   []rawGoal `json:"givenUpGoals"`
			UnfocusedGoals []rawGoal `json:"unfocusedGoals"`
		} `json:"proof"`
		Messages   []json.RawMessage `json:"messages"`
		PPMessages []json.RawMessage `json:"pp_messages"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		return nil
	}

	pv := &ProofView{}
	for _, g := range raw.Proof.Goals {
		goal := ProofGoal{
			ID:   strings.TrimSpace(string(g.ID)),
			Goal: renderPpcmd(g.Goal),
		}
		for _, h := range g.Hypotheses {
			goal.Hypotheses = append(goal.Hypotheses, renderPpcmd(h))
		}
		pv.Goals = append(pv.Goals, goal)
	}
	for _, m := range raw.Messages {
		// messages items can be [severity, ppcmd_tree] or plain ppcmd
		var pair []json.RawMessage
		if json.Unmarshal(m, &pair) == nil && len(pair) >= 2 {
			// Check if first element is a number (severity).
			var severity int
			if json.Unmarshal(pair[0], &severity) == nil {
				text := renderPpcmd(pair[1])
				if text != "" {
					pv.Messages = append(pv.Messages, text)
				}
				continue
			}
		}
		text := renderPpcmd(m)
		if text != "" {
			pv.Messages = append(pv.Messages, text)
		}
	}
	for _, m := range raw.PPMessages {
		// pp_messages items are [severity, ppcmd_tree]
		var pair []json.RawMessage
		if json.Unmarshal(m, &pair) == nil && len(pair) >= 2 {
			text := renderPpcmd(pair[1])
			if text != "" {
				pv.Messages = append(pv.Messages, text)
			}
		}
	}
	return pv
}

type rawGoal struct {
	ID         json.RawMessage   `json:"id"`
	Goal       json.RawMessage   `json:"goal"`
	Hypotheses []json.RawMessage `json:"hypotheses"`
}

// renderPpcmd renders a vsrocq Ppcmd tree to plain text.
// Ppcmd format: ["Ppcmd_string", "text"], ["Ppcmd_glue", [...]], etc.
func renderPpcmd(raw json.RawMessage) string {
	// Try as plain string first.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}

	// Parse as array.
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) == 0 {
		return string(raw)
	}

	var tag string
	if json.Unmarshal(arr[0], &tag) != nil {
		return string(raw)
	}

	switch tag {
	case "Ppcmd_string":
		if len(arr) > 1 {
			var text string
			json.Unmarshal(arr[1], &text)
			return text
		}
	case "Ppcmd_glue":
		if len(arr) > 1 {
			var children []json.RawMessage
			if json.Unmarshal(arr[1], &children) == nil {
				var sb strings.Builder
				for _, child := range children {
					sb.WriteString(renderPpcmd(child))
				}
				return sb.String()
			}
		}
	case "Ppcmd_box":
		// ["Ppcmd_box", boxtype, content]
		if len(arr) > 2 {
			return renderPpcmd(arr[2])
		}
	case "Ppcmd_tag":
		// ["Ppcmd_tag", tagname, content]
		if len(arr) > 2 {
			return renderPpcmd(arr[2])
		}
	case "Ppcmd_print_break":
		// ["Ppcmd_print_break", nspaces, offset]
		if len(arr) > 1 {
			var n int
			json.Unmarshal(arr[1], &n)
			return strings.Repeat(" ", n)
		}
		return " "
	case "Ppcmd_force_newline":
		return "\n"
	case "Ppcmd_comment":
		if len(arr) > 1 {
			var parts []string
			json.Unmarshal(arr[1], &parts)
			return strings.Join(parts, " ")
		}
	}
	return ""
}
