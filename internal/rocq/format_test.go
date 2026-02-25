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

func TestFormatDeltaResults_NoPrevious(t *testing.T) {
	prev := &ProofView{} // zero-value, as initialized in OpenDoc
	pv := &ProofView{
		Goals: []Goal{{ID: "1", Text: "  n : nat\n  ────────────────────\n  0 + n = n\n"}},
	}
	got := resultText(FormatDeltaResults(prev, pv, nil))
	want := `Goal:
  n : nat
  ────────────────────
  0 + n = n
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_GoalCountDelta(t *testing.T) {
	prev := &ProofView{
		Goals: []Goal{{ID: "1", Text: "  ────────────────────\n  A /\\ B\n"}},
	}
	cur := &ProofView{
		Goals: []Goal{
			{ID: "2", Text: "  ────────────────────\n  A\n"},
			{ID: "3", Text: "  ────────────────────\n  B\n"},
		},
	}
	got := resultText(FormatDeltaResults(prev, cur, nil))
	want := `Goal 1 of 2:
  ────────────────────
  A

Goal 2 of 2:
  ────────────────────
  B
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_ProofComplete(t *testing.T) {
	prev := &ProofView{
		Goals: []Goal{{ID: "1", Text: "  ────────────────────\n  True\n"}},
	}
	cur := &ProofView{} // Goals empty, UnfocusedCount=0
	got := resultText(FormatDeltaResults(prev, cur, nil))
	want := "Proof complete!\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_SubGoalComplete(t *testing.T) {
	prev := &ProofView{
		Goals: []Goal{{ID: "1", Text: "  ────────────────────\n  A\n"}},
	}
	cur := &ProofView{
		UnfocusedCount: 3,
	}
	got := resultText(FormatDeltaResults(prev, cur, nil))
	want := "Sub-goal complete! 3 unfocused remaining.\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_WithDiagnostics(t *testing.T) {
	got := resultText(FormatDeltaResults(&ProofView{}, nil, []Diagnostic{
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

func TestFormatDeltaResults_NoChanges(t *testing.T) {
	pv := &ProofView{
		Goals: []Goal{{ID: "1", Text: "  n : nat\n  ────────────────────\n  P\n"}},
	}
	got := resultText(FormatDeltaResults(pv, pv, nil))
	want := `Goal:

No changes to proof state.
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults(t *testing.T) {
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
	pv := &ProofView{} // Goals empty, UnfocusedCount=0
	got := resultText(FormatFullResults(pv, nil))
	want := "Proof complete!\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
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

func TestParseDiffHunks(t *testing.T) {
	raw := `diff --git a/old b/new
index 1234..5678 100644
--- a/old
+++ b/new
@@ -1,2 +1,2 @@
-old line
+new line
`
	got := ParseDiffHunks(raw)
	want := `@@ -1,2 +1,2 @@
-old line
+new line
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_SameGoalDiff(t *testing.T) {
	prev := &ProofView{
		Goals: []Goal{{ID: "1", Text: "  n : nat\n  ────────────────────\n  0 + n = n\n"}},
	}
	cur := &ProofView{
		Goals: []Goal{{ID: "1", Text: "  n : nat\n  ────────────────────\n  n = n\n"}},
	}
	got := resultText(FormatDeltaResults(prev, cur, nil))
	want := "Goal:\n\n@@ -3 +3 @@\n-  0 + n = n\n+  n = n\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_NewFocusedGoal(t *testing.T) {
	prev := &ProofView{
		Goals: []Goal{
			{ID: "1", Text: "  H : True\n  ────────────────────\n  A\n"},
			{ID: "2", Text: "  H : True\n  ────────────────────\n  B\n"},
		},
	}
	cur := &ProofView{
		Goals: []Goal{{ID: "2", Text: "  H : True\n  ────────────────────\n  B\n"}},
	}
	got := resultText(FormatDeltaResults(prev, cur, nil))
	want := `Goal:
  H : True
  ────────────────────
  B
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}
