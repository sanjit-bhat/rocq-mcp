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

// notifChanBuf is the buffer size for every notification channel.
// Notifications arriving while the consumer is not reading are buffered;
// overflow is dropped silently.
const notifChanBuf = 64

// Client is a connected vsrocq LSP client.
// It owns the vsrocqtop subprocess and speaks JSON-RPC 2.0 over its stdio.
//
// Server-to-client notifications are delivered on the exported channels below.
// Each channel is buffered (notifChanBuf); callers should drain it promptly or
// accept that low-priority notifications may be dropped.  All channels are
// closed when the subprocess exits.
type Client struct {
	cmd       *exec.Cmd
	rpcClient *rpc.Client

	// Notification channels (closed on subprocess exit).
	Highlights   chan *HighlightsParams
	MoveCursor   chan *MoveCursorParams
	BlockOnError chan *BlockOnErrorParams
	ProofView    chan *ProofViewParams
	SearchResult chan *SearchResult
	LogMessage   chan *LogMessageParams
	Diagnostics  chan *PublishDiagnosticsParams
}

// NewClient creates a Client that will launch the binary at the given path.
// Call Start to launch the process and initialize the LSP session.
func NewClient(binaryPath string, args ...string) *Client {
	c := &Client{}
	c.cmd = exec.Command(binaryPath, args...)

	c.Highlights = make(chan *HighlightsParams, notifChanBuf)
	c.MoveCursor = make(chan *MoveCursorParams, notifChanBuf)
	c.BlockOnError = make(chan *BlockOnErrorParams, notifChanBuf)
	c.ProofView = make(chan *ProofViewParams, notifChanBuf)
	c.SearchResult = make(chan *SearchResult, notifChanBuf)
	c.LogMessage = make(chan *LogMessageParams, notifChanBuf)
	c.Diagnostics = make(chan *PublishDiagnosticsParams, notifChanBuf)

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
	writer := &lspWriter{w: stdinPipe}

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

	c.rpcClient, err = rpc.DialIO(ctx, pr, writer)
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

	if err := c.rpcClient.Notify(context.Background(), "initialized", struct{}{}); err != nil {
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
	if err := c.rpcClient.Notify(context.Background(), "exit", nil); err != nil {
		return fmt.Errorf("exit: %w", err)
	}
	c.rpcClient.Close()
	return c.cmd.Wait()
}

// textDocument notifications

// DidOpen opens a document in the server.
func (c *Client) DidOpen(uri, languageID, text string, version int) error {
	return c.rpcClient.Notify(context.Background(), "textDocument/didOpen", &DidOpenTextDocumentParams{
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
	return c.rpcClient.Notify(context.Background(), "textDocument/didChange", &DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: text},
		},
	})
}

// DidClose closes a document.
func (c *Client) DidClose(uri string) error {
	return c.rpcClient.Notify(context.Background(), "textDocument/didClose", &DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
}

// vsrocq custom notifications (client → server)

// InterpretToPoint asks the server to interpret the document up to pos.
func (c *Client) InterpretToPoint(uri string, version int, pos Position) error {
	return c.rpcClient.Notify(context.Background(), "prover/interpretToPoint", &InterpretToPointParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
	})
}

// InterpretToEnd asks the server to interpret the entire document.
func (c *Client) InterpretToEnd(uri string, version int) error {
	return c.rpcClient.Notify(context.Background(), "prover/interpretToEnd", &InterpretToEndParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
	})
}

// StepForward advances one step in the document.
func (c *Client) StepForward(uri string, version int) error {
	return c.rpcClient.Notify(context.Background(), "prover/stepForward", &StepForwardParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
	})
}

// StepBackward retracts one step in the document.
func (c *Client) StepBackward(uri string, version int) error {
	return c.rpcClient.Notify(context.Background(), "prover/stepBackward", &StepBackwardParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
	})
}

// Interrupt asks the server to interrupt ongoing processing.
func (c *Client) Interrupt(uri string, version int) error {
	return c.rpcClient.Notify(context.Background(), "prover/interrupt", &InterruptParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
	})
}

// vsrocq custom requests (client → server, with response)

// ResetRocq resets the Rocq interpreter for the given document.
func (c *Client) ResetRocq(ctx context.Context, uri string) error {
	var result json.RawMessage
	return c.rpcClient.CallContext(ctx, &result, "prover/resetRocq", &ResetParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
}

// Search initiates an asynchronous search. Results arrive on SearchResult.
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

// internal transport

// readLoop reads Content-Length frames from stdout. Notifications are
// dispatched to the appropriate channels; responses are forwarded as raw JSON
// into the pipe that go-ethereum's codec reads from.
func (c *Client) readLoop(stdout *bufio.Reader, pw *io.PipeWriter) {
	defer pw.Close()
	defer c.closeNotifChans()
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

func (c *Client) closeNotifChans() {
	close(c.Highlights)
	close(c.MoveCursor)
	close(c.BlockOnError)
	close(c.ProofView)
	close(c.SearchResult)
	close(c.LogMessage)
	close(c.Diagnostics)
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
		var v HighlightsParams
		if json.Unmarshal(p, &v) == nil {
			select {
			case c.Highlights <- &v:
			default:
			}
		}
	case "prover/moveCursor":
		var v MoveCursorParams
		if json.Unmarshal(p, &v) == nil {
			select {
			case c.MoveCursor <- &v:
			default:
			}
		}
	case "prover/blockOnError":
		var v BlockOnErrorParams
		if json.Unmarshal(p, &v) == nil {
			select {
			case c.BlockOnError <- &v:
			default:
			}
		}
	case "prover/proofView":
		var v ProofViewParams
		if json.Unmarshal(p, &v) == nil {
			select {
			case c.ProofView <- &v:
			default:
			}
		}
	case "prover/searchResult":
		var v SearchResult
		if json.Unmarshal(p, &v) == nil {
			select {
			case c.SearchResult <- &v:
			default:
			}
		}
	case "prover/debugMessage":
		var v LogMessageParams
		if json.Unmarshal(p, &v) == nil {
			select {
			case c.LogMessage <- &v:
			default:
			}
		}
	case "textDocument/publishDiagnostics":
		var v PublishDiagnosticsParams
		if json.Unmarshal(p, &v) == nil {
			select {
			case c.Diagnostics <- &v:
			default:
			}
		}
	}
}
