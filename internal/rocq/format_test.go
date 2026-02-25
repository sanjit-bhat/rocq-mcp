package rocq

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func resultText(r *mcp.CallToolResult) string {
	if r == nil {
		return "<nil>"
	}
	var parts []string
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestFormatFullResults_SingleGoal(t *testing.T) {
	pv := &ProofView{
		Goals: []Goal{{ID: "1", Text: "  n : nat\n  ────────────────────\n  0 + n = n\n"}},
	}
	got := resultText(FormatFullResults(pv, nil))
	want := `Goal:
  n : nat
  ────────────────────
  0 + n = n
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults_MultipleGoals(t *testing.T) {
	pv := &ProofView{
		Goals: []Goal{
			{ID: "1", Text: "  H : True\n  ────────────────────\n  A\n"},
			{ID: "2", Text: "  H : True\n  ────────────────────\n  B\n"},
		},
	}
	got := resultText(FormatFullResults(pv, nil))
	want := `Goal 1 of 2:
  H : True
  ────────────────────
  A

Goal 2 of 2:
  H : True
  ────────────────────
  B
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults_ProofComplete(t *testing.T) {
	pv := &ProofView{}
	got := resultText(FormatFullResults(pv, nil))
	want := "Proof complete!\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults_NoFocusedWithBackground(t *testing.T) {
	pv := &ProofView{
		UnfocusedCount: 3,
		ShelvedCount:   1,
	}
	got := resultText(FormatFullResults(pv, nil))
	want := "No focused goals. 3 unfocused, 1 shelved remaining.\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults_GoalsWithBackground(t *testing.T) {
	pv := &ProofView{
		UnfocusedCount: 2,
		GivenUpCount:   1,
		Goals:          []Goal{{ID: "1", Text: "  ────────────────────\n  A\n"}},
	}
	got := resultText(FormatFullResults(pv, nil))
	want := `Goal:
  ────────────────────
  A

(+ 2 unfocused, 1 given up)
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults_WithDiagnostics(t *testing.T) {
	got := resultText(FormatFullResults(nil, []Diagnostic{
		{
			Severity: 1,
			Message:  "type error",
			Range: Range{
				Start: Position{Line: 5, Character: 0},
				End:   Position{Line: 5, Character: 10},
			},
		},
	}))
	want := `
=== Diagnostics ===
[error] line 6:0–6:10: type error
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults_NoGoalsOrDiagnostics(t *testing.T) {
	got := resultText(FormatFullResults(nil, nil))
	want := "No goals or diagnostics."
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatBackgroundCounts(t *testing.T) {
	tests := []struct {
		name string
		pv   *ProofView
		want string
	}{
		{"all zero", &ProofView{}, ""},
		{"unfocused only", &ProofView{UnfocusedCount: 3}, "3 unfocused"},
		{"shelved only", &ProofView{ShelvedCount: 1}, "1 shelved"},
		{"given up only", &ProofView{GivenUpCount: 2}, "2 given up"},
		{"mixed", &ProofView{UnfocusedCount: 3, ShelvedCount: 1, GivenUpCount: 2}, "3 unfocused, 1 shelved, 2 given up"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatBackgroundCounts(tt.pv)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderGoalText(t *testing.T) {
	got := RenderGoalText([]string{"H : True", "n : nat"}, "A")
	want := `  H : True
  n : nat
  ────────────────────
  A
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRenderGoalText_NoHypotheses(t *testing.T) {
	got := RenderGoalText(nil, "True")
	want := `  ────────────────────
  True
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}
