package rocq

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
)

func itoa(n int) string { return strconv.Itoa(n) }

func TestCodecRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	codec := newLSPCodec(&buf, &buf)

	// Send a request.
	id, err := codec.sendRequest("textDocument/didOpen", map[string]string{"uri": "file:///test.v"})
	if err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	if id != 1 {
		t.Fatalf("expected id=1, got %d", id)
	}

	// Decode it back.
	msg, err := codec.decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.ID == nil || *msg.ID != 1 {
		t.Fatalf("expected id=1, got %v", msg.ID)
	}
	if msg.Method == nil || *msg.Method != "textDocument/didOpen" {
		t.Fatalf("expected method textDocument/didOpen, got %v", msg.Method)
	}

	var params map[string]string
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["uri"] != "file:///test.v" {
		t.Fatalf("expected uri file:///test.v, got %s", params["uri"])
	}
}

func TestCodecNotification(t *testing.T) {
	var buf bytes.Buffer
	codec := newLSPCodec(&buf, &buf)

	err := codec.sendNotification("initialized", nil)
	if err != nil {
		t.Fatalf("sendNotification: %v", err)
	}

	msg, err := codec.decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.ID != nil {
		t.Fatalf("notification should have no id, got %v", msg.ID)
	}
	if msg.Method == nil || *msg.Method != "initialized" {
		t.Fatalf("expected method initialized, got %v", msg.Method)
	}
}

func TestCodecIDIncrement(t *testing.T) {
	var buf bytes.Buffer
	codec := newLSPCodec(&buf, &buf)

	id1, _ := codec.sendRequest("a", nil)
	id2, _ := codec.sendRequest("b", nil)
	if id1 != 1 || id2 != 2 {
		t.Fatalf("expected ids 1,2 got %d,%d", id1, id2)
	}
}

func TestDecodeContentLengthFraming(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"test"}`
	framed := "Content-Length: " + itoa(len(body)) + "\r\n\r\n" + body

	codec := newLSPCodec(bytes.NewBufferString(framed), nil)
	msg, err := codec.decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Method == nil || *msg.Method != "test" {
		t.Fatalf("expected method test, got %v", msg.Method)
	}
}
