package rocq

// proof.go — proof-checking operations: check, step, query, and result collection from vsrocq.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const NotifyTimeout = 10 * time.Second

// DoCheck sends interpretToPoint and waits for proofView + diagnostics.
func DoCheck(sm *StateManager, file string, line, col int) (*mcp.CallToolResult, any, error) {
	sm.Mu.Lock()
	doc, err := sm.GetDoc(file)
	if err != nil {
		sm.Mu.Unlock()
		return ErrResult(err), nil, nil
	}
	// Drain channels before sending.
	DrainChannels(doc)
	sm.Mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
		"position":     map[string]any{"line": line, "character": col},
	}
	if err := sm.Client.Notify("prover/interpretToPoint", params); err != nil {
		return ErrResult(err), nil, nil
	}

	return collectResultsFull(doc)
}

// DoCheckAll sends interpretToEnd and waits for results.
func DoCheckAll(sm *StateManager, file string) (*mcp.CallToolResult, any, error) {
	sm.Mu.Lock()
	doc, err := sm.GetDoc(file)
	if err != nil {
		sm.Mu.Unlock()
		return ErrResult(err), nil, nil
	}
	DrainChannels(doc)
	sm.Mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.Client.Notify("prover/interpretToEnd", params); err != nil {
		return ErrResult(err), nil, nil
	}

	return collectResultsFull(doc)
}

// DoStep sends stepForward or stepBackward and waits for results.
func DoStep(sm *StateManager, file string, method string) (*mcp.CallToolResult, any, error) {
	sm.Mu.Lock()
	doc, err := sm.GetDoc(file)
	if err != nil {
		sm.Mu.Unlock()
		return ErrResult(err), nil, nil
	}
	DrainChannels(doc)
	sm.Mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.Client.Notify(method, params); err != nil {
		return ErrResult(err), nil, nil
	}

	return collectResultsDelta(doc)
}

// WaitNotifications waits for proofView and diagnostics notifications from vsrocq.
func WaitNotifications(doc *DocState) (*ProofView, []Diagnostic) {
	var pv *ProofView
	var diags []Diagnostic

	timer := time.NewTimer(NotifyTimeout)
	defer timer.Stop()

	gotProofView := false
	gotDiags := false

	for !gotProofView || !gotDiags {
		select {
		case pv = <-doc.ProofViewCh:
			gotProofView = true
		case diags = <-doc.DiagnosticCh:
			gotDiags = true
		case <-timer.C:
			return pv, diags
		}
		// After getting the first notification, give a short window for the second.
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(500 * time.Millisecond)
	}
	return pv, diags
}

// collectResultsFull waits for notifications and formats with full context (no diffs).
func collectResultsFull(doc *DocState) (*mcp.CallToolResult, any, error) {
	pv, diags := WaitNotifications(doc)
	result := FormatFullResults(pv, diags)
	doc.PrevProofView = pv
	if pv != nil {
		doc.ProofView = pv
	}
	if diags != nil {
		doc.Diagnostics = diags
	}
	return result, nil, nil
}

// collectResultsDelta waits for notifications and formats as delta against previous state.
func collectResultsDelta(doc *DocState) (*mcp.CallToolResult, any, error) {
	pv, diags := WaitNotifications(doc)
	result := FormatDeltaResults(doc.PrevProofView, pv, diags)
	doc.PrevProofView = pv
	if pv != nil {
		doc.ProofView = pv
	}
	if diags != nil {
		doc.Diagnostics = diags
	}
	return result, nil, nil
}

// DrainChannels drains all pending notifications from a document's channels.
func DrainChannels(doc *DocState) {
	for {
		select {
		case <-doc.ProofViewCh:
		case <-doc.DiagnosticCh:
		case <-doc.CursorCh:
		default:
			return
		}
	}
}

// DoQuery sends a query request (about/check/locate/print) and returns the rendered result.
func DoQuery(sm *StateManager, file string, method string, pattern string) (*mcp.CallToolResult, any, error) {
	sm.Mu.Lock()
	doc, err := sm.GetDoc(file)
	sm.Mu.Unlock()
	if err != nil {
		return ErrResult(err), nil, nil
	}

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
		"position":     map[string]any{"line": 0, "character": 0},
		"pattern":      pattern,
	}
	result, err := sm.Client.Request(method, params)
	if err != nil {
		return ErrResult(err), nil, nil
	}

	text := RenderPpcmd(json.RawMessage(result))
	if text == "" {
		text = "No result."
	}
	return TextResult(text), nil, nil
}

