package main

import (
	"testing"
)

func TestFormatDeltaResults_NoPrevious(t *testing.T) {
	prev := &ProofView{} // zero-value, as initialized in openDoc
	pv := &ProofView{
		GoalCount: 1,
		GoalID:    "1",
		GoalText:  "  n : nat\n  ────────────────────\n  0 + n = n\n",
	}
	got := resultText(formatDeltaResults(prev, pv, nil))
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
		GoalCount: 1,
		GoalID:    "1",
		GoalText:  "  ────────────────────\n  A /\\ B\n",
	}
	cur := &ProofView{
		GoalCount: 2,
		GoalID:    "2",
		GoalText:  "  ────────────────────\n  A\n",
	}
	got := resultText(formatDeltaResults(prev, cur, nil))
	want := `Goal 1 of 2:
  ────────────────────
  A

2 goals remaining
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_ProofComplete(t *testing.T) {
	prev := &ProofView{
		GoalCount: 1,
		GoalID:    "1",
		GoalText:  "  ────────────────────\n  True\n",
	}
	cur := &ProofView{} // GoalCount=0, UnfocusedCount=0
	got := resultText(formatDeltaResults(prev, cur, nil))
	want := "Proof complete!\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_SubGoalComplete(t *testing.T) {
	prev := &ProofView{
		GoalCount: 1,
		GoalID:    "1",
		GoalText:  "  ────────────────────\n  A\n",
	}
	cur := &ProofView{
		GoalCount:      0,
		UnfocusedCount: 3,
	}
	got := resultText(formatDeltaResults(prev, cur, nil))
	want := "Sub-goal complete! 3 unfocused remaining.\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_WithDiagnostics(t *testing.T) {
	got := resultText(formatDeltaResults(&ProofView{}, nil, []Diagnostic{
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
		GoalCount: 1,
		GoalID:    "1",
		GoalText:  "  n : nat\n  ────────────────────\n  P\n",
	}
	got := resultText(formatDeltaResults(pv, pv, nil))
	want := `Goal:

No changes to proof state.
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults(t *testing.T) {
	pv := &ProofView{
		GoalCount: 2,
		GoalID:    "1",
		GoalText:  "  H : True\n  ────────────────────\n  A\n",
	}
	got := resultText(formatFullResults(pv, nil))
	want := `Goal 1 of 2:
  H : True
  ────────────────────
  A

2 goals remaining
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatFullResults_ProofComplete(t *testing.T) {
	pv := &ProofView{} // GoalCount=0, UnfocusedCount=0
	got := resultText(formatFullResults(pv, nil))
	want := "Proof complete!\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRenderGoalText(t *testing.T) {
	got := renderGoalText([]string{"H : True", "n : nat"}, "A")
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
	got := renderGoalText(nil, "True")
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
	got := parseDiffHunks(raw)
	want := `@@ -1,2 +1,2 @@
-old line
+new line
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_NewFocusedGoal(t *testing.T) {
	prev := &ProofView{
		GoalCount: 2,
		GoalID:    "1",
		GoalText:  "  H : True\n  ────────────────────\n  A\n",
	}
	cur := &ProofView{
		GoalCount: 1,
		GoalID:    "2",
		GoalText:  "  H : True\n  ────────────────────\n  B\n",
	}
	got := resultText(formatDeltaResults(prev, cur, nil))
	want := `Goal:
  H : True
  ────────────────────
  B
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}
