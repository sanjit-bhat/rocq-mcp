package main

import (
	"strings"
	"testing"
)

func TestFormatDeltaResults_NoPrevious(t *testing.T) {
	pv := &ProofView{
		Goals: []ProofGoal{
			{ID: "1", Goal: "0 + n = n", Hypotheses: []string{"n : nat"}},
		},
	}
	result := formatDeltaResults(nil, pv, nil)
	got := resultText(result)
	want := `=== Proof Goals: 1 ===
Goal 1 (1):
  n : nat
  ────────────────────
  0 + n = n
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_AddedHypothesis(t *testing.T) {
	prev := &ProofView{
		Goals: []ProofGoal{
			{Goal: "forall n, 0 + n = n"},
		},
	}
	cur := &ProofView{
		Goals: []ProofGoal{
			{Goal: "0 + n = n", Hypotheses: []string{"n : nat"}},
		},
	}
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	// Should contain diff lines showing added hypothesis and changed goal.
	if !strings.Contains(got, "=== Proof Goals: 1 ===") {
		t.Errorf("missing goal count header.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "+  n : nat") {
		t.Errorf("expected +  n : nat in diff.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "-  forall n, 0 + n = n") {
		t.Errorf("expected removed old goal in diff.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "+  0 + n = n") {
		t.Errorf("expected added new goal in diff.\ngot:\n%s", got)
	}
}

func TestFormatDeltaResults_RemovedHypothesis(t *testing.T) {
	prev := &ProofView{
		Goals: []ProofGoal{
			{Goal: "P", Hypotheses: []string{"H : True", "n : nat"}},
		},
	}
	cur := &ProofView{
		Goals: []ProofGoal{
			{Goal: "P", Hypotheses: []string{"n : nat"}},
		},
	}
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	if !strings.Contains(got, "-  H : True") {
		t.Errorf("expected removed hypothesis in diff.\ngot:\n%s", got)
	}
	// "n : nat" is unchanged so should not appear with + or -
	if strings.Contains(got, "+  n : nat") || strings.Contains(got, "-  n : nat") {
		t.Errorf("unchanged hypothesis should not appear in diff.\ngot:\n%s", got)
	}
}

func TestFormatDeltaResults_GoalCountDelta(t *testing.T) {
	prev := &ProofView{
		Goals: []ProofGoal{
			{Goal: "A /\\ B"},
		},
	}
	cur := &ProofView{
		Goals: []ProofGoal{
			{Goal: "A"},
			{Goal: "B"},
		},
	}
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	if !strings.Contains(got, "=== Proof Goals: 2 (+1) ===") {
		t.Errorf("expected goal count delta.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "+Goal 2:") {
		t.Errorf("expected added Goal 2 in diff.\ngot:\n%s", got)
	}
}

func TestFormatDeltaResults_GoalsSolved(t *testing.T) {
	prev := &ProofView{
		Goals: []ProofGoal{
			{Goal: "A"},
			{Goal: "B"},
			{Goal: "C"},
		},
	}
	cur := &ProofView{
		Goals: []ProofGoal{
			{Goal: "B"},
		},
	}
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	if !strings.Contains(got, "=== Proof Goals: 1 (-2) ===") {
		t.Errorf("expected negative goal delta.\ngot:\n%s", got)
	}
}

func TestFormatDeltaResults_NoGoals(t *testing.T) {
	prev := &ProofView{
		Goals: []ProofGoal{{Goal: "True"}},
	}
	cur := &ProofView{}
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	want := "No goals or diagnostics."
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFormatDeltaResults_WithDiagnostics(t *testing.T) {
	result := formatDeltaResults(nil, nil, []Diagnostic{
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
		Goals: []ProofGoal{
			{Goal: "P", Hypotheses: []string{"n : nat"}},
		},
	}
	result := formatDeltaResults(pv, pv, nil)
	got := resultText(result)
	if !strings.Contains(got, "No changes to proof state.") {
		t.Errorf("expected no-changes message.\ngot:\n%s", got)
	}
}

func TestFormatDeltaResults_MultiLineHypothesis(t *testing.T) {
	prev := &ProofView{
		Goals: []ProofGoal{
			{Goal: "P", Hypotheses: []string{"H : forall x y,\n  x = y -> y = x"}},
		},
	}
	cur := &ProofView{
		Goals: []ProofGoal{
			{Goal: "P", Hypotheses: []string{"H : forall x y z,\n  x = y -> y = z -> x = z"}},
		},
	}
	result := formatDeltaResults(prev, cur, nil)
	got := resultText(result)
	// Should show line-level changes within the multi-line hypothesis.
	if !strings.Contains(got, "-") && !strings.Contains(got, "+") {
		t.Errorf("expected diff markers for changed multi-line hypothesis.\ngot:\n%s", got)
	}
	// Old lines should be removed.
	if !strings.Contains(got, "-  H : forall x y,") {
		t.Errorf("expected old hypothesis first line removed.\ngot:\n%s", got)
	}
	// New lines should be added.
	if !strings.Contains(got, "+  H : forall x y z,") {
		t.Errorf("expected new hypothesis first line added.\ngot:\n%s", got)
	}
}

func TestFormatFullResults(t *testing.T) {
	pv := &ProofView{
		Goals: []ProofGoal{
			{ID: "1", Goal: "A", Hypotheses: []string{"H : True"}},
			{ID: "2", Goal: "B", Hypotheses: []string{"H : True", "n : nat"}},
		},
	}
	result := formatFullResults(pv, nil)
	got := resultText(result)
	want := `=== Proof Goals: 2 ===
Goal 1 (1):
  H : True
  ────────────────────
  A

Goal 2 (2):
  H : True
  n : nat
  ────────────────────
  B
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRenderProofText(t *testing.T) {
	pv := &ProofView{
		Goals: []ProofGoal{
			{ID: "1", Goal: "A", Hypotheses: []string{"H : True"}},
			{Goal: "B"},
		},
	}
	got := renderProofText(pv)
	want := `Goal 1 (1):
  H : True
  ────────────────────
  A

Goal 2:
  ────────────────────
  B
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestDiffText_Identical(t *testing.T) {
	got := diffText("hello\nworld\n", "hello\nworld\n")
	if got != "" {
		t.Errorf("expected empty diff for identical text, got:\n%s", got)
	}
}

func TestDiffText_Changes(t *testing.T) {
	old := "line1\nline2\nline3\n"
	new := "line1\nchanged\nline3\n"
	got := diffText(old, new)
	if !strings.Contains(got, "-line2") {
		t.Errorf("expected removed line in diff.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "+changed") {
		t.Errorf("expected added line in diff.\ngot:\n%s", got)
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
