package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	rocqmcp "github.com/sanjit-bhat/rocq-mcp/mcp"
	"github.com/sanjit-bhat/rocq-mcp/vsrocq"
)

// vsrocqBin returns the path to vsrocqtop, or skips the test if unavailable.
func vsrocqBin(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("VSROCQ_BIN"); p != "" {
		return p
	}
	if out, err := exec.Command("opam", "exec", "--switch", "pav", "--", "which", "vsrocqtop").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			return p
		}
	}
	if p, err := exec.LookPath("vsrocqtop"); err == nil {
		return p
	}
	t.Skip("vsrocqtop not found; set VSROCQ_BIN or install via opam switch pav")
	return ""
}

// startServer starts a rocqmcp.Server connected via in-memory MCP transport
// and returns a connected sdk.ClientSession for calling tools.
func startServer(t *testing.T, bin string) (*rocqmcp.Server, *sdk.ClientSession) {
	t.Helper()

	srv := rocqmcp.New(bin)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(func() {
		cancel()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
	})

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	mcpSrv := srv.MCPServer()
	st, ct := sdk.NewInMemoryTransports()

	// Connect server in background.
	go func() {
		_ = mcpSrv.Run(ctx, st)
	}()

	client := sdk.NewClient("test-client", "0.0.1", nil)
	cs, err := client.Connect(ctx, ct)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return srv, cs
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) rocqmcp.CheckResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	result, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      name,
		Arguments: json.RawMessage(argsJSON),
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if result.IsError {
		t.Fatalf("tool %s returned error: %v", name, result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatalf("tool %s returned no content", name)
	}
	tc, ok := result.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("tool %s content[0] is not TextContent", name)
	}

	var cr rocqmcp.CheckResult
	if err := json.Unmarshal([]byte(tc.Text), &cr); err != nil {
		t.Fatalf("unmarshal CheckResult from %s: %v\nraw: %s", name, err, tc.Text)
	}
	return cr
}

