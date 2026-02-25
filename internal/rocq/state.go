package rocq

// state.go — per-document state tracking and vsrocq notification dispatch.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// DocState tracks per-document state.
type DocState struct {
	URI           string
	Version       int
	Content       string
	Diagnostics   []Diagnostic
	ProofView     *ProofView
	PrevProofView *ProofView // previous proof view for delta computation

	// Channels for bridging async notifications to sync tool calls.
	ProofViewCh  chan *ProofView
	DiagnosticCh chan []Diagnostic
	CursorCh     chan Position
}

// StateManager manages per-document state and the vsrocq client.
type StateManager struct {
	Client *VsrocqClient
	Docs   map[string]*DocState // keyed by URI
	Mu     sync.Mutex
	args   []string // extra args for vsrocqtop

	// Search result channels, keyed by search ID.
	searchHandlers   map[string]chan SearchResult
	searchHandlersMu sync.Mutex
}

func NewStateManager(args []string) *StateManager {
	return &StateManager{
		Docs:           make(map[string]*DocState),
		args:           args,
		searchHandlers: make(map[string]chan SearchResult),
	}
}

// ensureClient lazily starts vsrocqtop.
func (sm *StateManager) ensureClient() error {
	if sm.Client != nil {
		return nil
	}
	client, err := newVsrocqClient(sm.args)
	if err != nil {
		return err
	}
	sm.Client = client

	// Register notification handlers.
	client.onNotification("textDocument/publishDiagnostics", sm.handleDiagnostics)
	client.onNotification("prover/proofView", sm.handleProofView)
	client.onNotification("prover/searchResult", sm.handleSearchResult)
	client.onNotification("prover/updateHighlights", func(params json.RawMessage) {})
	client.onNotification("prover/moveCursor", sm.handleMoveCursor)
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

func FileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "file://" + abs
}

// OpenDoc opens a .v file in vsrocq.
func (sm *StateManager) OpenDoc(path string) error {
	sm.Mu.Lock()
	defer sm.Mu.Unlock()

	if err := sm.ensureClient(); err != nil {
		return err
	}

	uri := FileURI(path)
	if _, exists := sm.Docs[uri]; exists {
		return fmt.Errorf("document already open: %s", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	doc := &DocState{
		URI:           uri,
		Version:       1,
		Content:       string(content),
		PrevProofView: &ProofView{}, // zero-value so FormatDeltaResults always has non-nil prev
		ProofViewCh:   make(chan *ProofView, 16),
		DiagnosticCh:  make(chan []Diagnostic, 16),
		CursorCh:      make(chan Position, 16),
	}
	sm.Docs[uri] = doc

	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "rocq",
			"version":    doc.Version,
			"text":       doc.Content,
		},
	}
	return sm.Client.Notify("textDocument/didOpen", params)
}

// CloseDoc closes a document in vsrocq.
func (sm *StateManager) CloseDoc(path string) error {
	sm.Mu.Lock()
	defer sm.Mu.Unlock()

	uri := FileURI(path)
	doc, ok := sm.Docs[uri]
	if !ok {
		return fmt.Errorf("document not open: %s", path)
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri": doc.URI,
		},
	}
	err := sm.Client.Notify("textDocument/didClose", params)
	delete(sm.Docs, uri)
	return err
}

// SyncDoc re-reads a file from disk and sends didChange.
func (sm *StateManager) SyncDoc(path string) error {
	sm.Mu.Lock()
	defer sm.Mu.Unlock()

	uri := FileURI(path)
	doc, ok := sm.Docs[uri]
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
	return sm.Client.Notify("textDocument/didChange", params)
}

// GetDoc returns the state for a file (caller must hold lock or accept races).
func (sm *StateManager) GetDoc(path string) (*DocState, error) {
	uri := FileURI(path)
	doc, ok := sm.Docs[uri]
	if !ok {
		return nil, fmt.Errorf("document not open: %s", path)
	}
	return doc, nil
}

// handleDiagnostics processes publishDiagnostics notifications.
func (sm *StateManager) handleDiagnostics(params json.RawMessage) {
	var p struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		log.Printf("parse diagnostics: %v", err)
		return
	}

	sm.Mu.Lock()
	doc, ok := sm.Docs[p.URI]
	if ok {
		doc.Diagnostics = p.Diagnostics
	}
	sm.Mu.Unlock()

	if ok {
		// Non-blocking send to channel.
		select {
		case doc.DiagnosticCh <- p.Diagnostics:
		default:
		}
	}
}

// handleProofView processes prover/proofView notifications.
func (sm *StateManager) handleProofView(params json.RawMessage) {
	pv := ParseProofView(params)
	if pv == nil {
		log.Printf("failed to parse proofView")
		return
	}

	// proofView doesn't include URI directly — deliver to all docs with waiting channels.
	// In practice, there's typically only one active proof at a time.
	sm.Mu.Lock()
	defer sm.Mu.Unlock()
	for _, doc := range sm.Docs {
		select {
		case doc.ProofViewCh <- pv:
		default:
		}
	}
}

// handleMoveCursor processes prover/moveCursor notifications.
func (sm *StateManager) handleMoveCursor(params json.RawMessage) {
	var p struct {
		URI   string `json:"uri"`
		Range Range  `json:"range"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		log.Printf("parse moveCursor: %v", err)
		return
	}

	pos := p.Range.End
	sm.Mu.Lock()
	defer sm.Mu.Unlock()

	if p.URI != "" {
		if doc, ok := sm.Docs[p.URI]; ok {
			select {
			case doc.CursorCh <- pos:
			default:
			}
		}
		return
	}

	// No URI — broadcast to all docs (like proofView).
	for _, doc := range sm.Docs {
		select {
		case doc.CursorCh <- pos:
		default:
		}
	}
}

// RegisterSearchHandler registers a channel to receive search results for a given ID.
func (sm *StateManager) RegisterSearchHandler(id string, ch chan SearchResult) {
	sm.searchHandlersMu.Lock()
	defer sm.searchHandlersMu.Unlock()
	sm.searchHandlers[id] = ch
}

// UnregisterSearchHandler removes a search result channel.
func (sm *StateManager) UnregisterSearchHandler(id string) {
	sm.searchHandlersMu.Lock()
	defer sm.searchHandlersMu.Unlock()
	delete(sm.searchHandlers, id)
}

// handleSearchResult processes prover/searchResult notifications.
func (sm *StateManager) handleSearchResult(params json.RawMessage) {
	var raw struct {
		ID        string          `json:"id"`
		Name      json.RawMessage `json:"name"`
		Statement json.RawMessage `json:"statement"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		log.Printf("parse searchResult: %v", err)
		return
	}

	result := SearchResult{
		ID:        raw.ID,
		Name:      RenderPpcmd(raw.Name),
		Statement: RenderPpcmd(raw.Statement),
	}

	sm.searchHandlersMu.Lock()
	ch, ok := sm.searchHandlers[raw.ID]
	sm.searchHandlersMu.Unlock()

	if ok {
		select {
		case ch <- result:
		default:
		}
	}
}

// Shutdown cleans up the vsrocq client.
func (sm *StateManager) Shutdown() error {
	if sm.Client == nil {
		return nil
	}
	return sm.Client.shutdown()
}
