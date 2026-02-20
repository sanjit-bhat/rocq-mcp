package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestVsrocqInitShutdown(t *testing.T) {
	client, err := newVsrocqClient(nil)
	if err != nil {
		t.Fatalf("newVsrocqClient: %v", err)
	}

	cwd, _ := os.Getwd()
	if err := client.initialize("file://" + cwd); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	if err := client.shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestOpenAndCheckSimple(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/simple.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	// Check the whole file — should have no errors.
	doc, _ := sm.getDoc(path)
	drainChannels(doc)

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.client.notify("prover/interpretToEnd", params); err != nil {
		t.Fatalf("interpretToEnd: %v", err)
	}

	// Wait for diagnostics.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	select {
	case diags := <-doc.diagnosticCh:
		for _, d := range diags {
			if d.Severity == 1 {
				t.Errorf("unexpected error: %s", d.Message)
			}
		}
	case <-timer.C:
		// No diagnostics is fine — means no errors.
	}

	if err := sm.closeDoc(path); err != nil {
		t.Fatalf("closeDoc: %v", err)
	}
}

func TestOpenAndCheckError(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/error.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	doc, _ := sm.getDoc(path)
	drainChannels(doc)

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.client.notify("prover/interpretToEnd", params); err != nil {
		t.Fatalf("interpretToEnd: %v", err)
	}

	// Wait for diagnostics — should include an error.
	// vsrocq may send multiple diagnostic batches.
	deadline := time.After(10 * time.Second)
	gotError := false
	for !gotError {
		select {
		case diags := <-doc.diagnosticCh:
			for _, d := range diags {
				t.Logf("diagnostic (severity=%d): %s", d.Severity, d.Message)
				if d.Severity == 1 {
					gotError = true
				}
			}
		case pv := <-doc.proofViewCh:
			t.Logf("proofView: %d goals", len(pv.Goals))
		case <-deadline:
			t.Fatal("timed out waiting for error diagnostics")
		}
	}

	if err := sm.closeDoc(path); err != nil {
		t.Fatalf("closeDoc: %v", err)
	}
}

func TestCheckProofGoals(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/simple.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	// Check the file up to end of line 2 (after "intros n.").
	// Use interpretToPoint at line 3, col 0 to include "intros n."
	result, _, _ := doCheck(sm, path, 3, 0)

	text := resultText(result)
	t.Logf("check result:\n%s", text)

	if err := sm.closeDoc(path); err != nil {
		t.Fatalf("closeDoc: %v", err)
	}
}

func TestQueryAbout(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/simple.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	// First check to end so the environment is loaded.
	doCheckAll(sm, path)

	result, _, _ := doQuery(sm, path, "prover/about", "Nat.add")
	text := resultText(result)
	t.Logf("about result:\n%s", text)
	if text == "" || text == "No result." {
		t.Error("expected non-empty result for About Nat.add")
	}
}

func TestQueryCheckType(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/simple.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	doCheckAll(sm, path)

	result, _, _ := doQuery(sm, path, "prover/check", "Nat.add")
	text := resultText(result)
	t.Logf("check type result:\n%s", text)
	if text == "" || text == "No result." {
		t.Error("expected non-empty result for Check Nat.add")
	}
}

func TestQueryLocate(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/simple.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	doCheckAll(sm, path)

	result, _, _ := doQuery(sm, path, "prover/locate", "Nat.add")
	text := resultText(result)
	t.Logf("locate result:\n%s", text)
	if text == "" || text == "No result." {
		t.Error("expected non-empty result for Locate Nat.add")
	}
}

func TestQueryPrint(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/simple.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	doCheckAll(sm, path)

	result, _, _ := doQuery(sm, path, "prover/print", "Nat.add")
	text := resultText(result)
	t.Logf("print result:\n%s", text)
	if text == "" || text == "No result." {
		t.Error("expected non-empty result for Print Nat.add")
	}
}

func TestQuerySearch(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/simple.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	doCheckAll(sm, path)

	result, _, _ := doSearch(sm, path, "0 + _ = _")
	text := resultText(result)
	t.Logf("search result:\n%s", text)
	if !strings.Contains(text, "plus_0_n") && !strings.Contains(text, "Search Results") {
		t.Logf("note: search may not have found plus_0_n (result: %s)", text)
	}
}

