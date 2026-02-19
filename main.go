package main

// main.go — entrypoint: starts the MCP server over stdio.

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// All args after the binary name are passed through to vsrocqtop.
	vsrocqArgs := os.Args[1:]

	sm := newStateManager(vsrocqArgs)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "rocq-mcp",
		Version: "0.1.0",
	}, nil)

	registerTools(server, sm)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}

	if err := sm.shutdown(); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
