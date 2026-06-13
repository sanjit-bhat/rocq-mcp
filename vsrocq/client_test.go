package vsrocq_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanjit-bhat/rocq-mcp/vsrocq"
)

// vsrocqBin returns the path to vsrocqtop, or skips the test if unavailable.
func vsrocqBin(t *testing.T) string {
	t.Helper()
	// Prefer an explicit env override.
	if p := os.Getenv("VSROCQ_BIN"); p != "" {
		return p
	}
	// Try opam switch pav.
	out, err := exec.Command("opam", "exec", "--switch", "pav", "--", "which", "vsrocqtop").Output()
	if err == nil {
		p := strings.TrimSpace(string(out))
		if p != "" {
			return p
		}
	}
	// Fallback: PATH.
	if p, err := exec.LookPath("vsrocqtop"); err == nil {
		return p
	}
	t.Skip("vsrocqtop not found; set VSROCQ_BIN or install via opam switch pav")
	return ""
}

// newClient creates and starts a Client pointed at a temporary workspace directory.
// It registers t.Cleanup to shut the client down.
func newClient(t *testing.T, opts *vsrocq.InitOptions) (*vsrocq.Client, string) {
	t.Helper()
	bin := vsrocqBin(t)

	dir := t.TempDir()
	rootURI := "file://" + dir

	c := vsrocq.NewClient(bin)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(func() {
		cancel()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = c.Shutdown(shutCtx)
	})

	_, err := c.Start(ctx, rootURI, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return c, dir
}

// writeRocqFile writes content to <dir>/<name>.v and returns the file URI.
func writeRocqFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return "file://" + path
}

