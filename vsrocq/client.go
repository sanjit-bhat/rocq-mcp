package vsrocq

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Handler functions for server-to-client notifications.
type (
	HighlightsHandler  func(*HighlightsParams)
	MoveCursorHandler  func(*MoveCursorParams)
	BlockOnErrorHandler func(*BlockOnErrorParams)
	ProofViewHandler   func(*ProofViewParams)
	SearchResultHandler func(*SearchResult)
	LogMessageHandler  func(*LogMessageParams)
	DiagnosticsHandler func(*PublishDiagnosticsParams)
)

// Client is a connected vsrocq LSP client.
// It owns the vsrocqtop subprocess and speaks JSON-RPC 2.0 over its stdio.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan *response

	closed chan struct{}
	once   sync.Once

	// Notification callbacks (set before calling Start).
	OnHighlights   HighlightsHandler
	OnMoveCursor   MoveCursorHandler
	OnBlockOnError BlockOnErrorHandler
	OnProofView    ProofViewHandler
	OnSearchResult SearchResultHandler
	OnLogMessage   LogMessageHandler
	OnDiagnostics  DiagnosticsHandler
}

// NewClient creates a Client that will launch the binary at the given path
// (typically the result of `opam exec -- which vsrocqtop`).
// Call Start to launch the process and initialize the LSP session.
func NewClient(binaryPath string, args ...string) *Client {
	c := &Client{
		pending: make(map[int64]chan *response),
		closed:  make(chan struct{}),
	}
	c.cmd = exec.Command(binaryPath, args...)
	return c
}

// Start launches the vsrocqtop process and performs the LSP initialize/initialized handshake.
// rootURI should be a file:// URI for the workspace root (e.g. "file:///tmp/workspace").
// opts may be nil, in which case DefaultInitOptions is used.
func (c *Client) Start(ctx context.Context, rootURI string, opts *InitOptions) (*InitializeResult, error) {
	if opts == nil {
		opts = DefaultInitOptions()
	}

	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	c.stdin = stdin

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReader(stdoutPipe)

	// Discard stderr to avoid blocking.
	c.cmd.Stderr = io.Discard

	if err := c.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start vsrocqtop: %w", err)
	}

	go c.readLoop()

	// Standard LSP initialize request.
	var result InitializeResult
	err = c.call(ctx, "initialize", &InitializeParams{
		ProcessID:             c.cmd.Process.Pid,
		RootURI:               rootURI,
		Capabilities:          ClientCapabilities{},
		InitializationOptions: opts,
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// Mandatory initialized notification.
	if err := c.notify("initialized", struct{}{}); err != nil {
		return nil, fmt.Errorf("initialized: %w", err)
	}

	return &result, nil
}

// Shutdown sends the LSP shutdown request and exit notification, then waits
// for the process to terminate.
func (c *Client) Shutdown(ctx context.Context) error {
	var result json.RawMessage
	if err := c.call(ctx, "shutdown", nil, &result); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := c.notify("exit", nil); err != nil {
		return fmt.Errorf("exit: %w", err)
	}
	c.close()
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
	return c.call(ctx, "prover/resetRocq", &ResetParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}, &result)
}

// Search initiates an asynchronous search. Results arrive via OnSearchResult.
// id is a caller-supplied correlation key returned in each SearchResult.
func (c *Client) Search(ctx context.Context, uri string, version int, pos Position, pattern, id string) error {
	var result json.RawMessage
	return c.call(ctx, "prover/search", &SearchParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
		ID:           id,
	}, &result)
}

// About queries information about a term at the given position.
func (c *Client) About(ctx context.Context, uri string, version int, pos Position, pattern string) (Pp, error) {
	var result json.RawMessage
	err := c.call(ctx, "prover/about", &AboutParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
	}, &result)
	return Pp(result), err
}

// Check type-checks a term at the given position.
func (c *Client) Check(ctx context.Context, uri string, version int, pos Position, pattern string) (Pp, error) {
	var result json.RawMessage
	err := c.call(ctx, "prover/check", &CheckParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
	}, &result)
	return Pp(result), err
}

// Locate looks up a name in the Rocq namespace.
func (c *Client) Locate(ctx context.Context, uri string, version int, pos Position, pattern string) (Pp, error) {
	var result json.RawMessage
	err := c.call(ctx, "prover/locate", &LocateParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
	}, &result)
	return Pp(result), err
}

