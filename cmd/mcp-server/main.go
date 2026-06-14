// Command mcp-server runs a Rocq MCP server over stdio.
//
// It launches vsrocqtop on startup and exposes three MCP tools:
// check_to_end, check, and close_file.
//
// Usage:
//
//	mcp-server [--vsrocq-bin PATH] [--workspace URI]
//
// If --vsrocq-bin is omitted, the binary is discovered from VSROCQ_BIN,
// `opam exec --switch pav -- which vsrocqtop`, or PATH in that order.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	rocqmcp "github.com/sanjit-bhat/rocq-mcp/mcp"
)

func main() {
	vsrocqBin := flag.String("vsrocq-bin", "", "path to vsrocqtop binary (default: auto-discover)")
	workspace := flag.String("workspace", "", "workspace root URI (default: file:// + CWD)")
	flag.Parse()

	bin := resolveVsrocqBin(*vsrocqBin)
	rootURI := resolveRootURI(*workspace)

	ctx := context.Background()
	srv := rocqmcp.New(bin, rootURI)

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("launch vsrocq: %v", err)
	}
	defer srv.Shutdown(ctx) //nolint:errcheck

	mcpSrv := srv.MCPServer()
	t := sdk.NewStdioTransport()
	if err := mcpSrv.Run(ctx, t); err != nil {
		log.Printf("MCP server stopped: %v", err)
	}
}

func resolveVsrocqBin(flag string) string {
	if flag != "" {
		return flag
	}
	if p := os.Getenv("VSROCQ_BIN"); p != "" {
		return p
	}
	if out, err := exec.Command("opam", "exec", "--switch", "pav", "--", "which", "vsrocqtop").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			return p
		}
	}
	if p, err := exec.LookPath("vsrocqtop"); err == nil {
		return p
	}
	log.Fatal("vsrocqtop not found; set --vsrocq-bin or VSROCQ_BIN env var")
	return ""
}

func resolveRootURI(flag string) string {
	if flag != "" {
		return flag
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}
	return "file://" + cwd
}
