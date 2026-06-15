// Package mcp wraps the vsrocq client as an MCP server exposing three tools:
// check_to_end, check, and close_file.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sanjit-bhat/rocq-mcp/vsrocq"
)

const stableTimeout = 200 * time.Millisecond

// CheckError is a single proof-checking error.
type CheckError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// CheckResult is returned by the check_to_end and check tools.
type CheckResult struct {
	CheckedTo  int          `json:"checked_to"`
	Errors     []CheckError `json:"errors"`
	ProofGoals string       `json:"proof_goals"` // plaintext proof goals (empty when no open proof)
}

type fileState struct {
	version int
	content string
}

// Server is the Rocq MCP server state.
type Server struct {
	vsrocqBin string

	mu     sync.Mutex
	client *vsrocq.Client
	files  map[string]*fileState
}

// New creates a Rocq MCP server. Call Start to launch vsrocq eagerly, or let
// the first tool call launch it lazily via ensureStarted.
func New(vsrocqBin string) *Server {
	return &Server{
		vsrocqBin: vsrocqBin,
		files:     make(map[string]*fileState),
	}
}

// Start launches vsrocq with default options. Idempotent if already running.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureStarted(ctx)
}

// Shutdown stops vsrocq and clears all file state.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopClient(ctx)
}

// MCPServer returns an sdk.Server with the three Rocq tools registered.
func (s *Server) MCPServer() *sdk.Server {
	srv := sdk.NewServer("rocq-mcp", "0.1.0", nil)
	srv.AddTools(
		sdk.NewServerTool("check_to_end", "Check a Rocq (.v) file to the end", s.handleCheckToEnd,
			sdk.Input(
				sdk.Property("path", sdk.Description("path to the .v file")),
			),
		),
		sdk.NewServerTool("check", "Check a Rocq (.v) file up to a given line", s.handleCheck,
			sdk.Input(
				sdk.Property("path", sdk.Description("path to the .v file")),
				sdk.Property("to_line", sdk.Description("check up to this 0-based line (inclusive)")),
				sdk.Property("omit", sdk.Description("if set, skip proof bodies before this 0-based line using delegation")),
			),
		),
		sdk.NewServerTool("close_file", "Free vsrocq incremental-checking cache for a file", s.handleClose,
			sdk.Input(
				sdk.Property("path", sdk.Description("path to the .v file")),
			),
		),
	)
	return srv
}

// ---- argument structs -------------------------------------------------------

type checkToEndArgs struct {
	Path string `json:"path"`
}

type checkArgs struct {
	Path   string `json:"path"`
	ToLine int    `json:"to_line"`
	Omit   *int   `json:"omit,omitempty"`
}

type closeArgs struct {
	Path string `json:"path"`
}

// ---- tool handlers ----------------------------------------------------------

func (s *Server) handleCheckToEnd(
	ctx context.Context,
	_ *sdk.ServerSession,
	params *sdk.CallToolParamsFor[checkToEndArgs],
) (*sdk.CallToolResultFor[struct{}], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureStarted(ctx); err != nil {
		return nil, err
	}
	uri, content, version, err := s.openOrUpdate(ctx, params.Arguments.Path)
	if err != nil {
		return nil, err
	}
	if err := s.client.InterpretToEnd(uri, version); err != nil {
		return nil, fmt.Errorf("InterpretToEnd: %w", err)
	}
	result := s.collectResult(ctx, uri, content, math.MaxInt)
	return toTextResult(result)
}

func (s *Server) handleCheck(
	ctx context.Context,
	_ *sdk.ServerSession,
	params *sdk.CallToolParamsFor[checkArgs],
) (*sdk.CallToolResultFor[struct{}], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a := params.Arguments
	if a.Omit != nil {
		return s.checkWithOmit(ctx, a.Path, a.ToLine, *a.Omit)
	}

	if err := s.ensureStarted(ctx); err != nil {
		return nil, err
	}
	uri, content, version, err := s.openOrUpdate(ctx, a.Path)
	if err != nil {
		return nil, err
	}
	if err := s.client.InterpretToPoint(uri, version, vsrocq.Position{Line: a.ToLine, Character: 0}); err != nil {
		return nil, fmt.Errorf("InterpretToPoint: %w", err)
	}
	result := s.collectResult(ctx, uri, content, a.ToLine)
	return toTextResult(result)
}

func (s *Server) handleClose(
	ctx context.Context,
	_ *sdk.ServerSession,
	params *sdk.CallToolParamsFor[closeArgs],
) (*sdk.CallToolResultFor[struct{}], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	absPath, err := filepath.Abs(params.Arguments.Path)
	if err != nil {
		return nil, err
	}
	uri := "file://" + absPath

	if s.client != nil {
		if _, ok := s.files[uri]; ok {
			if err := s.client.DidClose(uri); err != nil {
				// vsrocq may have crashed; reset state for next call
				s.client = nil
				s.files = make(map[string]*fileState)
			} else {
				delete(s.files, uri)
			}
		}
	}
	return toTextResult(CheckResult{
		Errors: []CheckError{},
	})
}

