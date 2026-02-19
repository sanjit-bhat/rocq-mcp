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

Focused Goal (1):
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
	want := `=== Proof Goals: 1 ===

Focused Goal:
  + n : nat
  ────────────────────
  0 + n = n
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
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
	want := `=== Proof Goals: 1 ===

Focused Goal:
  - H : True
  n : nat
  ────────────────────
  P
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
	want := `=== Proof Goals: 2 (+1) ===

Focused Goal:
  ────────────────────
  A

Goal 2: B
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
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
	want := `=== Proof Goals: 1 (-2) ===

Focused Goal:
  ────────────────────
  B
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
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

func TestWriteHypothesesDiff_MixedChanges(t *testing.T) {
	var sb strings.Builder
	prev := &ProofGoal{
		Hypotheses: []string{"a : nat", "b : nat", "H : a = b"},
	}
	cur := &ProofGoal{
		Hypotheses: []string{"a : nat", "b : nat", "H' : b = a"},
	}
	writeHypothesesDiff(&sb, prev, cur)
	got := sb.String()
	want := `  - H : a = b
  a : nat
  b : nat
  + H' : b = a
`
	if got != want {
		t.Errorf("mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}
