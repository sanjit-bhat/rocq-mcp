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