// Print pretty-prints a term.
func (c *Client) Print(ctx context.Context, uri string, version int, pos Position, pattern string) (Pp, error) {
	var result json.RawMessage
	err := c.call(ctx, "prover/print", &PrintParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     pos,
		Pattern:      pattern,
	}, &result)
	return Pp(result), err
}

// DocumentState returns the full pretty-printed document state.
func (c *Client) DocumentState(ctx context.Context, uri string) (*DocumentStateResult, error) {
	var result DocumentStateResult
	err := c.call(ctx, "prover/documentState", &DocumentStateParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}, &result)
	return &result, err
}

// DocumentProofs returns the proof structure of a document.
func (c *Client) DocumentProofs(ctx context.Context, uri string) (*DocumentProofsResult, error) {
	var result DocumentProofsResult
	err := c.call(ctx, "prover/documentProofs", &DocumentProofsParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}, &result)
	return &result, err
}

// ---- internal transport -----------------------------------------------------

// call sends a JSON-RPC request and waits for the response.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan *response, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	req := request{
		JSONRPC: "2.0",
		ID:      int(id),
		Method:  method,
		Params:  params,
	}
	if err := c.writeMessage(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	case <-c.closed:
		return fmt.Errorf("client closed")
	}
}

// notify sends a JSON-RPC notification (no response expected).
func (c *Client) notify(method string, params any) error {
	return c.writeMessage(notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

// writeMessage serialises msg as JSON and writes it with the LSP Content-Length framing.
func (c *Client) writeMessage(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(body)
	return err
}

// readLoop reads messages from the server and dispatches them.
func (c *Client) readLoop() {
	defer c.close()
	for {
		msg, err := c.readMessage()
		if err != nil {
			return
		}
		c.dispatch(msg)
	}
}

// readMessage reads one LSP-framed JSON message.
func (c *Client) readMessage() (*incomingMsg, error) {
	// Read headers.
	contentLength := -1
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %w", err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, err
	}

	var msg incomingMsg
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &msg, nil
}

// dispatch routes an incoming message to a pending request or a notification handler.
func (c *Client) dispatch(msg *incomingMsg) {
	// It's a response if it has an ID and no Method.
	if msg.ID != nil && msg.Method == "" {
		id := int64(*msg.ID)
		c.mu.Lock()
		ch, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.mu.Unlock()
		if ok {
			ch <- &response{
				JSONRPC: msg.JSONRPC,
				ID:      msg.ID,
				Result:  msg.Result,
				Error:   msg.Error,
			}
		}
		return
	}

	// It's a notification or a server-initiated request.
	c.handleNotification(msg)
}

// handleNotification dispatches a server notification to the appropriate handler.
func (c *Client) handleNotification(msg *incomingMsg) {
	switch msg.Method {
	case "prover/updateHighlights":
		if c.OnHighlights != nil {
			var p HighlightsParams
			if json.Unmarshal(msg.Params, &p) == nil {
				c.OnHighlights(&p)
			}
		}
	case "prover/moveCursor":
		if c.OnMoveCursor != nil {
			var p MoveCursorParams
			if json.Unmarshal(msg.Params, &p) == nil {
				c.OnMoveCursor(&p)
			}
		}
	case "prover/blockOnError":
		if c.OnBlockOnError != nil {
			var p BlockOnErrorParams
			if json.Unmarshal(msg.Params, &p) == nil {
				c.OnBlockOnError(&p)
			}
		}
	case "prover/proofView":
		if c.OnProofView != nil {
			var p ProofViewParams
			if json.Unmarshal(msg.Params, &p) == nil {
				c.OnProofView(&p)
			}
		}
	case "prover/searchResult":
		if c.OnSearchResult != nil {
			var p SearchResult
			if json.Unmarshal(msg.Params, &p) == nil {
				c.OnSearchResult(&p)
			}
		}
	case "prover/debugMessage":
		if c.OnLogMessage != nil {
			var p LogMessageParams
			if json.Unmarshal(msg.Params, &p) == nil {
				c.OnLogMessage(&p)
			}
		}
	case "textDocument/publishDiagnostics":
		if c.OnDiagnostics != nil {
			var p PublishDiagnosticsParams
			if json.Unmarshal(msg.Params, &p) == nil {
				c.OnDiagnostics(&p)
			}
		}
	}
}

func (c *Client) close() {
	c.once.Do(func() { close(c.closed) })
}
