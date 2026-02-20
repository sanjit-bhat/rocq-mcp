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

// renderGoalText renders a single goal body: hypotheses + separator + conclusion.
// This is the pre-rendered text stored in ProofView.GoalText.
func renderGoalText(hyps []string, conclusion string) string {
	var sb strings.Builder
	for _, h := range hyps {
		fmt.Fprintf(&sb, "  %s\n", h)
	}
	sb.WriteString("  ────────────────────\n")
	fmt.Fprintf(&sb, "  %s\n", conclusion)
	return sb.String()
}

// formatDeltaResults formats proof state as a delta against the previous proof view.
// prev is always non-nil (initialized to zero-value in openDoc).
// Same GoalID → show diff. Different GoalID or no previous goals → show full context.
func formatDeltaResults(prev *ProofView, pv *ProofView, diags []Diagnostic) *mcp.CallToolResult {
	var sb strings.Builder

	// Handle proof completion.
	if pv != nil && pv.GoalCount == 0 {
		sb.WriteString("Proof complete!\n")
	}

	// Show goal: full if goal changed, diff if same.
	if pv != nil && pv.GoalCount > 0 {
		fmt.Fprintf(&sb, "Goal 1 (%s):\n", pv.GoalID)
		if prev.GoalCount > 0 && prev.GoalID == pv.GoalID {
			// Same goal — diff.
			d := diffText(prev.GoalText, pv.GoalText)
			if d == "" {
				sb.WriteString("\nNo changes to proof state.\n")
			} else {
				sb.WriteString("\n")
				sb.WriteString(d)
			}
		} else {
			// New/different goal — full context.
			sb.WriteString(pv.GoalText)
		}
		if pv.GoalCount > 1 {
			fmt.Fprintf(&sb, "\n%d goals remaining\n", pv.GoalCount)
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

	if pv != nil && pv.GoalCount > 0 {
		fmt.Fprintf(&sb, "Goal 1 (%s):\n", pv.GoalID)
		sb.WriteString(pv.GoalText)
		if pv.GoalCount > 1 {
			fmt.Fprintf(&sb, "\n%d goals remaining\n", pv.GoalCount)
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
// The focused goal (Goals[0]) is pre-rendered to text; only GoalCount is kept for the rest.
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

	pv := &ProofView{
		GoalCount: len(raw.Proof.Goals),
	}

	// Pre-render only the focused goal (Goals[0]).
	if len(raw.Proof.Goals) > 0 {
		g := raw.Proof.Goals[0]
		pv.GoalID = strings.TrimSpace(string(g.ID))
		conclusion := renderPpcmd(g.Goal)
		var hyps []string
		for _, h := range g.Hypotheses {
			hyps = append(hyps, renderPpcmd(h))
		}
		pv.GoalText = renderGoalText(hyps, conclusion)
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
