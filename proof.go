package main

import (
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
