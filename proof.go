package main

// proof.go — proof-checking operations: check, step, query, and result collection from vsrocq.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const notifyTimeout = 10 * time.Second

// doCheck sends interpretToPoint and waits for proofView + diagnostics.
func doCheck(sm *stateManager, file string, line, col int) (*mcp.CallToolResult, any, error) {
	sm.mu.Lock()
	doc, err := sm.getDoc(file)
	if err != nil {
		sm.mu.Unlock()
		return errResult(err), nil, nil
	}
	// Drain channels before sending.
	drainChannels(doc)
	sm.mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
		"position":     map[string]any{"line": line, "character": col},
	}
	if err := sm.client.notify("prover/interpretToPoint", params); err != nil {
		return errResult(err), nil, nil
	}

	return collectResults(doc)
}

// doCheckAll sends interpretToEnd and waits for results.
func doCheckAll(sm *stateManager, file string) (*mcp.CallToolResult, any, error) {
	sm.mu.Lock()
	doc, err := sm.getDoc(file)
	if err != nil {
		sm.mu.Unlock()
		return errResult(err), nil, nil
	}
	drainChannels(doc)
	sm.mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.client.notify("prover/interpretToEnd", params); err != nil {
		return errResult(err), nil, nil
	}

	return collectResults(doc)
}

// doStep sends stepForward or stepBackward and waits for results.
func doStep(sm *stateManager, file string, method string) (*mcp.CallToolResult, any, error) {
	sm.mu.Lock()
	doc, err := sm.getDoc(file)
	if err != nil {
		sm.mu.Unlock()
		return errResult(err), nil, nil
	}
	drainChannels(doc)
	sm.mu.Unlock()

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
	}
	if err := sm.client.notify(method, params); err != nil {
		return errResult(err), nil, nil
	}

	return collectResults(doc)
}

// collectResults waits for proofView and diagnostics notifications.
func collectResults(doc *docState) (*mcp.CallToolResult, any, error) {
	var pv *ProofView
	var diags []Diagnostic

	// Wait for at least one notification, then collect any others that arrive quickly.
	timer := time.NewTimer(notifyTimeout)
	defer timer.Stop()

	gotProofView := false
	gotDiags := false

loop:
	for !gotProofView || !gotDiags {
		select {
		case pv = <-doc.proofViewCh:
			gotProofView = true
		case diags = <-doc.diagnosticCh:
			gotDiags = true
		case <-timer.C:
			// Use whatever we have so far.
			break loop
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

	result := formatDeltaResults(doc.PrevProofView, pv, diags)
	doc.PrevProofView = pv
	if pv != nil {
		doc.ProofView = pv
	}
	if diags != nil {
		doc.Diagnostics = diags
	}
	return result, nil, nil
}

func drainChannels(doc *docState) {
	for {
		select {
		case <-doc.proofViewCh:
		case <-doc.diagnosticCh:
		default:
			return
		}
	}
}

// doQuery sends a query request (about/check/locate/print) and returns the rendered result.
// These are LSP requests that return a Ppcmd tree directly.
func doQuery(sm *stateManager, file string, method string, pattern string) (*mcp.CallToolResult, any, error) {
	sm.mu.Lock()
	doc, err := sm.getDoc(file)
	sm.mu.Unlock()
	if err != nil {
		return errResult(err), nil, nil
	}

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
		"position":     map[string]any{"line": 0, "character": 0},
		"pattern":      pattern,
	}
	result, err := sm.client.request(method, params)
	if err != nil {
		return errResult(err), nil, nil
	}

	text := renderPpcmd(json.RawMessage(result))
	if text == "" {
		text = "No result."
	}
	return textResult(text), nil, nil
}

// doSearch sends a search request and collects results from prover/searchResult notifications.
func doSearch(sm *stateManager, file string, pattern string) (*mcp.CallToolResult, any, error) {
	sm.mu.Lock()
	doc, err := sm.getDoc(file)
	sm.mu.Unlock()
	if err != nil {
		return errResult(err), nil, nil
	}

	// Register a channel to collect search results before sending the request.
	searchID := fmt.Sprintf("search-%d", time.Now().UnixNano())
	resultCh := make(chan searchResult, 256)
	sm.registerSearchHandler(searchID, resultCh)
	defer sm.unregisterSearchHandler(searchID)

	params := map[string]any{
		"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
		"position":     map[string]any{"line": 0, "character": 0},
		"pattern":      pattern,
		"id":           searchID,
	}
	_, err = sm.client.request("prover/search", params)
	if err != nil {
		return errResult(err), nil, nil
	}

	// vsrocqtop sends searchResult notifications after the request response,
	// so we need to wait briefly for them to arrive.
	results := collectSearchResults(resultCh)

	if len(results) == 0 {
		return textResult("No results found."), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "=== Search Results: %d ===\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&sb, "%s : %s\n", r.Name, r.Statement)
	}
	return textResult(sb.String()), nil, nil
}

// collectSearchResults drains search results from the channel with a timeout.
func collectSearchResults(ch <-chan searchResult) []searchResult {
	var results []searchResult
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r := <-ch:
			results = append(results, r)
			// Reset timer after each result — more may follow.
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