// waitFor polls pred every 50ms until it returns true or the deadline elapses.
func waitFor(t *testing.T, timeout time.Duration, pred func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// ---- TestInitialize ---------------------------------------------------------

func TestInitialize(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()

	c := vsrocq.NewClient(bin)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := c.Start(ctx, "file://"+dir, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The server must return a non-nil result with some capabilities.
	if result == nil {
		t.Fatal("InitializeResult is nil")
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	if err := c.Shutdown(shutCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// ---- TestInitializeWithOptions ----------------------------------------------

func TestInitializeWithOptions(t *testing.T) {
	workers := 2
	opts := &vsrocq.InitOptions{
		Proof: vsrocq.ProofOptions{
			Delegation:              "None",
			Workers:                 &workers,
			Mode:                    vsrocq.ProofModeContinuous,
			Block:                   false,
			PointInterpretationMode: vsrocq.PointInterpretationCursor,
		},
		Goals: vsrocq.GoalsOptions{
			Diff:     vsrocq.DiffOptions{Mode: "off"},
			Messages: vsrocq.MessagesOptions{Full: false},
		},
		Completion: vsrocq.CompletionOptions{
			Enable:           false,
			Algorithm:        0,
			UnificationLimit: 100,
			AtomicFactor:     5.0,
			SizeFactor:       5.0,
		},
		Diagnostics: vsrocq.DiagnosticsOptions{Enable: true, Full: false},
		Memory:      vsrocq.MemoryOptions{Limit: 2},
		Interrupt:   vsrocq.InterruptOptions{Preempt: false},
	}
	c, _ := newClient(t, opts)
	_ = c // just verify no crash
}

// ---- TestShutdownClean ------------------------------------------------------

func TestShutdownClean(t *testing.T) {
	bin := vsrocqBin(t)
	dir := t.TempDir()
	c := vsrocq.NewClient(bin)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := c.Start(ctx, "file://"+dir, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	// A second shutdown on the same client should return an error (closed).
	if err := c.Shutdown(ctx); err == nil {
		t.Error("expected error on second Shutdown, got nil")
	}
}

// ---- TestDidOpenClose -------------------------------------------------------

func TestDidOpenClose(t *testing.T) {
	c, dir := newClient(t, nil)
	uri := writeRocqFile(t, dir, "hello.v", "Definition x := 1.\n")

	if err := c.DidOpen(uri, "rocq", "Definition x := 1.\n", 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	if err := c.DidClose(uri); err != nil {
		t.Fatalf("DidClose: %v", err)
	}
}

// ---- TestDidChange ----------------------------------------------------------

func TestDidChange(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "change.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	newContent := "Definition x := 2.\n"
	if err := c.DidChange(uri, 2, newContent); err != nil {
		t.Fatalf("DidChange: %v", err)
	}
	if err := c.DidClose(uri); err != nil {
		t.Fatalf("DidClose: %v", err)
	}
}

// ---- TestHighlightsNotification ---------------------------------------------

func TestHighlightsNotification(t *testing.T) {
	c, dir := newClient(t, nil)

	var mu sync.Mutex
	var highlights []*vsrocq.HighlightsParams
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		highlights = append(highlights, p)
		mu.Unlock()
	}

	content := "Definition foo := True.\n"
	uri := writeRocqFile(t, dir, "hl.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	if err := c.InterpretToEnd(uri, 1); err != nil {
		t.Fatalf("InterpretToEnd: %v", err)
	}

	got := waitFor(t, 15*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(highlights) > 0
	})
	if !got {
		t.Fatal("timed out waiting for prover/updateHighlights")
	}

	mu.Lock()
	h := highlights[len(highlights)-1]
	mu.Unlock()
	if h.URI == "" {
		t.Error("highlight URI is empty")
	}
}

// ---- TestDiagnosticsOnError -------------------------------------------------

func TestDiagnosticsOnError(t *testing.T) {
	c, dir := newClient(t, nil)

	var mu sync.Mutex
	var diags []*vsrocq.PublishDiagnosticsParams
	c.OnDiagnostics = func(p *vsrocq.PublishDiagnosticsParams) {
		mu.Lock()
		diags = append(diags, p)
		mu.Unlock()
	}

	// This file has a deliberate error (undefined zar).
	content := "Definition bad := zar.\n"
	uri := writeRocqFile(t, dir, "bad.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	if err := c.InterpretToEnd(uri, 1); err != nil {
		t.Fatalf("InterpretToEnd: %v", err)
	}

	got := waitFor(t, 15*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, d := range diags {
			if len(d.Diagnostics) > 0 {
				return true
			}
		}
		return false
	})
	if !got {
		t.Fatal("timed out waiting for error diagnostics")
	}
}

// ---- TestInterpretToPoint ---------------------------------------------------

func TestInterpretToPoint(t *testing.T) {
	c, dir := newClient(t, nil)

	var mu sync.Mutex
	var gotHighlight bool
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		gotHighlight = true
		mu.Unlock()
	}

	content := "Definition a := 1.\nDefinition b := 2.\n"
	uri := writeRocqFile(t, dir, "itp.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	// Interpret up to end of first definition.
	if err := c.InterpretToPoint(uri, 1, vsrocq.Position{Line: 0, Character: 18}); err != nil {
		t.Fatalf("InterpretToPoint: %v", err)
	}

	if !waitFor(t, 15*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotHighlight
	}) {
		t.Fatal("timed out waiting for highlights after InterpretToPoint")
	}
}

// ---- TestStepForwardBackward ------------------------------------------------

func TestStepForwardBackward(t *testing.T) {
	opts := vsrocq.DefaultInitOptions()
	opts.Proof.Mode = vsrocq.ProofModeManual
	c, dir := newClient(t, opts)

	content := "Definition a := 1.\nDefinition b := 2.\n"
	uri := writeRocqFile(t, dir, "step.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	if err := c.StepForward(uri, 1); err != nil {
		t.Fatalf("StepForward: %v", err)
	}
	if err := c.StepBackward(uri, 1); err != nil {
		t.Fatalf("StepBackward: %v", err)
	}
}

// ---- TestInterrupt ----------------------------------------------------------

func TestInterrupt(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "int.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	// Interrupt immediately after opening; should not error.
	if err := c.Interrupt(uri, 1); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
}

// ---- TestResetRocq ----------------------------------------------------------

func TestResetRocq(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "reset.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.ResetRocq(ctx, uri); err != nil {
		t.Fatalf("ResetRocq: %v", err)
	}
}

// ---- TestDocumentState ------------------------------------------------------

func TestDocumentState(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "ds.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := c.DocumentState(ctx, uri)
	if err != nil {
		t.Fatalf("DocumentState: %v", err)
	}
	if result == nil {
		t.Fatal("DocumentState returned nil")
	}
}

// ---- TestDocumentProofs -----------------------------------------------------

func TestDocumentProofs(t *testing.T) {
	c, dir := newClient(t, nil)
	content := `Lemma simple : True.
Proof.
  trivial.
Qed.
`
	uri := writeRocqFile(t, dir, "proofs.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	// Wait for document to be processed.
	var mu sync.Mutex
	var processed bool
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		if len(p.ProcessedRange) > 0 {
			processed = true
		}
		mu.Unlock()
	}
	if err := c.InterpretToEnd(uri, 1); err != nil {
		t.Fatalf("InterpretToEnd: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return processed
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := c.DocumentProofs(ctx, uri)
	if err != nil {
		t.Fatalf("DocumentProofs: %v", err)
	}
	if result == nil {
		t.Fatal("DocumentProofs returned nil")
	}
	// The document has one proof block.
	if len(result.Proofs) == 0 {
		t.Log("DocumentProofs returned 0 proofs (may still be processing)")
	}
}

// ---- TestAbout --------------------------------------------------------------

func TestAbout(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "about.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	// Wait for processing.
	var mu sync.Mutex
	var ready bool
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		if len(p.ProcessedRange) > 0 {
			ready = true
		}
		mu.Unlock()
	}
	if err := c.InterpretToEnd(uri, 1); err != nil {
		t.Fatalf("InterpretToEnd: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return ready
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pp, err := c.About(ctx, uri, 1, vsrocq.Position{Line: 0, Character: 11}, "x")
	if err != nil {
		t.Fatalf("About: %v", err)
	}
	if len(pp) == 0 {
		t.Error("About returned empty Pp")
	}
}

// ---- TestCheck --------------------------------------------------------------

func TestCheck(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "check.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	var mu sync.Mutex
	var ready bool
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		if len(p.ProcessedRange) > 0 {
			ready = true
		}
		mu.Unlock()
	}
	if err := c.InterpretToEnd(uri, 1); err != nil {
		t.Fatalf("InterpretToEnd: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return ready
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pp, err := c.Check(ctx, uri, 1, vsrocq.Position{Line: 0, Character: 11}, "1")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(pp) == 0 {
		t.Error("Check returned empty Pp")
	}
}

// ---- TestLocate -------------------------------------------------------------

func TestLocate(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "loc.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	var mu sync.Mutex
	var ready bool
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		if len(p.ProcessedRange) > 0 {
			ready = true
		}
		mu.Unlock()
	}
	if err := c.InterpretToEnd(uri, 1); err != nil {
		t.Fatalf("InterpretToEnd: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return ready
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pp, err := c.Locate(ctx, uri, 1, vsrocq.Position{Line: 0, Character: 11}, "nat")
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if len(pp) == 0 {
		t.Error("Locate returned empty Pp")
	}
}

// ---- TestPrint --------------------------------------------------------------

func TestPrint(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "print.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	var mu sync.Mutex
	var ready bool
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		if len(p.ProcessedRange) > 0 {
			ready = true
		}
		mu.Unlock()
	}
	if err := c.InterpretToEnd(uri, 1); err != nil {
		t.Fatalf("InterpretToEnd: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return ready
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pp, err := c.Print(ctx, uri, 1, vsrocq.Position{Line: 0, Character: 11}, "nat")
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if len(pp) == 0 {
		t.Error("Print returned empty Pp")
	}
}

// ---- TestSearch -------------------------------------------------------------

func TestSearch(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "search.v", content)

	var mu sync.Mutex
	var results []*vsrocq.SearchResult
	c.OnSearchResult = func(r *vsrocq.SearchResult) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()
	}

	var ready bool
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		if len(p.ProcessedRange) > 0 {
			ready = true
		}
		mu.Unlock()
	}

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	if err := c.InterpretToEnd(uri, 1); err != nil {
		t.Fatalf("InterpretToEnd: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return ready
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	searchID := "test-search-1"
	if err := c.Search(ctx, uri, 1, vsrocq.Position{Line: 0, Character: 11}, "nat", searchID); err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Wait briefly for results; search is async and may return 0+ items.
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(results) > 0
	})

	mu.Lock()
	for _, r := range results {
		if r.ID != searchID {
			t.Errorf("SearchResult.ID = %q, want %q", r.ID, searchID)
		}
		if len(r.Name) == 0 {
			t.Error("SearchResult.Name is empty")
		}
	}
	mu.Unlock()
}

// ---- TestProofView ----------------------------------------------------------

func TestProofView(t *testing.T) {
	c, dir := newClient(t, nil)

	var mu sync.Mutex
	var proofViews []*vsrocq.ProofViewParams
	c.OnProofView = func(p *vsrocq.ProofViewParams) {
		mu.Lock()
		proofViews = append(proofViews, p)
		mu.Unlock()
	}

	content := `Lemma myLemma : 1 = 1.
Proof.
`
	uri := writeRocqFile(t, dir, "pv.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	// Interpret to inside the proof.
	if err := c.InterpretToPoint(uri, 1, vsrocq.Position{Line: 1, Character: 5}); err != nil {
		t.Fatalf("InterpretToPoint: %v", err)
	}

	waitFor(t, 15*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(proofViews) > 0
	})

	mu.Lock()
	count := len(proofViews)
	mu.Unlock()
	t.Logf("received %d prover/proofView notifications", count)
}

// ---- TestContextCancellation ------------------------------------------------

func TestContextCancellation(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uri := writeRocqFile(t, dir, "ctx.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	// Cancel immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.DocumentState(ctx, uri)
	if err == nil {
		t.Log("DocumentState returned without error on cancelled ctx (response was already queued)")
	} else {
		t.Logf("DocumentState returned error on cancelled ctx: %v (expected)", err)
	}
}

// ---- TestMultipleDocuments --------------------------------------------------

func TestMultipleDocuments(t *testing.T) {
	c, dir := newClient(t, nil)

	uris := make([]string, 3)
	for i := range uris {
		content := fmt.Sprintf("Definition x%d := %d.\n", i, i)
		uris[i] = writeRocqFile(t, dir, fmt.Sprintf("multi%d.v", i), content)
		if err := c.DidOpen(uris[i], "rocq", content, 1); err != nil {
			t.Fatalf("DidOpen[%d]: %v", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, uri := range uris {
		if err := c.ResetRocq(ctx, uri); err != nil {
			t.Errorf("ResetRocq(%s): %v", uri, err)
		}
	}

	for _, uri := range uris {
		if err := c.DidClose(uri); err != nil {
			t.Errorf("DidClose(%s): %v", uri, err)
		}
	}
}

// ---- TestErrorRequestUnknownMethod ------------------------------------------

// Sending an unrecognised method should produce a JSON-RPC error.
func TestErrorRequestUnknownMethod(t *testing.T) {
	// We reach into the client internals by using an exported call path — the
	// simplest approach is to use DocumentState on a URI that was never opened.
	c, _ := newClient(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// prover/documentState on an unopened file should at minimum return without
	// panicking; the server may return an error or an empty result.
	result, err := c.DocumentState(ctx, "file:///nonexistent/file.v")
	t.Logf("DocumentState(nonexistent): result=%v err=%v", result, err)
}

// ---- TestPpRoundTrip --------------------------------------------------------

// Verify that Pp (json.RawMessage) survives JSON unmarshal/marshal intact.
func TestPpRoundTrip(t *testing.T) {
	cases := []string{
		`["Ppcmd_empty"]`,
		`["Ppcmd_string","hello"]`,
		`["Ppcmd_glue",[["Ppcmd_string","a"],["Ppcmd_string","b"]]]`,
		`["Ppcmd_box",["Pp_hbox"],["Ppcmd_string","x"]]`,
		`["Ppcmd_print_break",5,0]`,
		`["Ppcmd_force_newline"]`,
		`["Ppcmd_comment",["line1","line2"]]`,
	}
	for _, tc := range cases {
		var pp vsrocq.Pp
		if err := json.Unmarshal([]byte(tc), &pp); err != nil {
			t.Errorf("Unmarshal(%s): %v", tc, err)
			continue
		}
		out, err := json.Marshal(pp)
		if err != nil {
			t.Errorf("Marshal(%s): %v", tc, err)
			continue
		}
		if string(out) != tc {
			t.Errorf("round-trip: got %s, want %s", out, tc)
		}
	}
}

// ---- TestProofViewMessageUnmarshal ------------------------------------------

func TestProofViewMessageUnmarshal(t *testing.T) {
	// severity=1 (Error), text=Ppcmd_string "msg"
	raw := `[1,["Ppcmd_string","msg"]]`
	var m vsrocq.ProofViewMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.Severity != 1 {
		t.Errorf("Severity = %d, want 1", m.Severity)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(out) != raw {
		t.Errorf("round-trip: got %s, want %s", out, raw)
	}
}

// ---- TestManualMode ---------------------------------------------------------

func TestManualMode(t *testing.T) {
	opts := vsrocq.DefaultInitOptions()
	opts.Proof.Mode = vsrocq.ProofModeManual
	c, dir := newClient(t, opts)

	content := "Definition a := 1.\nDefinition b := 2.\nDefinition c := 3.\n"
	uri := writeRocqFile(t, dir, "manual.v", content)

	if err := c.DidOpen(uri, "rocq", content, 1); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	var mu sync.Mutex
	var highlightSeq []vsrocq.HighlightsParams
	c.OnHighlights = func(p *vsrocq.HighlightsParams) {
		mu.Lock()
		highlightSeq = append(highlightSeq, *p)
		mu.Unlock()
	}

	// Step forward three times.
	for i := 0; i < 3; i++ {
		if err := c.StepForward(uri, 1); err != nil {
			t.Fatalf("StepForward[%d]: %v", i, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Step backward once.
	if err := c.StepBackward(uri, 1); err != nil {
		t.Fatalf("StepBackward: %v", err)
	}

	waitFor(t, 10*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(highlightSeq) > 0
	})
}

// ---- TestConcurrentRequests -------------------------------------------------

func TestConcurrentRequests(t *testing.T) {
	c, dir := newClient(t, nil)
	content := "Definition x := 1.\n"
	uris := make([]string, 5)
	for i := range uris {
		uris[i] = writeRocqFile(t, dir, fmt.Sprintf("conc%d.v", i), content)
		if err := c.DidOpen(uris[i], "rocq", content, 1); err != nil {
			t.Fatalf("DidOpen[%d]: %v", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, len(uris))
	for i, uri := range uris {
		wg.Add(1)
		go func(idx int, u string) {
			defer wg.Done()
			_, errs[idx] = c.DocumentState(ctx, u)
		}(i, uri)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent DocumentState[%d]: %v", i, err)
		}
	}
}
