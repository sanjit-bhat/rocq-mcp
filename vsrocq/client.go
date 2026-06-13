package vsrocq

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/ethereum/go-ethereum/rpc"
)

// Handler functions for server-to-client notifications.
type (
	HighlightsHandler   func(*HighlightsParams)
	MoveCursorHandler   func(*MoveCursorParams)
	BlockOnErrorHandler func(*BlockOnErrorParams)
	ProofViewHandler    func(*ProofViewParams)
	SearchResultHandler func(*SearchResult)
	LogMessageHandler   func(*LogMessageParams)
	DiagnosticsHandler  func(*PublishDiagnosticsParams)
)

// Client is a connected vsrocq LSP client.
// It owns the vsrocqtop subprocess and speaks JSON-RPC 2.0 over its stdio.
type Client struct {
	cmd       *exec.Cmd
	rpcClient *rpc.Client
	writer    *lspWriter

	// Notification callbacks (set before calling Start).
	OnHighlights   HighlightsHandler
	OnMoveCursor   MoveCursorHandler
	OnBlockOnError BlockOnErrorHandler
	OnProofView    ProofViewHandler
	OnSearchResult SearchResultHandler
	OnLogMessage   LogMessageHandler
	OnDiagnostics  DiagnosticsHandler
}

// NewClient creates a Client that will launch the binary at the given path.
// Call Start to launch the process and initialize the LSP session.
func NewClient(binaryPath string, args ...string) *Client {
	c := &Client{}
	c.cmd = exec.Command(binaryPath, args...)
	return c
}

// Start launches the vsrocqtop process and performs the LSP initialize/initialized handshake.
func (c *Client) Start(ctx context.Context, rootURI string, opts *InitOptions) (*InitializeResult, error) {
	if opts == nil {
		opts = DefaultInitOptions()
	}

	stdinPipe, err := c.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	c.writer = &lspWriter{w: stdinPipe}

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	c.cmd.Stderr = io.Discard

	if err := c.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start vsrocqtop: %w", err)
	}

	// Bridge: readLoop reads Content-Length frames from stdout, dispatches
	// notifications directly, and forwards response frames as raw JSON into
	// the pipe that go-ethereum reads from.
	pr, pw := io.Pipe()
	go c.readLoop(bufio.NewReader(stdoutPipe), pw)

	c.rpcClient, err = rpc.DialIO(ctx, pr, c.writer)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	var result InitializeResult
	if err := c.rpcClient.CallContext(ctx, &result, "initialize", &InitializeParams{
		ProcessID:             c.cmd.Process.Pid,
		RootURI:               rootURI,
		Capabilities:          ClientCapabilities{},
		InitializationOptions: opts,
	}); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	if err := c.notify("initialized", struct{}{}); err != nil {
		return nil, fmt.Errorf("initialized: %w", err)
	}

	return &result, nil
}

// Shutdown sends the LSP shutdown request and exit notification, then waits
// for the process to terminate.
func (c *Client) Shutdown(ctx context.Context) error {
	var result json.RawMessage
	if err := c.rpcClient.CallContext(ctx, &result, "shutdown"); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := c.notify("exit", nil); err != nil {
		return fmt.Errorf("exit: %w", err)
	}
	c.rpcClient.Close()
	return c.cmd.Wait()
}

// ---- textDocument notifications --------------------------------------------

// DidOpen opens a document in the server.
func (c *Client) DidOpen(uri, languageID, text string, version int) error {
	return c.notify("textDocument/didOpen", &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    version,
			Text:       text,
		},
	})
}

// DidChange replaces the full document text (full-sync only).
func (c *Client) DidChange(uri string, version int, text string) error {
	return c.notify("textDocument/didChange", &DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: text},
		},
	})
}

// DidClose closes a document.
func (c *Client) DidClose(uri string) error {
	return c.notify("textDocument/didClose", &DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
}

// ---- vsrocq custom notifications (client → server) -------------------------

// InterpretToPoint asks the server to interpret the document up to pos.
func (c *Client) InterpretToPoint(uri string, version int, pos Position) error {
	return c.notify("prover/interpretToPoint", &InterpretToPointParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
	})
}

// InterpretToEnd asks the server to interpret the entire document.
func (c *Client) InterpretToEnd(uri string, version int) error {
	return c.notify("prover/interpretToEnd", &InterpretToEndParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
	})
}

// StepForward advances one step in the document.
func (c *Client) StepForward(uri string, version int) error {
	return c.notify("prover/stepForward", &StepForwardParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
	})
}

// StepBackward retracts one step in the document.
func (c *Client) StepBackward(uri string, version int) error {
	return c.notify("prover/stepBackward", &StepBackwardParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
	})
}

// Interrupt asks the server to interrupt ongoing processing.
func (c *Client) Interrupt(uri string, version int) error {
	return c.notify("prover/interrupt", &InterruptParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
	})
}

// ---- vsrocq custom requests (client → server, with response) ---------------

