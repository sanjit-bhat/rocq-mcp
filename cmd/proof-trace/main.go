package main

// proof-trace steps through every sentence in a .v file and prints the full
// proof state returned by vsrocqtop at each step. For debugging.

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/sanjit/rocq-mcp/internal/rocq"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: proof-trace <file.v> [-- vsrocqtop flags...]\n")
		os.Exit(1)
	}

	file := os.Args[1]
	var vsrocqArgs []string
	for i, arg := range os.Args[2:] {
		if arg == "--" {
			vsrocqArgs = os.Args[i+3:]
			break
		}
	}

	sm := rocq.NewStateManager(vsrocqArgs)
	defer sm.Shutdown()

	if err := sm.OpenDoc(file); err != nil {
		log.Fatalf("open: %v", err)
	}
	defer sm.CloseDoc(file)

	sm.Mu.Lock()
	doc, err := sm.GetDoc(file)
	sm.Mu.Unlock()
	if err != nil {
		log.Fatalf("getDoc: %v", err)
	}

	content := doc.Content
	prevOffset := 0
	step := 0

	for {
		rocq.DrainChannels(doc)

		params := map[string]any{
			"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version},
		}
		if err := sm.Client.Notify("prover/stepForward", params); err != nil {
			log.Fatalf("stepForward: %v", err)
		}

		// Wait for moveCursor, proofView, and diagnostics.
		var cursorPos *rocq.Position
		var pv *rocq.ProofView
		var diags []rocq.Diagnostic

		timer := time.NewTimer(5 * time.Second)
		gotCursor := false
		gotProofView := false
		gotDiags := false

		for !gotCursor || !gotProofView || !gotDiags {
			select {
			case pos := <-doc.CursorCh:
				cursorPos = &pos
				gotCursor = true
			case p := <-doc.ProofViewCh:
				pv = p
				gotProofView = true
			case d := <-doc.DiagnosticCh:
				diags = d
				gotDiags = true
			case <-timer.C:
				goto done
			}
			// After first notification, shorten timeout for the rest.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(500 * time.Millisecond)
		}
	done:
		timer.Stop()

		// No cursor movement means vsrocqtop didn't step — we're at the end.
		if cursorPos == nil {
			break
		}

		step++

		// Extract sentence text from file content.
		newOffset := positionToOffset(content, *cursorPos)
		sentence := ""
		if newOffset > prevOffset {
			sentence = strings.TrimSpace(content[prevOffset:newOffset])
		}
		prevOffset = newOffset

		// Print step header.
		fmt.Printf("=== Step %d ===\n", step)
		if sentence != "" {
			fmt.Printf("> %s\n", sentence)
		}
		fmt.Println()

		// Print focused goals.
		if pv != nil && len(pv.Goals) > 0 {
			fmt.Printf("Focused Goals (%d):\n", len(pv.Goals))
			for i, g := range pv.Goals {
				if len(pv.Goals) > 1 {
					fmt.Printf("Goal %d:\n", i+1)
				}
				fmt.Print(g.Text)
			}
		} else if pv != nil {
			fmt.Println("Focused Goals (0)")
		}

		// Print unfocused counts.
		if pv != nil {
			fmt.Printf("Unfocused: %d\n", pv.UnfocusedCount)
		}

		// Print messages.
		if pv != nil && len(pv.Messages) > 0 {
			fmt.Printf("\nMessages (%d):\n", len(pv.Messages))
			for _, m := range pv.Messages {
				fmt.Printf("  %s\n", m)
			}
		}

		// Print diagnostics.
		if len(diags) > 0 {
			fmt.Printf("\nDiagnostics (%d):\n", len(diags))
			for _, d := range diags {
				severity := "info"
				switch d.Severity {
				case 1:
					severity = "error"
				case 2:
					severity = "warning"
				case 3:
					severity = "info"
				case 4:
					severity = "hint"
				}
				fmt.Printf("  [%s] line %d:%d–%d:%d: %s\n",
					severity,
					d.Range.Start.Line+1, d.Range.Start.Character,
					d.Range.End.Line+1, d.Range.End.Character,
					d.Message)
			}
		}

		fmt.Println()
	}

	fmt.Printf("--- Done: %d steps ---\n", step)
}

// positionToOffset converts an LSP Position (line, character) to a byte offset in content.
func positionToOffset(content string, pos rocq.Position) int {
	line := 0
	for i, ch := range content {
		if line == pos.Line {
			return i + pos.Character
		}
		if ch == '\n' {
			line++
		}
	}
	return len(content)
}