// ---- omit (two-phase) -------------------------------------------------------

func (s *Server) checkWithOmit(ctx context.Context, path string, toLine, omit int) (*sdk.CallToolResultFor[struct{}], error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := "file://" + absPath

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	content := string(data)

	// Phase 1: DelegationSkip up to omit (fast build of context before target).
	if err := s.reinit(ctx, vsrocq.ProofOptions{
		Delegation:              vsrocq.DelegationSkip,
		Mode:                    vsrocq.ProofModeContinuous,
		PointInterpretationMode: vsrocq.PointInterpretationCursor,
	}); err != nil {
		return nil, fmt.Errorf("phase1 reinit: %w", err)
	}
	if err := s.client.DidOpen(uri, "rocq", content, 1); err != nil {
		return nil, fmt.Errorf("phase1 DidOpen: %w", err)
	}
	s.files[uri] = &fileState{version: 1, content: content}
	if err := s.client.InterpretToPoint(uri, 1, vsrocq.Position{Line: omit, Character: 0}); err != nil {
		return nil, fmt.Errorf("phase1 InterpretToPoint: %w", err)
	}
	s.drainUntilStable(uri) // discard phase-1 result

	// Phase 2: DelegationNone to toLine (verify target proofs).
	if err := s.reinit(ctx, vsrocq.ProofOptions{
		Delegation:              vsrocq.DelegationNone,
		Mode:                    vsrocq.ProofModeContinuous,
		PointInterpretationMode: vsrocq.PointInterpretationCursor,
	}); err != nil {
		return nil, fmt.Errorf("phase2 reinit: %w", err)
	}
	if err := s.client.DidOpen(uri, "rocq", content, 1); err != nil {
		return nil, fmt.Errorf("phase2 DidOpen: %w", err)
	}
	s.files[uri] = &fileState{version: 1, content: content}
	if err := s.client.InterpretToPoint(uri, 1, vsrocq.Position{Line: toLine, Character: 0}); err != nil {
		return nil, fmt.Errorf("phase2 InterpretToPoint: %w", err)
	}
	result := s.collectResult(ctx, uri, content, toLine)
	return toTextResult(result)
}

// ---- vsrocq lifecycle -------------------------------------------------------

func (s *Server) ensureStarted(ctx context.Context) error {
	if s.client != nil {
		return nil
	}
	return s.startClient(ctx, nil)
}

func (s *Server) startClient(ctx context.Context, proofOpts *vsrocq.ProofOptions) error {
	opts := vsrocq.DefaultInitOptions()
	if proofOpts != nil {
		opts.Proof = *proofOpts
	}
	c := vsrocq.NewClient(s.vsrocqBin)
	if _, err := c.Start(ctx, opts); err != nil {
		return fmt.Errorf("start vsrocq: %w", err)
	}
	s.client = c
	return nil
}

func (s *Server) reinit(ctx context.Context, proofOpts vsrocq.ProofOptions) error {
	_ = s.stopClient(ctx)
	return s.startClient(ctx, &proofOpts)
}

func (s *Server) stopClient(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.client.Shutdown(shutCtx)
	s.client = nil
	s.files = make(map[string]*fileState)
	return err
}

// ---- document management ----------------------------------------------------

// openOrUpdate ensures the file at path is open in vsrocq with current disk content.
// Before sending a content change, it interrupts any ongoing processing.
func (s *Server) openOrUpdate(ctx context.Context, path string) (uri, content string, version int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", 0, fmt.Errorf("read %s: %w", path, err)
	}
	content = string(data)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", 0, err
	}
	uri = "file://" + absPath

	fs := s.files[uri]
	switch {
	case fs == nil:
		version = 1
		if err := s.client.DidOpen(uri, "rocq", content, version); err != nil {
			return "", "", 0, fmt.Errorf("DidOpen: %w", err)
		}
		s.files[uri] = &fileState{version: 1, content: content}
	case fs.content != content:
		// Interrupt ongoing processing before changing the document.
		_ = s.client.Interrupt(uri, fs.version)
		version = fs.version + 1
		if err := s.client.DidChange(uri, version, content); err != nil {
			return "", "", 0, fmt.Errorf("DidChange: %w", err)
		}
		fs.version = version
		fs.content = content
	default:
		version = fs.version
	}
	return uri, content, version, nil
}

// ---- notification draining --------------------------------------------------

// collectResult drains vsrocq notifications and builds a CheckResult.
// toLine caps which errors are reported (math.MaxInt = no cap).
// If vsrocq dies while draining, client state is reset for the next call.
func (s *Server) collectResult(ctx context.Context, uri, content string, toLine int) CheckResult {
	processedTo, diags, goals, alive := s.drainUntilStable(uri)
	if !alive {
		// vsrocq exited unexpectedly; reset so the next call starts fresh.
		s.client = nil
		s.files = make(map[string]*fileState)
	}
	return buildCheckResult(processedTo, toLine, diags, goals)
}

