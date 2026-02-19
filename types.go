package main

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
