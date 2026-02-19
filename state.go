package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// ProofGoal represents a single goal in a proof view.
type ProofGoal struct {
	ID         string   `json:"id"`
	Goal       string   `json:"goal"`
	Hypotheses []string `json:"hypotheses"`
}

// ProofView is the structured proof state from vsrocq.
type ProofView struct {
	Goals    []ProofGoal `json:"goals"`
	Messages []string    `json:"messages"`
}

// Diagnostic is an LSP diagnostic.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// docState tracks per-document state.
type docState struct {
	URI           string
	Version       int
	Content       string
	Diagnostics   []Diagnostic
	ProofView     *ProofView
	PrevProofView *ProofView // previous proof view for delta computation

	// Channels for bridging async notifications to sync tool calls.
	proofViewCh  chan *ProofView
	diagnosticCh chan []Diagnostic
}

// stateManager manages per-document state and the vsrocq client.
type stateManager struct {
	client *vsrocqClient
	docs   map[string]*docState // keyed by URI
	mu     sync.Mutex
	args   []string // extra args for vsrocqtop
}

func newStateManager(args []string) *stateManager {
	return &stateManager{
		docs: make(map[string]*docState),
		args: args,
	}
}

// ensureClient lazily starts vsrocqtop.
func (sm *stateManager) ensureClient() error {
	if sm.client != nil {
		return nil
	}
	client, err := newVsrocqClient(sm.args)
	if err != nil {
		return err
	}
	sm.client = client

	// Register notification handlers.
	client.onNotification("textDocument/publishDiagnostics", sm.handleDiagnostics)
	client.onNotification("prover/proofView", sm.handleProofView)
	client.onNotification("prover/updateHighlights", func(params json.RawMessage) {})
	client.onNotification("prover/moveCursor", func(params json.RawMessage) {})
	client.onNotification("prover/blockOnError", func(params json.RawMessage) {})
	client.onNotification("prover/debugMessage", func(params json.RawMessage) {
		log.Printf("vsrocq debug: %s", string(params))
	})

	// Initialize with current working directory.
	cwd, _ := os.Getwd()
	rootURI := "file://" + cwd
	if err := client.initialize(rootURI); err != nil {
		return err
	}

	return nil
}

func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "file://" + abs
}

// openDoc opens a .v file in vsrocq.
func (sm *stateManager) openDoc(path string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if err := sm.ensureClient(); err != nil {
		return err
	}

	uri := fileURI(path)
	if _, exists := sm.docs[uri]; exists {
		return fmt.Errorf("document already open: %s", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	doc := &docState{
		URI:          uri,
		Version:      1,
		Content:      string(content),
		proofViewCh:  make(chan *ProofView, 16),
		diagnosticCh: make(chan []Diagnostic, 16),
	}
	sm.docs[uri] = doc

	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "rocq",
			"version":    doc.Version,
			"text":       doc.Content,
		},
	}
	return sm.client.notify("textDocument/didOpen", params)
}

// closeDoc closes a document in vsrocq.
func (sm *stateManager) closeDoc(path string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	uri := fileURI(path)
	doc, ok := sm.docs[uri]
	if !ok {
		return fmt.Errorf("document not open: %s", path)
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri": doc.URI,
		},
	}
	err := sm.client.notify("textDocument/didClose", params)
	delete(sm.docs, uri)
	return err
}

// syncDoc re-reads a file from disk and sends didChange.
func (sm *stateManager) syncDoc(path string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	uri := fileURI(path)
	doc, ok := sm.docs[uri]
	if !ok {
		return fmt.Errorf("document not open: %s", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	doc.Version++
	doc.Content = string(content)

	params := map[string]any{
		"textDocument": map[string]any{
			"uri":     doc.URI,
			"version": doc.Version,
		},
		"contentChanges": []map[string]any{
			{"text": doc.Content},
		},
	}
	return sm.client.notify("textDocument/didChange", params)
}

// getDoc returns the state for a file (caller must hold lock or accept races).
func (sm *stateManager) getDoc(path string) (*docState, error) {
	uri := fileURI(path)
	doc, ok := sm.docs[uri]
	if !ok {
		return nil, fmt.Errorf("document not open: %s", path)
	}
	return doc, nil
}

// handleDiagnostics processes publishDiagnostics notifications.
func (sm *stateManager) handleDiagnostics(params json.RawMessage) {
	var p struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		log.Printf("parse diagnostics: %v", err)
		return
	}

	sm.mu.Lock()
	doc, ok := sm.docs[p.URI]
	if ok {
		doc.Diagnostics = p.Diagnostics
	}
	sm.mu.Unlock()

	if ok {
		// Non-blocking send to channel.
		select {
		case doc.diagnosticCh <- p.Diagnostics:
		default:
		}
	}
}

// handleProofView processes prover/proofView notifications.
func (sm *stateManager) handleProofView(params json.RawMessage) {
	pv := parseProofView(params)
	if pv == nil {
		log.Printf("failed to parse proofView")
		return
	}

	// proofView doesn't include URI directly — deliver to all docs with waiting channels.
	// In practice, there's typically only one active proof at a time.
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, doc := range sm.docs {
		select {
		case doc.proofViewCh <- pv:
		default:
		}
	}
}

// shutdown cleans up the vsrocq client.
func (sm *stateManager) shutdown() error {
	if sm.client == nil {
		return nil
	}
	return sm.client.shutdown()
}