// drainUntilStable reads notification channels until no new message arrives
// within stableTimeout, or BlockOnError fires.
// alive is false if vsrocq's channels were closed (process exited).
func (s *Server) drainUntilStable(uri string) (processedTo int, diags []vsrocq.Diagnostic, goals string, alive bool) {
	c := s.client
	alive = true
	timer := time.NewTimer(stableTimeout)
	defer timer.Stop()

	for {
		select {
		case h, ok := <-c.Highlights:
			if !ok {
				alive = false
				return
			}
			if h.URI == uri {
				for _, r := range h.ProcessedRange {
					if r.End.Line > processedTo {
						processedTo = r.End.Line
					}
				}
			}
			resetTimer(timer, stableTimeout)

		case pv, ok := <-c.ProofView:
			if !ok {
				alive = false
				return
			}
			if pv.PPProof != nil {
				goals = FormatProofState(pv.PPProof)
			}
			resetTimer(timer, stableTimeout)

		case d, ok := <-c.Diagnostics:
			if !ok {
				alive = false
				return
			}
			if d.URI == uri {
				diags = d.Diagnostics
			}
			resetTimer(timer, stableTimeout)

		case _, ok := <-c.BlockOnError:
			if !ok {
				alive = false
				return
			}
			// Sweep any messages already buffered before returning.
			s.drainBuffered(uri, &processedTo, &diags, &goals)
			return

		case _, ok := <-c.MoveCursor:
			if !ok {
				alive = false
				return
			}
		case _, ok := <-c.SearchResult:
			if !ok {
				alive = false
				return
			}
		case _, ok := <-c.LogMessage:
			if !ok {
				alive = false
				return
			}
		case <-timer.C:
			return
		}
	}
}

// drainBuffered does a non-blocking sweep of all notification channels.
func (s *Server) drainBuffered(uri string, processedTo *int, diags *[]vsrocq.Diagnostic, goals *string) {
	c := s.client
	for {
		select {
		case h, ok := <-c.Highlights:
			if !ok {
				return
			}
			if h.URI == uri {
				for _, r := range h.ProcessedRange {
					if r.End.Line > *processedTo {
						*processedTo = r.End.Line
					}
				}
			}
		case pv, ok := <-c.ProofView:
			if !ok {
				return
			}
			if pv.PPProof != nil {
				*goals = FormatProofState(pv.PPProof)
			}
		case d, ok := <-c.Diagnostics:
			if !ok {
				return
			}
			if d.URI == uri {
				*diags = d.Diagnostics
			}
		case _, ok := <-c.BlockOnError:
			if !ok {
				return
			}
		case _, ok := <-c.MoveCursor:
			if !ok {
				return
			}
		case _, ok := <-c.SearchResult:
			if !ok {
				return
			}
		case _, ok := <-c.LogMessage:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

// ---- result construction ----------------------------------------------------

// FormatProofState renders a StringProofState as a human-readable plaintext
// string in the classic Rocq proof-view style.
func FormatProofState(ps *vsrocq.StringProofState) string {
	if ps == nil || len(ps.Goals) == 0 {
		return ""
	}
	var b []byte
	n := len(ps.Goals)
	if n == 1 {
		b = append(b, "1 goal\n"...)
	} else {
		b = append(b, fmt.Sprintf("%d goals\n", n)...)
	}
	for i, g := range ps.Goals {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, '\n')
		for _, h := range g.Hypotheses {
			b = append(b, h...)
			b = append(b, '\n')
		}
		b = append(b, "============================\n"...)
		b = append(b, g.Goal...)
		b = append(b, '\n')
	}
	return string(b)
}

// buildCheckResult assembles a CheckResult from the stable-state outputs.
// toLine is the scope boundary: errors on lines > toLine are omitted,
// and CheckedTo is capped at toLine (math.MaxInt = no cap).
func buildCheckResult(processedTo int, toLine int, diags []vsrocq.Diagnostic, goals string) CheckResult {
	var errors []CheckError
	for _, d := range diags {
		if d.Severity == 1 && d.Range.Start.Line <= toLine {
			errors = append(errors, CheckError{
				Line:    d.Range.Start.Line,
				Message: d.Message,
			})
		}
	}

	checkedTo := processedTo
	if checkedTo > toLine {
		checkedTo = toLine
	}

	if errors == nil {
		errors = []CheckError{}
	}
	return CheckResult{
		CheckedTo:  checkedTo,
		Errors:     errors,
		ProofGoals: goals,
	}
}

func toTextResult(r CheckResult) (*sdk.CallToolResultFor[struct{}], error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return &sdk.CallToolResultFor[struct{}]{
		Content: []sdk.Content{&sdk.TextContent{Text: string(data)}},
	}, nil
}
