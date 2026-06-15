// Package vsrocq provides a Go client for the vsrocq LSP server.
// Protocol reference: https://github.com/rocq-prover/vsrocq
package vsrocq

import "encoding/json"

// LSP base types

// Position is a zero-based line and character offset.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a start/end pair of positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// TextDocumentIdentifier identifies a document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier adds a version number.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentItem is a document opened via textDocument/didOpen.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// DidOpenTextDocumentParams is the params for textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// TextDocumentContentChangeEvent is a change event for textDocument/didChange.
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidChangeTextDocumentParams is the params for textDocument/didChange.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseTextDocumentParams is the params for textDocument/didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// Diagnostic represents a language diagnostic.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Message  string `json:"message"`
}

// PublishDiagnosticsParams is the params for textDocument/publishDiagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// LSP initialize

// ClientCapabilities is intentionally minimal; vsrocq doesn't require specifics.
type ClientCapabilities struct{}

// InitializeParams is the params for the initialize request.
type InitializeParams struct {
	Capabilities          ClientCapabilities `json:"capabilities"`
	InitializationOptions *InitOptions       `json:"initializationOptions,omitempty"`
}

// InitializeResult is the result of initialize.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities is a partial view of what vsrocq advertises.
type ServerCapabilities struct {
	TextDocumentSync   json.RawMessage `json:"textDocumentSync,omitempty"`
	CompletionProvider json.RawMessage `json:"completionProvider,omitempty"`
}

// vsrocq initialization options

// InitOptions mirrors vsrocq's initializationOptions schema.
type InitOptions struct {
	Proof       ProofOptions       `json:"proof"`
	Goals       GoalsOptions       `json:"goals"`
	Completion  CompletionOptions  `json:"completion"`
	Diagnostics DiagnosticsOptions `json:"diagnostics"`
	Memory      MemoryOptions      `json:"memory"`
	Interrupt   InterruptOptions   `json:"interrupt"`
}

// ProofMode configures the proof checking mode for Rocq.
type ProofMode int

const (
	ProofModeManual     ProofMode = 0 // proofs are checked only on explicit user commands
	ProofModeContinuous ProofMode = 1 // proofs are checked continuously as the document changes
)

// PointInterpretationMode determines the point to which the proof is checked
// when using the "Interpret to point" command.
type PointInterpretationMode int

const (
	PointInterpretationCursor      PointInterpretationMode = 0 // interpret up to the cursor position
	PointInterpretationNextCommand PointInterpretationMode = 1 // interpret up to the next command after the cursor
)

// Delegation strategy constants for ProofOptions.Delegation.
const (
	DelegationNone = "None" // check all proof sentences in the main process
	// Drop proof sentences from the schedule; only the Qed terminator is executed.
	// Because no proof state is established, Qed fails and the proof is
	// auto-admitted via RAdmitted (Declare.Proof.save_admitted).  The Qed failure
	// is reported as a diagnostic but execution continues.
	DelegationSkip     = "Skip"
	DelegationDelegate = "Delegate" // delegate proof sentences to background workers
)

// ProofOptions configures proof checking behaviour.
type ProofOptions struct {
	// delegation strategy used by the server (None/Skip/Delegate)
	Delegation string `json:"delegation"`
	// number of workers assigned to proofs in delegation mode
	Workers *int `json:"workers"`
	// Manual or Continuous checking
	Mode ProofMode `json:"mode"`
	// halt execution after the first error
	Block bool `json:"block"`
	// Cursor or NextCommand interpretation
	PointInterpretationMode PointInterpretationMode `json:"pointInterpretationMode"`
}

// GoalsOptions configures goal display.
type GoalsOptions struct {
	Messages MessagesOptions `json:"messages"`
	// "Pp" or "String"; "String" returns plaintext via pp_proof/pp_messages
	PPMode string `json:"ppmode,omitempty"`
}

// MessagesOptions controls what appears in proofview messages.
type MessagesOptions struct {
	Full bool `json:"full"` // include warnings and errors in proofview messages
}