// DoSearch sends a search request and collects results from prover/searchResult notifications.
func DoSearch(sm *StateManager, file string, pattern string) (*mcp.CallToolResult, any, error) {
	sm.Mu.Lock()
	doc, err := sm.GetDoc(file)
	sm.Mu.Unlock()
	if err != nil {
		return ErrResult(err), nil, nil
	}

	// Register a channel to collect search results before sending the request.
	searchID := fmt.Sprintf("search-%d", time.Now().UnixNano())
	resultCh := make(chan SearchResult, 256)
	sm.RegisterSearchHandler(searchID, resultCh)
	defer sm.UnregisterSearchHandler(searchID)

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
		"position":     map[string]any{"line": 0, "character": 0},
		"pattern":      pattern,
		"id":           searchID,
	}
	_, err = sm.Client.Request("prover/search", params)
	if err != nil {
		return ErrResult(err), nil, nil
	}

	results := CollectSearchResults(resultCh)

	if len(results) == 0 {
		return TextResult("No results found."), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "=== Search Results: %d ===\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&sb, "%s : %s\n", r.Name, r.Statement)
	}
	return TextResult(sb.String()), nil, nil
}

// DoReset sends prover/resetRocq to reset the prover state for a document.
func DoReset(sm *StateManager, file string) (*mcp.CallToolResult, any, error) {
	sm.Mu.Lock()
	doc, err := sm.GetDoc(file)
	if err != nil {
		sm.Mu.Unlock()
		return ErrResult(err), nil, nil
	}
	DrainChannels(doc)
	sm.Mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI},
	}
	_, err = sm.Client.Request("prover/resetRocq", params)
	if err != nil {
		return ErrResult(err), nil, nil
	}

	// Clear cached proof state — it's no longer valid after reset.
	sm.Mu.Lock()
	doc.ProofView = nil
	doc.PrevProofView = nil
	doc.Diagnostics = nil
	sm.Mu.Unlock()

	return TextResult("Reset " + file), nil, nil
}

// DoDocumentProofs sends prover/documentProofs and returns the proof structure.
func DoDocumentProofs(sm *StateManager, file string) (*mcp.CallToolResult, any, error) {
	sm.Mu.Lock()
	doc, err := sm.GetDoc(file)
	sm.Mu.Unlock()
	if err != nil {
		return ErrResult(err), nil, nil
	}

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI},
	}
	result, err := sm.Client.Request("prover/documentProofs", params)
	if err != nil {
		return ErrResult(fmt.Errorf("parse documentProofs: %w", err)), nil, nil
	}

	var resp struct {
		Proofs []ProofBlock `json:"proofs"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return ErrResult(fmt.Errorf("parse documentProofs: %w", err)), nil, nil
	}

	if len(resp.Proofs) == 0 {
		return TextResult("No proofs found in " + file), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "=== Proofs: %d ===\n", len(resp.Proofs))
	for i, p := range resp.Proofs {
		fmt.Fprintf(&sb, "\n--- Proof %d (lines %d–%d) ---\n",
			i+1, p.Range.Start.Line+1, p.Range.End.Line+1)
		fmt.Fprintf(&sb, "Statement: %s\n", p.Statement.Statement)
		if len(p.Steps) > 0 {
			fmt.Fprintf(&sb, "Steps:\n")
			for _, s := range p.Steps {
				fmt.Fprintf(&sb, "  L%d: %s\n", s.Range.Start.Line+1, s.Tactic)
			}
		}
	}
	return TextResult(sb.String()), nil, nil
}

// CollectSearchResults drains search results from the channel with a timeout.
func CollectSearchResults(ch <-chan SearchResult) []SearchResult {
	var results []SearchResult
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r := <-ch:
			results = append(results, r)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(200 * time.Millisecond)
		case <-timer.C:
			return results
		}
	}
}
