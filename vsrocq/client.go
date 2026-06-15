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

// Start launches the vsrocqtop process and sets up the JSON-RPC transport and
// notification handlers. Call Initialize to perform the LSP initialize/initialized
// handshake before using any other methods.
func (c *Client) Start(ctx context.Context) error {
	stdinPipe, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	writer := &lspWriter{w: stdinPipe}

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	c.cmd.Stderr = io.Discard

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start vsrocqtop: %w", err)
	}

	pr, pw := io.Pipe()
	go c.lspBridge(bufio.NewReader(stdoutPipe), pw)

	c.rpcClient, err = rpc.DialIO(ctx, pr, writer)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	proverH := &proverNotifHandler{
		highlights:   c.Highlights,
		moveCursor:   c.MoveCursor,
		blockOnError: c.BlockOnError,
		proofView:    c.ProofView,
		searchResult: c.SearchResult,
		logMessage:   c.LogMessage,
	}
	if err := c.rpcClient.RegisterName("prover", proverH); err != nil {
		return fmt.Errorf("register prover handlers: %w", err)
	}
	if err := c.rpcClient.RegisterName("textDocument", &textDocNotifHandler{c.Diagnostics}); err != nil {
		return fmt.Errorf("register textDocument handlers: %w", err)
	}

	return nil
}

// Initialize sends the LSP initialize/initialized handshake. It may be called
// multiple times to reconfigure the server (e.g. change delegation strategy)
// without restarting the process — vsrocqserv resets its init vars on each call.
func (c *Client) Initialize(ctx context.Context, opts *InitOptions) (*InitializeResult, error) {
	if opts == nil {
		opts = DefaultInitOptions()
	}

	var result InitializeResult
	if err := c.rpcClient.CallContext(ctx, &result, "initialize", &InitializeParams{
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

// lspToRPC reads one Content-Length frame from an LSP stream and rewrites the
// JSON body so go-ethereum can dispatch it.
func lspToRPC(r *bufio.Reader) ([]byte, error) {
	body, err := readLSPFrame(r)
	if err != nil {
		return nil, err
	}
	return rewriteBody(body), nil
}

// lspBridge pumps messages from vsrocqtop's stdout into pw for go-ethereum.
func (c *Client) lspBridge(stdout *bufio.Reader, pw *io.PipeWriter) {
	defer pw.Close()
	defer c.closeNotifChans()
	for {
		body, err := lspToRPC(stdout)
		if err != nil {
			return
		}
		if _, err := pw.Write(body); err != nil {
			return
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

type proverNotifHandler struct {
	highlights   chan *HighlightsParams
	moveCursor   chan *MoveCursorParams
	blockOnError chan *BlockOnErrorParams
	proofView    chan *ProofViewParams
	searchResult chan *SearchResult
	logMessage   chan *LogMessageParams
}

func sendOrDrop[T any](ch chan *T, v T) {
	select {
	case ch <- &v:
	default:
	}
}

func (h *proverNotifHandler) UpdateHighlights(_ context.Context, p HighlightsParams) error {
	sendOrDrop(h.highlights, p)
	return nil
}

func (h *proverNotifHandler) MoveCursor(_ context.Context, p MoveCursorParams) error {
	sendOrDrop(h.moveCursor, p)
	return nil
}

func (h *proverNotifHandler) BlockOnError(_ context.Context, p BlockOnErrorParams) error {
	sendOrDrop(h.blockOnError, p)
	return nil
}

func (h *proverNotifHandler) ProofView(_ context.Context, p ProofViewParams) error {
	sendOrDrop(h.proofView, p)
	return nil
}

func (h *proverNotifHandler) SearchResult(_ context.Context, p SearchResult) error {
	sendOrDrop(h.searchResult, p)
	return nil
}

func (h *proverNotifHandler) DebugMessage(_ context.Context, p LogMessageParams) error {
	sendOrDrop(h.logMessage, p)
	return nil
}

type textDocNotifHandler struct {
	diagnostics chan *PublishDiagnosticsParams
}

func (h *textDocNotifHandler) PublishDiagnostics(_ context.Context, p PublishDiagnosticsParams) error {
	sendOrDrop(h.diagnostics, p)
	return nil
}
