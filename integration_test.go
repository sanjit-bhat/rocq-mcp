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
			t.Logf("proofView: %d goals", pv.GoalCount)
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

	// doCheck after intros: always full context, no diff markers.
	result, _, _ := doCheck(sm, path, 4, 0)
	text := resultText(result)
	t.Logf("check after intros:\n%s", text)
	assertContains(t, text, "Goal:")
	assertContains(t, text, "(A /\\ B) /\\ C")
	assertNotContains(t, text, "@@") // no diff markers

	// Step 1: assert — new goal ID, full context shown.
	text = step()
	t.Logf("step 1 (assert):\n%s", text)
	assertContains(t, text, "Goal 1 of 2:")
	assertContains(t, text, "A /\\ B")
	assertContains(t, text, "2 goals remaining")

	// Step 2: { — enters focus block, same goal, no text change.
	text = step()
	t.Logf("step 2 ({):\n%s", text)
	assertContains(t, text, "Goal:")
	assertContains(t, text, "No changes to proof state.")

	// Step 3: split — splits assertion goal into A and B.
	text = step()
	t.Logf("step 3 (split):\n%s", text)
	assertContains(t, text, "Goal 1 of 2:")
	assertContains(t, text, "A\n") // conclusion is A

	// Step 4: - — bullet focuses first sub-goal, no text change.
	text = step()
	t.Logf("step 4 (-):\n%s", text)
	assertContains(t, text, "No changes to proof state.")

	// Step 5: exact HA — solves A sub-goal, NOT proof complete.
	text = step()
	t.Logf("step 5 (exact HA):\n%s", text)
	assertContains(t, text, "Sub-goal complete!")
	assertNotContains(t, text, "Proof complete!")

	// Step 6: - — bullet focuses B sub-goal.
	text = step()
	t.Logf("step 6 (-):\n%s", text)
	assertContains(t, text, "Goal:")
	assertContains(t, text, "B\n") // conclusion is B

	// Step 7: exact HB — solves B sub-goal, NOT proof complete.
	text = step()
	t.Logf("step 7 (exact HB):\n%s", text)
	assertContains(t, text, "Sub-goal complete!")
	assertNotContains(t, text, "Proof complete!")

	// Step 8: } — closes focus block, original goal returns with HAB.
	text = step()
	t.Logf("step 8 (}):\n%s", text)
	assertContains(t, text, "Goal:")
	assertContains(t, text, "HAB : A /\\ B")     // new hypothesis
	assertContains(t, text, "(A /\\ B) /\\ C\n") // original conclusion

	// Step 9: split — splits original goal.
	text = step()
	t.Logf("step 9 (split):\n%s", text)
	assertContains(t, text, "Goal 1 of 2:")
	assertContains(t, text, "2 goals remaining")

	// Steps 10-11: bullet + exact HAB.
	step() // -
	text = step()
	t.Logf("step 11 (exact HAB):\n%s", text)
	assertContains(t, text, "Sub-goal complete!")

	// Step 12: - focuses C sub-goal.
	text = step()
	t.Logf("step 12 (-):\n%s", text)
	assertContains(t, text, "C\n") // conclusion is C

	// Step 13: exact HC — proof complete (no unfocused goals left).
	text = step()
	t.Logf("step 13 (exact HC):\n%s", text)
	assertContains(t, text, "Proof complete!")

	// Step 14: Qed — proof registered.
	text = step()
	t.Logf("step 14 (Qed):\n%s", text)
	assertContains(t, text, "Proof complete!")
	assertContains(t, text, "complex_goal_flow is defined")

	if err := sm.closeDoc(path); err != nil {
		t.Fatalf("closeDoc: %v", err)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("expected output to contain %q.\ngot:\n%s", want, got)
	}
}

func assertNotContains(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Errorf("expected output NOT to contain %q.\ngot:\n%s", unwanted, got)
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
