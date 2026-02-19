package main

// vsrocq.go — vsrocqtop subprocess management and LSP client handshake.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
)

// vsrocqClient manages a vsrocqtop subprocess and its LSP communication.
type vsrocqClient struct {
	cmd   *exec.Cmd
	codec *lspCodec

	// Pending request responses, keyed by ID.
	pending   map[int64]chan *rawMessage
	pendingMu sync.Mutex

	// Notification handlers.
	handlers   map[string]func(json.RawMessage)
	handlersMu sync.RWMutex
}

func newVsrocqClient(extraArgs []string) (*vsrocqClient, error) {
	args := append([]string{}, extraArgs...)
	cmd := exec.Command("vsrocqtop", args...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start vsrocqtop: %w", err)
	}

	client := &vsrocqClient{
		cmd:      cmd,
		codec:    newLSPCodec(stdout, stdin),
		pending:  make(map[int64]chan *rawMessage),
		handlers: make(map[string]func(json.RawMessage)),
	}

	go client.readLoop()
	return client, nil
}

// readLoop reads messages from vsrocqtop and dispatches them.
func (c *vsrocqClient) readLoop() {
	for {
		msg, err := c.codec.decode()
		if err != nil {
			log.Printf("vsrocq read error: %v", err)
			return
		}

		if msg.ID != nil && msg.Method == nil {
			// Response to a request.
			c.pendingMu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.pendingMu.Unlock()
			if ok {
				ch <- msg
			}
		} else if msg.ID != nil && msg.Method != nil {
			// Server→client request (e.g. workspace/configuration).
			c.handleServerRequest(*msg.ID, *msg.Method, msg.Params)
		} else if msg.Method != nil {
			// Notification from server.
			c.handlersMu.RLock()
			handler, ok := c.handlers[*msg.Method]
			c.handlersMu.RUnlock()
			if ok {
				handler(msg.Params)
			} else {
				log.Printf("unhandled notification: %s", *msg.Method)
			}
		}
	}
}

// handleServerRequest responds to server→client requests.
func (c *vsrocqClient) handleServerRequest(id int64, method string, params json.RawMessage) {
	switch method {
	case "workspace/configuration":
		// Respond with vsrocq settings for each requested item.
		settings := map[string]any{
			"proof": map[string]any{
				"mode": 0, // Manual
			},
		}
		// workspace/configuration expects an array of results, one per item.
		// We return our settings for each item requested.
		var req struct {
			Items []any `json:"items"`
		}
		n := 1
		if json.Unmarshal(params, &req) == nil {
			n = len(req.Items)
		}
		results := make([]any, n)
		for i := range results {
			results[i] = settings
		}
		data, err := json.Marshal(results)
		if err != nil {
			log.Printf("marshal workspace/configuration response: %v", err)
			return
		}
		if err := c.codec.encode(&jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result:  data,
		}); err != nil {
			log.Printf("send workspace/configuration response: %v", err)
		}
	default:
		log.Printf("unhandled server request: %s (id=%d)", method, id)
		if err := c.codec.encode(&jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result:  json.RawMessage("null"),
		}); err != nil {
			log.Printf("send default response: %v", err)
		}
	}
}

// request sends an LSP request and waits for the response.
func (c *vsrocqClient) request(method string, params any) (json.RawMessage, error) {
	ch := make(chan *rawMessage, 1)

	// Register the response channel before sending so readLoop can't
	// deliver the response before we're listening.
	id := c.codec.nextID.Add(1) - 1
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			c.pendingMu.Lock()
			delete(c.pending, id)
			c.pendingMu.Unlock()
			return nil, err
		}
	}
	if err := c.codec.encode(&jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	resp := <-ch
	if resp.Error != nil {
		return nil, fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// notify sends an LSP notification.
func (c *vsrocqClient) notify(method string, params any) error {
	return c.codec.sendNotification(method, params)
}

// onNotification registers a handler for a server notification method.
func (c *vsrocqClient) onNotification(method string, handler func(json.RawMessage)) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[method] = handler
}

// initialize performs the LSP initialize/initialized handshake.
func (c *vsrocqClient) initialize(rootURI string) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
			},
		},
	}

	_, err := c.request("initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	if err := c.notify("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("initialized: %w", err)
	}

	// Set manual proof mode.
	settings := map[string]any{
		"settings": map[string]any{
			"vsrocq": map[string]any{
				"proof": map[string]any{
					"mode": 0, // Manual mode
				},
			},
		},
	}
	if err := c.notify("workspace/didChangeConfiguration", settings); err != nil {
		return fmt.Errorf("didChangeConfiguration: %w", err)
	}

	return nil
}

// shutdown sends the shutdown request and exit notification.
func (c *vsrocqClient) shutdown() error {
	_, err := c.request("shutdown", nil)
	if err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := c.notify("exit", nil); err != nil {
		return fmt.Errorf("exit: %w", err)
	}
	return c.cmd.Wait()
}
