package rocq

// lsp.go — Content-Length framed JSON-RPC 2.0 codec for LSP communication.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// lspCodec handles Content-Length framed JSON-RPC reading and writing.
type lspCodec struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex // protects writer
	nextID atomic.Int64
}

func newLSPCodec(r io.Reader, w io.Writer) *lspCodec {
	c := &lspCodec{
		reader: bufio.NewReader(r),
		writer: w,
	}
	c.nextID.Store(1)
	return c
}

// rawMessage is the decoded JSON-RPC envelope.
type rawMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method *string         `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Wire types for encoding.

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// encode writes a JSON-RPC message with Content-Length framing.
func (c *lspCodec) encode(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := io.WriteString(c.writer, header); err != nil {
		return err
	}
	_, err = c.writer.Write(data)
	return err
}

// decode reads one Content-Length framed JSON-RPC message.
func (c *lspCodec) decode() (*rawMessage, error) {
	// Read headers until empty line.
	contentLength := -1
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if after, ok := strings.CutPrefix(line, "Content-Length: "); ok {
			n, err := strconv.Atoi(after)
			if err != nil {
				return nil, fmt.Errorf("parse Content-Length: %w", err)
			}
			contentLength = n
		}
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var msg rawMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &msg, nil
}

// sendRequest sends a JSON-RPC request and returns the assigned ID.
func (c *lspCodec) sendRequest(method string, params any) (int64, error) {
	id := c.nextID.Add(1) - 1

	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return 0, err
		}
	}

	req := &jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: rawParams}
	return id, c.encode(req)
}

// sendNotification sends a JSON-RPC notification (no ID, no response expected).
func (c *lspCodec) sendNotification(method string, params any) error {
	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return err
		}
	}

	msg := &jsonRPCNotification{JSONRPC: "2.0", Method: method, Params: rawParams}
	return c.encode(msg)
}