// TestOmitCheckUpdateClose is the end-to-end scenario:
//  1. check with omit (two-phase) → should surface proof goals
//  2. update the file (complete the proof)
//  3. check_to_end → should have no errors, no goals
//  4. close_file
func TestOmitCheckUpdateClose(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()
	_, cs := startServer(t, bin)

	// Initial content: lem1 complete, lem2 proof open.
	initial := `Lemma lem1 : True.
Proof. trivial. Qed.

Lemma lem2 : 1 = 1.
Proof.
`
	path := filepath.Join(dir, "test.v")
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Step 1: check with omit=2 (skip lem1 proof body), to_line=4 (stop at Proof.)
	// Phase 1 builds up to line 2 with delegation:Skip.
	// Phase 2 checks lines 3-4 (lem2 statement + Proof.) with delegation:None.
	// We expect at least one goal (1 = 1) in ProofState.
	r1 := callTool(t, cs, "check", map[string]any{
		"path":    path,
		"to_line": 4,
		"omit":    2,
	})
	t.Logf("check with omit: checked_to=%d errors=%v goals=%q", r1.CheckedTo, r1.Errors, r1.ProofGoals)
	if len(r1.Errors) != 0 {
		t.Errorf("expected no errors after omit check, got %v", r1.Errors)
	}
	if r1.ProofGoals == "" {
		t.Error("expected proof goals (1 = 1) after stopping inside proof, got empty string")
	}

	// Step 2: update file to complete lem2.
	complete := `Lemma lem1 : True.
Proof. trivial. Qed.

Lemma lem2 : 1 = 1.
Proof.
  reflexivity.
Qed.
`
	if err := os.WriteFile(path, []byte(complete), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Step 3: check_to_end — both lemmas should be proven, no goals.
	r2 := callTool(t, cs, "check_to_end", map[string]any{
		"path": path,
	})
	t.Logf("check_to_end: checked_to=%d errors=%v goals=%q", r2.CheckedTo, r2.Errors, r2.ProofGoals)
	if len(r2.Errors) != 0 {
		t.Errorf("expected no errors after completing proofs, got %v", r2.Errors)
	}

	// Step 4: close_file — should succeed.
	callTool(t, cs, "close_file", map[string]any{
		"path": path,
	})
}

// TestCheckToEndNoErrors verifies a trivially correct file has no errors.
func TestCheckToEndNoErrors(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()
	_, cs := startServer(t, bin)

	path := filepath.Join(dir, "ok.v")
	if err := os.WriteFile(path, []byte("Definition x := 42.\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := callTool(t, cs, "check_to_end", map[string]any{"path": path})
	if len(r.Errors) != 0 {
		t.Errorf("expected no errors, got %v", r.Errors)
	}
}

// TestCheckToEndWithError verifies a file with an error surfaces the error.
func TestCheckToEndWithError(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()
	_, cs := startServer(t, bin)

	path := filepath.Join(dir, "bad.v")
	if err := os.WriteFile(path, []byte("Definition bad := zar.\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := callTool(t, cs, "check_to_end", map[string]any{"path": path})
	if len(r.Errors) == 0 {
		t.Error("expected an error for undefined 'zar', got none")
	}
	t.Logf("error: %v", r.Errors)
}

// TestCheckToLine verifies partial checking stops at the given line.
func TestCheckToLine(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()
	_, cs := startServer(t, bin)

	content := "Definition a := 1.\nDefinition bad := zar.\n"
	path := filepath.Join(dir, "partial.v")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Check only line 0 (first definition, which is fine).
	r := callTool(t, cs, "check", map[string]any{
		"path":    path,
		"to_line": 0,
	})
	t.Logf("check line 0: checked_to=%d errors=%v", r.CheckedTo, r.Errors)
	if len(r.Errors) != 0 {
		t.Errorf("expected no errors when checking only line 0, got %v", r.Errors)
	}
}

// TestProofGoalsPlaintext verifies that an in-progress proof returns non-empty
// plaintext goals containing the expected goal term.
func TestProofGoalsPlaintext(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()
	_, cs := startServer(t, bin)

	// Open a proof for "1 = 1" but don't close it.
	content := "Lemma lem : 1 = 1.\nProof.\n"
	path := filepath.Join(dir, "goals.v")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Check to line 1 (the "Proof." line) — goal should be live.
	r := callTool(t, cs, "check", map[string]any{
		"path":    path,
		"to_line": 1,
	})
	want := "1 goal\n\n============================\n1 = 1\n"
	if r.ProofGoals != want {
		t.Errorf("proof_goals = %q, want %q", r.ProofGoals, want)
	}
}

// TestUpdateFile verifies that a second call reflects file changes.
func TestUpdateFile(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()
	_, cs := startServer(t, bin)

	path := filepath.Join(dir, "update.v")
	if err := os.WriteFile(path, []byte("Definition bad := zar.\n"), 0644); err != nil {
		t.Fatalf("write initial: %v", err)
	}
	r1 := callTool(t, cs, "check_to_end", map[string]any{"path": path})
	if len(r1.Errors) == 0 {
		t.Error("expected error in first check")
	}

	// Fix the file.
	if err := os.WriteFile(path, []byte("Definition ok := 1.\n"), 0644); err != nil {
		t.Fatalf("write fixed: %v", err)
	}
	r2 := callTool(t, cs, "check_to_end", map[string]any{"path": path})
	if len(r2.Errors) != 0 {
		t.Errorf("expected no errors after fix, got %v", r2.Errors)
	}
}

// TestDelegationSkipOmitsFoo verifies that check with omit skips foo's proof
// body (Phase 1 DelegationSkip) and returns bar's proof state.
//
// File layout (0-indexed lines):
//
//	0: Lemma foo (x y : nat) : x + y = y + x.
//	1: Proof.
//	2:   reflexivity.            ← would fail if executed in Phase 2
//	3: Qed.
//	4: Lemma bar (x : nat) : x = x.
//	5: Proof.                   ← cursor; bar's goal (x = x) is live here
//
// With omit=2 Phase 1 uses DelegationSkip up to line 2: foo's proof sentences
// are dropped, only Qed runs (fails, foo auto-admitted).  Phase 2 (DelegationNone)
// verifies bar's proof state starting from the cursor at line 5.
func TestDelegationSkipOmitsFoo(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()
	_, cs := startServer(t, bin)

	const content = "" +
		"Lemma foo (x y : nat) : x + y = y + x.\n" + // 0
		"Proof.\n" + // 1
		"  reflexivity.\n" + // 2
		"Qed.\n" + // 3
		"Lemma bar (x : nat) : x = x.\n" + // 4
		"Proof.\n" // 5

	path := filepath.Join(dir, "omit.v")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := callTool(t, cs, "check", map[string]any{
		"path":    path,
		"to_line": 5,
		"omit":    2,
	})
	t.Logf("check: checked_to=%d errors=%v goals=%q", r.CheckedTo, r.Errors, r.ProofGoals)

	wantGoals := rocqmcp.FormatProofState(&vsrocq.StringProofState{
		Goals: []vsrocq.StringGoal{{
			Hypotheses: []string{"x : nat"},
			Goal:       "x = x",
		}},
	})
	if r.ProofGoals != wantGoals {
		t.Errorf("proof_goals = %q, want %q", r.ProofGoals, wantGoals)
	}
	for _, e := range r.Errors {
		if e.Line >= 4 {
			t.Errorf("unexpected error on bar: line %d: %s", e.Line, e.Message)
		}
	}
}
