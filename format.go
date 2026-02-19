package main

// format.go — rendering proof views, diagnostics, and Ppcmd trees to human-readable text.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// formatDeltaResults formats proof state as a delta against the previous proof view.
// When prev is non-nil, renders both as plain text and uses git diff for a line-level diff.
func formatDeltaResults(prev *ProofView, pv *ProofView, diags []Diagnostic) *mcp.CallToolResult {
	var sb strings.Builder

	if pv != nil && len(pv.Goals) > 0 {
		// Header with goal count and change.
		prevCount := 0
		if prev != nil {
			prevCount = len(prev.Goals)
		}
		if prev == nil || prevCount == 0 {
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

		if prev == nil || prevCount == 0 {
			// No previous state — show full proof text.
			sb.WriteString(renderProofText(pv))
		} else {
			// Diff previous vs current proof text.
			d := diffText(renderProofText(prev), renderProofText(pv))
			if d == "" {
				sb.WriteString("\nNo changes to proof state.\n")
			} else {
				sb.WriteString("\n")
				sb.WriteString(d)
			}
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

// renderProofText renders a proof view as plain text for diffing.
func renderProofText(pv *ProofView) string {
	var sb strings.Builder
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
	return sb.String()
}

// diffText computes a line-level diff between old and new text using git diff.
// Returns just the hunk lines (@@, +, -) with file headers stripped.
// Returns empty string if texts are identical.
func diffText(old, new string) string {
	if old == new {
		return ""
	}

	oldFile, err := os.CreateTemp("", "rocq-diff-old-*")
	if err != nil {
		log.Fatalf("diffText: create temp file: %v", err)
	}
	defer os.Remove(oldFile.Name())

	newFile, err := os.CreateTemp("", "rocq-diff-new-*")
	if err != nil {
		oldFile.Close()
		log.Fatalf("diffText: create temp file: %v", err)
	}
	defer os.Remove(newFile.Name())

	oldFile.WriteString(old)
	oldFile.Close()
	newFile.WriteString(new)
	newFile.Close()

	cmd := exec.Command("git", "diff", "--no-index", "--histogram", "--unified=0", oldFile.Name(), newFile.Name())
	out, _ := cmd.Output()
	// git diff exits 1 when files differ, so ignore exit error.
	if len(out) == 0 {
		log.Fatal("diffText: git diff produced no output for differing inputs")
	}

	return parseDiffHunks(string(out))
}

// parseDiffHunks extracts just the @@ hunk headers and +/- lines from git diff output.
func parseDiffHunks(raw string) string {
	var sb strings.Builder
	for line := range strings.SplitSeq(raw, "\n") {
		if strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			// Skip file headers (--- a/..., +++ b/...).
			if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
				continue
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return sb.String()
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
