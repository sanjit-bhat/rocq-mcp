package rocq

// format.go — rendering proof views, diagnostics, and Ppcmd trees to human-readable text.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RenderGoalText renders a single goal body: hypotheses + separator + conclusion.
func RenderGoalText(hyps []string, conclusion string) string {
	var sb strings.Builder
	for _, h := range hyps {
		fmt.Fprintf(&sb, "  %s\n", h)
	}
	sb.WriteString("  ────────────────────\n")
	fmt.Fprintf(&sb, "  %s\n", conclusion)
	return sb.String()
}

// WriteGoals writes all focused goals to the string builder.
func WriteGoals(sb *strings.Builder, goals []Goal) {
	if len(goals) == 1 {
		sb.WriteString("Goal:\n")
		sb.WriteString(goals[0].Text)
		return
	}
	for i, g := range goals {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(sb, "Goal %d of %d:\n", i+1, len(goals))
		sb.WriteString(g.Text)
	}
}

// FormatBackgroundCounts returns a summary of non-zero background goal counts.
// Returns empty string if all counts are zero.
func FormatBackgroundCounts(pv *ProofView) string {
	var parts []string
	if pv.UnfocusedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d unfocused", pv.UnfocusedCount))
	}
	if pv.ShelvedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d shelved", pv.ShelvedCount))
	}
	if pv.GivenUpCount > 0 {
		parts = append(parts, fmt.Sprintf("%d given up", pv.GivenUpCount))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// FormatFullResults formats the complete proof state.
func FormatFullResults(pv *ProofView, diags []Diagnostic) *mcp.CallToolResult {
	var sb strings.Builder

	if pv != nil {
		bg := FormatBackgroundCounts(pv)

		if len(pv.Goals) == 0 {
			if bg == "" {
				sb.WriteString("Proof complete!\n")
			} else {
				fmt.Fprintf(&sb, "No focused goals. %s remaining.\n", bg)
			}
		}

		if len(pv.Goals) > 0 {
			WriteGoals(&sb, pv.Goals)
			if bg != "" {
				fmt.Fprintf(&sb, "\n(+ %s)\n", bg)
			}
		}
	}

	if pv != nil && len(pv.Messages) > 0 {
		sb.WriteString("\n=== Messages ===\n")
		for _, m := range pv.Messages {
			fmt.Fprintf(&sb, "%s\n", m)
		}
	}

	FormatDiagnostics(&sb, diags)

	if sb.Len() == 0 {
		sb.WriteString("No goals or diagnostics.")
	}

	return TextResult(sb.String())
}

// FormatDiagnostics appends diagnostic output to a string builder.
func FormatDiagnostics(sb *strings.Builder, diags []Diagnostic) {
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

// ParseProofView parses the vsrocq proofView notification params.
func ParseProofView(params json.RawMessage) *ProofView {
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

	// background_subgoals includes focused goals, so subtract them.
	unfocused := len(raw.Proof.UnfocusedGoals) - len(raw.Proof.Goals)
	if unfocused < 0 {
		log.Printf("warning: fewer unfocused goals (%d) than focused (%d)", len(raw.Proof.UnfocusedGoals), len(raw.Proof.Goals))
		unfocused = 0
	}
	pv := &ProofView{
		UnfocusedCount: unfocused,
		ShelvedCount:   len(raw.Proof.ShelvedGoals),
		GivenUpCount:   len(raw.Proof.GivenUpGoals),
	}

	// Pre-render all focused goals.
	for _, g := range raw.Proof.Goals {
		id := strings.TrimSpace(string(g.ID))
		conclusion := RenderPpcmd(g.Goal)
		var hyps []string
		for _, h := range g.Hypotheses {
			hyps = append(hyps, RenderPpcmd(h))
		}
		pv.Goals = append(pv.Goals, Goal{ID: id, Text: RenderGoalText(hyps, conclusion)})
	}

	for _, m := range raw.Messages {
		// messages items can be [severity, ppcmd_tree] or plain ppcmd
		var pair []json.RawMessage
		if json.Unmarshal(m, &pair) == nil && len(pair) >= 2 {
			// Check if first element is a number (severity).
			var severity int
			if json.Unmarshal(pair[0], &severity) == nil {
				text := RenderPpcmd(pair[1])
				if text != "" {
					pv.Messages = append(pv.Messages, text)
				}
				continue
			}
		}
		text := RenderPpcmd(m)
		if text != "" {
			pv.Messages = append(pv.Messages, text)
		}
	}
	for _, m := range raw.PPMessages {
		// pp_messages items are [severity, ppcmd_tree]
		var pair []json.RawMessage
		if json.Unmarshal(m, &pair) == nil && len(pair) >= 2 {
			text := RenderPpcmd(pair[1])
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

// RenderPpcmd renders a vsrocq Ppcmd tree to plain text.
func RenderPpcmd(raw json.RawMessage) string {
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
					sb.WriteString(RenderPpcmd(child))
				}
				return sb.String()
			}
		}
	case "Ppcmd_box":
		if len(arr) > 2 {
			return RenderPpcmd(arr[2])
		}
	case "Ppcmd_tag":
		if len(arr) > 2 {
			return RenderPpcmd(arr[2])
		}
	case "Ppcmd_print_break":
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

// TextResult wraps a string in an MCP CallToolResult.
func TextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

// ErrResult wraps an error in an MCP CallToolResult.
func ErrResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: err.Error()},
		},
	}
}
