package main

// types.go — shared domain types for proof goals, diagnostics, and LSP positions.

// ProofView stores the focused goal as pre-rendered text, plus metadata.
// Only the first focused goal (Goals[0]) is kept; GoalCount tracks the total.
type ProofView struct {
	GoalCount      int      // total number of focused goals
	UnfocusedCount int      // total number of unfocused goals (in focus blocks, shelved, etc.)
	GoalID         string   // ID of the focused goal
	GoalText       string   // pre-rendered: hypotheses + separator + conclusion
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