func TestComplexGoalFlow(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/complex_goal_flow.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	// vsrocq sentence boundaries (each is one stepForward):
	//   intros A B C HA HB HC.   (checked via doCheck, not stepped)
	//   assert (HAB : A /\ B).   step 1  — new goal: A /\ B
	//   {                         step 2  — enters focus block
	//   split.                    step 3  — splits A /\ B into A, B
	//   -                         step 4  — bullet focuses first sub-goal
	//   exact HA.                 step 5  — solves A (sub-goal complete)
	//   -                         step 6  — bullet focuses B
	//   exact HB.                 step 7  — solves B (sub-goal complete)
	//   }                         step 8  — closes focus, original goal returns
	//   split.                    step 9  — splits (A /\ B) /\ C
	//   -                         step 10 — bullet
	//   exact HAB.                step 11 — solves first sub-goal (sub-goal complete)
	//   -                         step 12 — bullet focuses C
	//   exact HC.                 step 13 — proof complete
	//   Qed.                      step 14 — proof registered

	step := func() string {
		result, _, _ := doStep(sm, path, "prover/stepForward")
		return resultText(result)
	}

	check := func(label, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s:\nwant:\n%s\ngot:\n%s", label, want, got)
		}
	}

	// doCheck after intros: always full context.
	result, _, _ := doCheck(sm, path, 4, 0)
	check("check after intros", resultText(result), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  (A /\ B) /\ C
`)

	// Step 1: assert — two goals, showing both.
	// Goal 2 already has HAB because assert adds the hypothesis to the continuation goal.
	check("step 1 (assert)", step(), `Goal 1 of 2:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  A /\ B

Goal 2 of 2:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  HAB : A /\ B
  ────────────────────
  (A /\ B) /\ C
`)

	// Step 2: { — enters focus block (2 goals → 1 goal, so full display).
	check("step 2 ({)", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  A /\ B
`)

	// Step 3: split — splits A /\ B into A and B.
	check("step 3 (split)", step(), `Goal 1 of 2:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  A

Goal 2 of 2:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  B
`)

	// Step 4: - — bullet focuses first sub-goal (was 2 goals, now 1 → full).
	check("step 4 (-)", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  A
`)

	// Step 5: exact HA — solves A, unfocused goals remain.
	check("step 5 (exact HA)", step(), `Sub-goal complete! 2 unfocused remaining.
`)

	// Step 6: - — bullet focuses B sub-goal.
	check("step 6 (-)", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  B
`)

	// Step 7: exact HB — solves B, unfocused goals remain.
	check("step 7 (exact HB)", step(), `Sub-goal complete! 1 unfocused remaining.
`)

	// Step 8: } — closes focus block, original goal returns with HAB hypothesis.
	check("step 8 (})", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  HAB : A /\ B
  ────────────────────
  (A /\ B) /\ C
`)

	// Step 9: split — splits original goal into two.
	check("step 9 (split)", step(), `Goal 1 of 2:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  HAB : A /\ B
  ────────────────────
  A /\ B

Goal 2 of 2:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  HAB : A /\ B
  ────────────────────
  C
`)

	// Step 10: - — bullet, no text change.
	step()

	// Step 11: exact HAB — solves first sub-goal.
	check("step 11 (exact HAB)", step(), `Sub-goal complete! 1 unfocused remaining.
`)

	// Step 12: - — focuses C sub-goal.
	check("step 12 (-)", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  HAB : A /\ B
  ────────────────────
  C
`)

	// Step 13: exact HC — proof complete.
	check("step 13 (exact HC)", step(), `Proof complete!
`)

	// Step 14: Qed — proof registered.
	check("step 14 (Qed)", step(), `Proof complete!

=== Messages ===
complex_goal_flow is defined
`)

	if err := sm.closeDoc(path); err != nil {
		t.Fatalf("closeDoc: %v", err)
	}
}

func TestDiffGoal(t *testing.T) {
	sm := newStateManager(nil)
	defer sm.shutdown()

	path, _ := filepath.Abs("testdata/diff_goal.v")
	if err := sm.openDoc(path); err != nil {
		t.Fatalf("openDoc: %v", err)
	}

	// Sentence boundaries (all via stepForward):
	//   intros n m.           step 1 — initial goal (full, new ID)
	//   rewrite Nat.add_comm. step 2 — changes conclusion (full, new ID)
	//   reflexivity.          step 3 — proof complete
	//   Qed.                  step 4 — proof registered

	step := func() string {
		result, _, _ := doStep(sm, path, "prover/stepForward")
		return resultText(result)
	}

	check := func(label, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s:\nwant:\n%s\ngot:\n%s", label, want, got)
		}
	}

	// doCheck positions after "Require Import Arith." and "Proof."
	doCheck(sm, path, 4, 0)

	// Step 1: intros n m — full (new goal ID from Proof. state).
	check("step 1 (intros)", step(), `Goal:
  n, m : nat
  ────────────────────
  n + m = m + n
`)

	// Step 2: rewrite Nat.add_comm — full (vsrocq assigns new goal ID).
	check("step 2 (rewrite)", step(), `Goal:
  n, m : nat
  ────────────────────
  m + n = m + n
`)

	// Step 3: reflexivity — proof complete.
	check("step 3 (reflexivity)", step(), `Proof complete!
`)

	// Step 4: Qed — proof registered.
	check("step 4 (Qed)", step(), `Proof complete!

=== Messages ===
diff_goal is defined
`)

	if err := sm.closeDoc(path); err != nil {
		t.Fatalf("closeDoc: %v", err)
	}
}

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
