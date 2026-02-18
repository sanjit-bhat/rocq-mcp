package main

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

// request sends an LSP request and waits for the response.
func (c *vsrocqClient) request(method string, params any) (json.RawMessage, error) {
	id, err := c.codec.sendRequest(method, params)
	if err != nil {
		return nil, err
	}

	ch := make(chan *rawMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

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
