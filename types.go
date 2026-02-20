package main

// types.go — shared domain types for proof goals, diagnostics, and LSP positions.

// Goal represents a single focused goal with its ID and pre-rendered text.
type Goal struct {
	ID   string
	Text string // pre-rendered: hypotheses + separator + conclusion
}

// ProofView stores all focused goals as pre-rendered text, plus metadata.
type ProofView struct {
	UnfocusedCount int      // total number of unfocused goals (in focus blocks, shelved, etc.)
	Goals          []Goal   // all focused goals, pre-rendered
	Messages       []string // prover messages
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

// searchResult is a single result from prover/searchResult notifications.
type searchResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Statement string `json:"statement"`
}

// ProofBlock represents a proof in the document, as returned by prover/documentProofs.
type ProofBlock struct {
	Statement ProofStatement `json:"statement"`
	Range     Range          `json:"range"`
	Steps     []ProofStep    `json:"steps"`
}

type ProofStatement struct {
	Statement string `json:"statement"`
	Range     Range  `json:"range"`
}

type ProofStep struct {
	Tactic string `json:"tactic"`
	Range  Range  `json:"range"`
}