// ResetRocq resets the Rocq interpreter for the given document.
func (c *Client) ResetRocq(ctx context.Context, uri string) error {
	var result json.RawMessage
	return c.rpcClient.CallContext(ctx, &result, "prover/resetRocq", &ResetParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
}

// Search initiates an asynchronous search. Results arrive via OnSearchResult.
func (c *Client) Search(ctx context.Context, uri string, version int, pos Position, pattern, id string) error {
	var result json.RawMessage
	return c.rpcClient.CallContext(ctx, &result, "prover/search", &SearchParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
		ID:           id,
	})
}

// About queries information about a term at the given position.
func (c *Client) About(ctx context.Context, uri string, version int, pos Position, pattern string) (Pp, error) {
	var result json.RawMessage
	err := c.rpcClient.CallContext(ctx, &result, "prover/about", &AboutParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
	})
	return Pp(result), err
}

// Check type-checks a term at the given position.
func (c *Client) Check(ctx context.Context, uri string, version int, pos Position, pattern string) (Pp, error) {
	var result json.RawMessage
	err := c.rpcClient.CallContext(ctx, &result, "prover/check", &CheckParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
	})
	return Pp(result), err
}

// Locate looks up a name in the Rocq namespace.
func (c *Client) Locate(ctx context.Context, uri string, version int, pos Position, pattern string) (Pp, error) {
	var result json.RawMessage
	err := c.rpcClient.CallContext(ctx, &result, "prover/locate", &LocateParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
	})
	return Pp(result), err
}

// Print pretty-prints a term.
func (c *Client) Print(ctx context.Context, uri string, version int, pos Position, pattern string) (Pp, error) {
	var result json.RawMessage
	err := c.rpcClient.CallContext(ctx, &result, "prover/print", &PrintParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
	})
	return Pp(result), err
}

// DocumentState returns the full pretty-printed document state.
func (c *Client) DocumentState(ctx context.Context, uri string) (*DocumentStateResult, error) {
	var result DocumentStateResult
	err := c.rpcClient.CallContext(ctx, &result, "prover/documentState", &DocumentStateParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	return &result, err
}

// DocumentProofs returns the proof structure of a document.
func (c *Client) DocumentProofs(ctx context.Context, uri string) (*DocumentProofsResult, error) {
	var result DocumentProofsResult
	err := c.rpcClient.CallContext(ctx, &result, "prover/documentProofs", &DocumentProofsParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	return &result, err
}

// ---- internal transport -----------------------------------------------------

// notify sends a JSON-RPC notification (no response expected) by writing
// directly to the LSP writer, bypassing go-ethereum's request-response cycle.
func (c *Client) notify(method string, params any) error {
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.writer.WriteMessage(body)
}

// readLoop reads Content-Length frames from stdout. Notifications are
// dispatched to the registered callbacks; responses are forwarded as raw JSON
// into the pipe that go-ethereum's codec reads from.
func (c *Client) readLoop(stdout *bufio.Reader, pw *io.PipeWriter) {
	defer pw.Close()
	for {
		body, err := readLSPFrame(stdout)
		if err != nil {
			return
		}

		var peek struct {
			ID     *json.RawMessage `json:"id,omitempty"`
			Method string           `json:"method,omitempty"`
		}
		if json.Unmarshal(body, &peek) == nil && peek.Method != "" && peek.ID == nil {
			c.handleNotification(peek.Method, body)
		} else {
			if _, err := pw.Write(body); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleNotification(method string, body []byte) {
	var env struct {
		Params json.RawMessage `json:"params,omitempty"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	p := env.Params

	switch method {
	case "prover/updateHighlights":
		if c.OnHighlights != nil {
			var v HighlightsParams
			if json.Unmarshal(p, &v) == nil {
				c.OnHighlights(&v)
			}
		}
	case "prover/moveCursor":
		if c.OnMoveCursor != nil {
			var v MoveCursorParams
			if json.Unmarshal(p, &v) == nil {
				c.OnMoveCursor(&v)
			}
		}
	case "prover/blockOnError":
		if c.OnBlockOnError != nil {
			var v BlockOnErrorParams
			if json.Unmarshal(p, &v) == nil {
				c.OnBlockOnError(&v)
			}
		}
	case "prover/proofView":
		if c.OnProofView != nil {
			var v ProofViewParams
			if json.Unmarshal(p, &v) == nil {
				c.OnProofView(&v)
			}
		}
	case "prover/searchResult":
		if c.OnSearchResult != nil {
			var v SearchResult
			if json.Unmarshal(p, &v) == nil {
				c.OnSearchResult(&v)
			}
		}
	case "prover/debugMessage":
		if c.OnLogMessage != nil {
			var v LogMessageParams
			if json.Unmarshal(p, &v) == nil {
				c.OnLogMessage(&v)
			}
		}
	case "textDocument/publishDiagnostics":
		if c.OnDiagnostics != nil {
			var v PublishDiagnosticsParams
			if json.Unmarshal(p, &v) == nil {
				c.OnDiagnostics(&v)
			}
		}
	}
}
