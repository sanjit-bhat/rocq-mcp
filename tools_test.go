package main

import (
	"strings"
	"testing"
)

func TestFormatDeltaResults_NoPrevious(t *testing.T) {
	pv := &ProofView{
		Goals: []ProofGoal{
			{
				ID:         "1",
				Goal:       "0 + n = n",
				Hypotheses: []string{"n : nat"},
			},
		},
	}
	result := formatDeltaResults(nil, pv, nil)
	text := resultText(result)

	if !strings.Contains(text, "Proof Goals: 1") {
		t.Errorf("expected goal count, got:\n%s", text)
	}
	if !strings.Contains(text, "Focused Goal") {
		t.Errorf("expected focused goal header, got:\n%s", text)
	}
	if !strings.Contains(text, "n : nat") {
		t.Errorf("expected hypothesis, got:\n%s", text)
	}
	if !strings.Contains(text, "0 + n = n") {
		t.Errorf("expected goal conclusion, got:\n%s", text)
	}
	// No previous state — hypotheses should not be prefixed with +/-.
	if strings.Contains(text, "+ n : nat") || strings.Contains(text, "- n : nat") {
		t.Errorf("first proof view should not have +/- markers:\n%s", text)
	}
}

func TestFormatDeltaResults_AddedHypothesis(t *testing.T) {
	prev := &ProofView{
		Goals: []ProofGoal{
			{Goal: "forall n, 0 + n = n", Hypotheses: nil},
		},
	}
	cur := &ProofView{
		Goals: []ProofGoal{
			{Goal: "0 + n = n", Hypotheses: []string{"n : nat"}},
		},
	}
	result := formatDeltaResults(prev, cur, nil)
	text := resultText(result)

	if !strings.Contains(text, "+ n : nat") {
		t.Errorf("expected added hypothesis marker, got:\n%s", text)
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
	text := resultText(result)

	if !strings.Contains(text, "- H : True") {
		t.Errorf("expected removed hypothesis marker, got:\n%s", text)
	}
	// n : nat should not be marked.
	if strings.Contains(text, "+ n : nat") {
		t.Errorf("unchanged hypothesis should not be marked as added:\n%s", text)
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
	text := resultText(result)

	if !strings.Contains(text, "Proof Goals: 2 (+1)") {
		t.Errorf("expected goal count delta, got:\n%s", text)
	}
	// Goal 2 should just show conclusion, no hypotheses.
	if !strings.Contains(text, "Goal 2: B") {
		t.Errorf("expected summarized non-focused goal, got:\n%s", text)
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
	text := resultText(result)

	if !strings.Contains(text, "Proof Goals: 1 (-2)") {
		t.Errorf("expected negative goal count delta, got:\n%s", text)
	}
}

func TestFormatDeltaResults_NoGoals(t *testing.T) {
	prev := &ProofView{
		Goals: []ProofGoal{{Goal: "True"}},
	}
	cur := &ProofView{
		Goals: nil,
	}
	result := formatDeltaResults(prev, cur, nil)
	text := resultText(result)

	if !strings.Contains(text, "No goals or diagnostics.") {
		t.Errorf("expected no goals message, got:\n%s", text)
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
	text := resultText(result)

	if !strings.Contains(text, "[error]") {
		t.Errorf("expected error diagnostic, got:\n%s", text)
	}
	if !strings.Contains(text, "type error") {
		t.Errorf("expected diagnostic message, got:\n%s", text)
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
	text := resultText(result)

	// Full results should show all hypotheses for all goals.
	if !strings.Contains(text, "Goal 1") {
		t.Errorf("expected Goal 1, got:\n%s", text)
	}
	if !strings.Contains(text, "Goal 2") {
		t.Errorf("expected Goal 2, got:\n%s", text)
	}
	if !strings.Contains(text, "n : nat") {
		t.Errorf("expected hypothesis in goal 2, got:\n%s", text)
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
	text := sb.String()

	if !strings.Contains(text, "- H : a = b") {
		t.Errorf("expected removed H, got:\n%s", text)
	}
	if !strings.Contains(text, "+ H' : b = a") {
		t.Errorf("expected added H', got:\n%s", text)
	}
	// Unchanged ones should not be marked.
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "a : nat" || trimmed == "b : nat" {
			if strings.HasPrefix(strings.TrimSpace(line), "+") || strings.HasPrefix(strings.TrimSpace(line), "-") {
				t.Errorf("unchanged hypothesis should not be marked:\n%s", text)
			}
		}
	}
}
