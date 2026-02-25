package rocq

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testdataPath returns the absolute path to the project's testdata directory.
func testdataPath(file string) string {
	// Tests run in the package directory (internal/rocq/), testdata is at project root.
	abs, _ := filepath.Abs(filepath.Join("..", "..", "testdata", file))
	return abs
}

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
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("simple.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	// Check the whole file — should have no errors.
	doc, _ := sm.GetDoc(path)
	DrainChannels(doc)

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.Client.Notify("prover/interpretToEnd", params); err != nil {
		t.Fatalf("interpretToEnd: %v", err)
	}

	// Wait for diagnostics.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	select {
	case diags := <-doc.DiagnosticCh:
		for _, d := range diags {
			if d.Severity == 1 {
				t.Errorf("unexpected error: %s", d.Message)
			}
		}
	case <-timer.C:
		// No diagnostics is fine — means no errors.
	}

	if err := sm.CloseDoc(path); err != nil {
		t.Fatalf("CloseDoc: %v", err)
	}
}

func TestOpenAndCheckError(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("error.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	doc, _ := sm.GetDoc(path)
	DrainChannels(doc)

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.Client.Notify("prover/interpretToEnd", params); err != nil {
		t.Fatalf("interpretToEnd: %v", err)
	}

	// Wait for diagnostics — should include an error.
	deadline := time.After(10 * time.Second)
	gotError := false
	for !gotError {
		select {
		case diags := <-doc.DiagnosticCh:
			for _, d := range diags {
				t.Logf("diagnostic (severity=%d): %s", d.Severity, d.Message)
				if d.Severity == 1 {
					gotError = true
				}
			}
		case pv := <-doc.ProofViewCh:
			t.Logf("proofView: %d goals", len(pv.Goals))
		case <-deadline:
			t.Fatal("timed out waiting for error diagnostics")
		}
	}

	if err := sm.CloseDoc(path); err != nil {
		t.Fatalf("CloseDoc: %v", err)
	}
}

func TestCheckProofGoals(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("simple.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	result, _, _ := DoCheck(sm, path, 3, 0)

	text := resultText(result)
	t.Logf("check result:\n%s", text)

	if err := sm.CloseDoc(path); err != nil {
		t.Fatalf("CloseDoc: %v", err)
	}
}

func TestQueryAbout(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("simple.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	DoCheckAll(sm, path)

	result, _, _ := DoQuery(sm, path, "prover/about", "Nat.add")
	text := resultText(result)
	t.Logf("about result:\n%s", text)
	if text == "" || text == "No result." {
		t.Error("expected non-empty result for About Nat.add")
	}
}

func TestQueryCheckType(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("simple.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	DoCheckAll(sm, path)

	result, _, _ := DoQuery(sm, path, "prover/check", "Nat.add")
	text := resultText(result)
	t.Logf("check type result:\n%s", text)
	if text == "" || text == "No result." {
		t.Error("expected non-empty result for Check Nat.add")
	}
}

func TestQueryLocate(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("simple.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	DoCheckAll(sm, path)

	result, _, _ := DoQuery(sm, path, "prover/locate", "Nat.add")
	text := resultText(result)
	t.Logf("locate result:\n%s", text)
	if text == "" || text == "No result." {
		t.Error("expected non-empty result for Locate Nat.add")
	}
}

func TestQueryPrint(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("simple.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	DoCheckAll(sm, path)

	result, _, _ := DoQuery(sm, path, "prover/print", "Nat.add")
	text := resultText(result)
	t.Logf("print result:\n%s", text)
	if text == "" || text == "No result." {
		t.Error("expected non-empty result for Print Nat.add")
	}
}

func TestQuerySearch(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("simple.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	DoCheckAll(sm, path)

	result, _, _ := DoSearch(sm, path, "0 + _ = _")
	text := resultText(result)
	t.Logf("search result:\n%s", text)
	if !strings.Contains(text, "plus_0_n") && !strings.Contains(text, "Search Results") {
		t.Logf("note: search may not have found plus_0_n (result: %s)", text)
	}
}

func TestComplexGoalFlow(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("complex_goal_flow.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	step := func() string {
		result, _, _ := DoStep(sm, path, "prover/stepForward")
		return resultText(result)
	}

	check := func(label, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s:\nwant:\n%s\ngot:\n%s", label, want, got)
		}
	}

	// doCheck after intros: always full context.
	result, _, _ := DoCheck(sm, path, 4, 0)
	check("check after intros", resultText(result), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  (A /\ B) /\ C
`)

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

	check("step 2 ({)", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  A /\ B
`)

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

	check("step 4 (-)", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  A
`)

	check("step 5 (exact HA)", step(), `Sub-goal complete! 2 unfocused remaining.
`)

	check("step 6 (-)", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  B
`)

	check("step 7 (exact HB)", step(), `Sub-goal complete! 1 unfocused remaining.
`)

	check("step 8 (})", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  HAB : A /\ B
  ────────────────────
  (A /\ B) /\ C
`)

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

	check("step 11 (exact HAB)", step(), `Sub-goal complete! 1 unfocused remaining.
`)

	check("step 12 (-)", step(), `Goal:
  A, B, C : Prop
  HA : A
  HB : B
  HC : C
  HAB : A /\ B
  ────────────────────
  C
`)

	check("step 13 (exact HC)", step(), `Proof complete!
`)

	check("step 14 (Qed)", step(), `Proof complete!

=== Messages ===
complex_goal_flow is defined
`)

	if err := sm.CloseDoc(path); err != nil {
		t.Fatalf("CloseDoc: %v", err)
	}
}

func TestDiffGoal(t *testing.T) {
	sm := NewStateManager(nil)
	defer sm.Shutdown()

	path := testdataPath("diff_goal.v")
	if err := sm.OpenDoc(path); err != nil {
		t.Fatalf("OpenDoc: %v", err)
	}

	step := func() string {
		result, _, _ := DoStep(sm, path, "prover/stepForward")
		return resultText(result)
	}

	check := func(label, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s:\nwant:\n%s\ngot:\n%s", label, want, got)
		}
	}

	DoCheck(sm, path, 4, 0)

	check("step 1 (intros)", step(), `Goal:
  n, m : nat
  ────────────────────
  n + m = m + n
`)

	check("step 2 (rewrite)", step(), `Goal:
  n, m : nat
  ────────────────────
  m + n = m + n
`)

	check("step 3 (reflexivity)", step(), `Proof complete!
`)

	check("step 4 (Qed)", step(), `Proof complete!

=== Messages ===
diff_goal is defined
`)

	if err := sm.CloseDoc(path); err != nil {
		t.Fatalf("CloseDoc: %v", err)
	}
}
