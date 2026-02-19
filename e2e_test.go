package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestE2EMCPProofSession(t *testing.T) {
	// Build the binary.
	binPath := filepath.Join(t.TempDir(), "rocq-mcp")
	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	ctx := context.Background()
	absPath, _ := filepath.Abs("testdata/simple.v")

	// Connect to rocq-mcp as MCP client.
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	transport := &mcp.CommandTransport{
		Command: exec.Command(binPath),
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// List tools.
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	toolNames := make(map[string]bool)
	for _, tool := range tools.Tools {
		toolNames[tool.Name] = true
	}
	for _, name := range []string{"rocq_open", "rocq_close", "rocq_check", "rocq_check_all", "rocq_step_forward", "rocq_step_backward", "rocq_sync", "rocq_get_proof_state"} {
		if !toolNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}

	// Open file.
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "rocq_open",
		Arguments: map[string]any{"file": absPath},
	})
	if err != nil {
		t.Fatalf("rocq_open: %v", err)
	}
	text := contentText(res)
	if !strings.Contains(text, "Opened") {
		t.Fatalf("expected 'Opened', got: %s", text)
	}

	// Check up to after "intros n." (line 3, col 0).
	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "rocq_check",
		Arguments: map[string]any{"file": absPath, "line": 3, "col": 0},
	})
	if err != nil {
		t.Fatalf("rocq_check: %v", err)
	}
	text = contentText(res)
	t.Logf("rocq_check result:\n%s", text)
	if !strings.Contains(text, "0 + n = n") {
		t.Errorf("expected goal '0 + n = n', got:\n%s", text)
	}

	// Check all (should complete cleanly).
	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "rocq_check_all",
		Arguments: map[string]any{"file": absPath},
	})
	if err != nil {
		t.Fatalf("rocq_check_all: %v", err)
	}
	text = contentText(res)
	t.Logf("rocq_check_all result:\n%s", text)
	if strings.Contains(text, "error") {
		t.Errorf("unexpected error: %s", text)
	}

	// Close file.
	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "rocq_close",
		Arguments: map[string]any{"file": absPath},
	})
	if err != nil {
		t.Fatalf("rocq_close: %v", err)
	}
	text = contentText(res)
	if !strings.Contains(text, "Closed") {
		t.Fatalf("expected 'Closed', got: %s", text)
	}
}

func contentText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}
