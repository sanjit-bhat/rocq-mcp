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
