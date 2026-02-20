package main

import (
	"strings"
	"testing"
)

func TestFormatDeltaResults_NoPrevious(t *testing.T) {
	prev := &ProofView{} // zero-value, as initialized in openDoc
	pv := &ProofView{
		GoalCount: 1,
		GoalID:    "1",
		GoalText:  "  n : nat\n  ────────────────────\n  0 + n = n\n",
	}
	result := formatDeltaResults(prev, pv, nil)
	got := resultText(result)
	want := "Goal 1 (1):\n  n : nat\n  ────────────────────\n  0 + n = n\n"
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
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	// Different GoalID → full context (no diff markers).
	if !strings.Contains(got, "  A\n") {
		t.Errorf("expected full goal text.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "2 goals remaining") {
		t.Errorf("expected 2 goals remaining.\ngot:\n%s", got)
	}
}

func TestFormatDeltaResults_NoGoals(t *testing.T) {
	prev := &ProofView{
		GoalCount: 1,
		GoalID:    "1",
		GoalText:  "  ────────────────────\n  True\n",
	}
	cur := &ProofView{}
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	want := "Proof complete!\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_WithDiagnostics(t *testing.T) {
	result := formatDeltaResults(&ProofView{}, nil, []Diagnostic{
		{
			Severity: 1,
			Message:  "type error",
			Range: Range{
				Start: Position{Line: 5, Character: 0},
				End:   Position{Line: 5, Character: 10},
			},
		},
	})
	got := resultText(result)
	want := "\n=== Diagnostics ===\n[error] line 6:0–6:10: type error\n"
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
	result := formatDeltaResults(pv, pv, nil)
	got := resultText(result)
	if !strings.Contains(got, "No changes to proof state.") {
		t.Errorf("expected no-changes message.\ngot:\n%s", got)
	}
}

func TestFormatFullResults(t *testing.T) {
	pv := &ProofView{
		GoalCount: 2,
		GoalID:    "1",
		GoalText:  "  H : True\n  ────────────────────\n  A\n",
	}
	result := formatFullResults(pv, nil)
	got := resultText(result)
	want := "Goal 1 (1):\n  H : True\n  ────────────────────\n  A\n\n2 goals remaining\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRenderGoalText(t *testing.T) {
	got := renderGoalText([]string{"H : True", "n : nat"}, "A")
	want := "  H : True\n  n : nat\n  ────────────────────\n  A\n"
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRenderGoalText_NoHypotheses(t *testing.T) {
	got := renderGoalText(nil, "True")
	want := "  ────────────────────\n  True\n"
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
	if strings.Contains(got, "---") || strings.Contains(got, "+++") {
		t.Errorf("file headers should be stripped.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "@@ -1,2 +1,2 @@") {
		t.Errorf("hunk header should be present.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "-old line") {
		t.Errorf("removed line should be present.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "+new line") {
		t.Errorf("added line should be present.\ngot:\n%s", got)
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
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	// Different GoalID → full context shown (no diff markers).
	if !strings.Contains(got, "Goal 1 (2):") {
		t.Errorf("expected goal header with new ID.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "  B\n") {
		t.Errorf("expected full goal text for new goal.\ngot:\n%s", got)
	}
}