// CompletionOptions configures completion.
type CompletionOptions struct {
	// enable completion support from the vsrocq language server
	Enable bool `json:"enable"`
	// ranking algorithm: 0=SplitTypeIntersection, 1=StructuredSplitUnification
	Algorithm int `json:"algorithm"`
	// max theorems for unification during completion; higher improves results but slows completion
	UnificationLimit int     `json:"unificationLimit"`
	AtomicFactor     float64 `json:"atomicFactor"`
	SizeFactor       float64 `json:"sizeFactor"`
}

// DiagnosticsOptions configures diagnostics.
type DiagnosticsOptions struct {
	Enable bool `json:"enable"`
	Full   bool `json:"full"` // include info-level entries in diagnostics
}

// MemoryOptions configures memory limits.
type MemoryOptions struct {
	Limit int `json:"limit"` // GB threshold above which execution state is discarded for saved documents
}

// InterruptOptions configures interruption behaviour.
type InterruptOptions struct {
	Preempt bool `json:"preempt"` // hovering and other queries preempt checking tasks
}

// DefaultInitOptions returns safe defaults for vsrocq.
func DefaultInitOptions() *InitOptions {
	return &InitOptions{
		Proof: ProofOptions{
			Delegation:              DelegationNone,
			Mode:                    ProofModeManual,
			Block:                   false,
			PointInterpretationMode: PointInterpretationNextCommand,
		},
		Goals: GoalsOptions{
			Messages: MessagesOptions{Full: false},
			PPMode:   "String",
		},
		Completion:  CompletionOptions{Enable: false},
		Diagnostics: DiagnosticsOptions{Enable: true, Full: false},
		Memory:      MemoryOptions{Limit: 4},
		Interrupt:   InterruptOptions{Preempt: false},
	}
}

// vsrocq custom pp type

// Pp is a structured pretty-print command (Rocq Ppcmd).
// It is represented as a JSON tagged union: ["Ppcmd_string", "text"] etc.
type Pp = json.RawMessage

// vsrocq server notifications

// HighlightsParams is the payload of prover/updateHighlights.
type HighlightsParams struct {
	URI             string  `json:"uri"`
	PreparedRange   []Range `json:"preparedRange"`   // scheduled for checking
	ProcessingRange []Range `json:"processingRange"` // currently being checked
	ProcessedRange  []Range `json:"processedRange"`  // already checked (verified)
}

// MoveCursorParams is the payload of prover/moveCursor.
type MoveCursorParams struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// BlockOnErrorParams is the payload of prover/blockOnError.
type BlockOnErrorParams struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Goal is a single proof goal with hypotheses (Pp format).
type Goal struct {
	ID         int     `json:"id"`
	Name       *string `json:"name,omitempty"`
	Hypotheses []Pp    `json:"hypotheses"`
	Goal       Pp      `json:"goal"`
}

// ProofState groups goals by category.
type ProofState struct {
	Goals          []Goal `json:"goals"`
	ShelvedGoals   []Goal `json:"shelvedGoals"`
	GivenUpGoals   []Goal `json:"givenUpGoals"`
	UnfocusedGoals []Goal `json:"unfocusedGoals"`
}

// StringGoal is a single proof goal in String ppmode — all text fields are
// plain strings rather than Ppcmd trees.
type StringGoal struct {
	ID         int      `json:"id"`
	Name       *string  `json:"name"`
	Hypotheses []string `json:"hypotheses"`
	Goal       string   `json:"goal"`
}

// StringProofState is the pp_proof payload when ppmode is "String".
type StringProofState struct {
	Goals          []StringGoal `json:"goals"`
	ShelvedGoals   []StringGoal `json:"shelvedGoals"`
	GivenUpGoals   []StringGoal `json:"givenUpGoals"`
	UnfocusedGoals []StringGoal `json:"unfocusedGoals"`
}

