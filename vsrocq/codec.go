package vsrocq

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// rewriteBody rewrites a vsrocq server→client message body for go-ethereum dispatch:
//   - method "foo/bar" → "foo_bar"  (go-ethereum splits on "_")
//   - params {…}       → [{…}]      (go-ethereum expects positional array)
//
// Responses (id present) and messages with no method pass through unchanged.
func rewriteBody(body []byte) []byte {
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method,omitempty"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	if err := json.Unmarshal(body, &msg); err != nil || msg.Method == "" || msg.ID != nil {
		return body
	}
	msg.Method = strings.ReplaceAll(msg.Method, "/", "_")
	if len(msg.Params) > 0 && msg.Params[0] == '{' {
		wrapped := make(json.RawMessage, 0, len(msg.Params)+2)
		wrapped = append(append(append(wrapped, '['), msg.Params...), ']')
		msg.Params = wrapped
	}
	out, err := json.Marshal(msg)
	if err != nil {
		return body
	}
	return out
}

// readLSPFrame reads one Content-Length framed LSP message from r.
func readLSPFrame(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
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
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// lspWriter is an io.Writer that adds Content-Length framing.
//
// go-ethereum's codec writes complete JSON messages, each followed by '\n', in a
// single Write call.  lspWriter strips that trailing '\n', rewrites go-ethereum's
// positional array params ([{...}]) to LSP's named-params object ({...}), and
// wraps the body with the standard LSP Content-Length header.
type lspWriter struct {
	w io.Writer
}

func (w *lspWriter) writeMessage(body []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(w.w, header); err != nil {
		return err
	}
	_, err := w.w.Write(body)
	return err
}

// Write implements io.Writer for go-ethereum's codec.
func (w *lspWriter) Write(p []byte) (n int, err error) {
	body := p
	if len(body) > 0 && body[len(body)-1] == '\n' {
		body = body[:len(body)-1]
	}
	if len(body) == 0 {
		return len(p), nil
	}
	body = unwrapArrayParams(body)
	return len(p), w.writeMessage(body)
}

// unwrapArrayParams rewrites {"params":[{...}]} → {"params":{...}}.
//
// go-ethereum serialises CallContext arguments as a positional JSON array; the
// LSP spec and vsrocq's ppx_yojson_conv decoder both expect a named-params
// object.  When there is exactly one argument and it is a JSON object we unwrap
// the outer array.  All other cases (no params, empty array, primitive arg,
// multiple args) are left unchanged.
func unwrapArrayParams(body []byte) []byte {
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method,omitempty"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	if err := json.Unmarshal(body, &msg); err != nil || len(msg.Params) == 0 || msg.Params[0] != '[' {
		return body
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(msg.Params, &arr); err != nil || len(arr) != 1 || len(arr[0]) == 0 || arr[0][0] != '{' {
		return body
	}
	msg.Params = arr[0]
	out, err := json.Marshal(msg)
	if err != nil {
		return body
	}
	return out
}
