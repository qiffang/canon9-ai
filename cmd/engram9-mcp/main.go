// engram9-mcp is a Model Context Protocol (MCP) server for engram9.
//
// It exposes engram9 knowledge bundles as MCP tools that Claude, Codex, Pi,
// and other MCP-compatible agents can consume over stdio.
//
// Usage:
//
//	engram9-mcp -data ./data
//
// The server reads JSON-RPC 2.0 requests from stdin and writes responses to
// stdout, one JSON object per line. Diagnostic logs go to stderr.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/qiffang/engram9/internal/mcp"
	"github.com/qiffang/engram9/internal/storage"
)

func main() {
	dataDir := flag.String("data", "./data", "data directory (same as engram9 HTTP server)")
	flag.Parse()

	// Direct all log output to stderr so stdout is clean JSON-RPC.
	log.SetOutput(os.Stderr)

	store, err := storage.NewFS(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: init store: %v\n", err)
		os.Exit(1)
	}

	server := mcp.NewServer(store)

	log.Printf("engram9-mcp started (data: %s)", *dataDir)
	if err := server.Serve(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