// ProofViewMessage is a (severity, pp) pair in a ProofView notification.
// On the wire it serialises as a 2-element JSON array.
type ProofViewMessage struct {
	Severity int // 1=Error 2=Warning 3=Info 4=Hint
	Text     Pp
}

func (m *ProofViewMessage) UnmarshalJSON(b []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var sev int
	if err := json.Unmarshal(raw[0], &sev); err != nil {
		return err
	}
	m.Severity = sev
	m.Text = raw[1]
	return nil
}

func (m ProofViewMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]any{m.Severity, m.Text})
}

// ProofViewParams is the payload of prover/proofView.
type ProofViewParams struct {
	Range      Range              `json:"range"`
	Proof      *ProofState        `json:"proof"`
	Messages   []ProofViewMessage `json:"messages"`
	PPProof    *StringProofState  `json:"pp_proof,omitempty"`
	PPMessages []json.RawMessage  `json:"pp_messages,omitempty"`
}

// SearchResult is a single result from prover/searchResult.
type SearchResult struct {
	ID        string `json:"id"`
	Name      Pp     `json:"name"`
	Statement Pp     `json:"statement"`
}

// LogMessageParams is the payload of prover/debugMessage.
type LogMessageParams struct {
	Message string `json:"message"`
}

// vsrocq custom request params / results

// InterpretToPointParams is the params for prover/interpretToPoint (notification).
// Interprets the current Rocq file up to the given point.
type InterpretToPointParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Position     Position                        `json:"position"`
}

// InterpretToEndParams is the params for prover/interpretToEnd (notification).
// Interprets the current Rocq file until the end.
type InterpretToEndParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
}

// StepForwardParams is the params for prover/stepForward (notification).
type StepForwardParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
}

// StepBackwardParams is the params for prover/stepBackward (notification).
type StepBackwardParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
}

// InterruptParams is the params for prover/interrupt (notification).
type InterruptParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
}

// ResetParams is the params for prover/resetRocq (request).
type ResetParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SearchParams is the params for prover/search (request).
// Searches for the term pattern at the cursor.
type SearchParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Position     Position                        `json:"position"`
	Pattern      string                          `json:"pattern"`
	ID           string                          `json:"id"`
}

// AboutParams is the params for prover/about (request).
// Returns information about the term pattern at the cursor.
type AboutParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Position     Position                        `json:"position"`
	Pattern      string                          `json:"pattern"`
}

// CheckParams is the params for prover/check (request).
// Checks the type of the term at the cursor.
type CheckParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Position     Position                        `json:"position"`
	Pattern      string                          `json:"pattern"`
}

// LocateParams is the params for prover/locate (request).
// Locates the term at the cursor.
type LocateParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Position     Position                        `json:"position"`
	Pattern      string                          `json:"pattern"`
}

// PrintParams is the params for prover/print (request).
// Prints the definition of the term at the cursor.
type PrintParams struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Position     Position                        `json:"position"`
	Pattern      string                          `json:"pattern"`
}

// DocumentStateParams is the params for prover/documentState (request).
type DocumentStateParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DocumentStateResult is the result of prover/documentState.
type DocumentStateResult struct {
	Document string `json:"document"`
}

// DocumentProofsParams is the params for prover/documentProofs (request).
type DocumentProofsParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// ProofStatement describes the statement of a proof.
type ProofStatement struct {
	Statement string `json:"statement"`
	Range     Range  `json:"range"`
}

// ProofStep describes one tactic step.
type ProofStep struct {
	Tactic string `json:"tactic"`
	Range  Range  `json:"range"`
}

// ProofBlock groups a statement with its steps.
type ProofBlock struct {
	Statement ProofStatement `json:"statement"`
	Range     Range          `json:"range"`
	Steps     []ProofStep    `json:"steps"`
}

// DocumentProofsResult is the result of prover/documentProofs.
type DocumentProofsResult struct {
	Proofs []ProofBlock `json:"proofs"`
}
