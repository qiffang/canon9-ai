// engram9-mcp is a Model Context Protocol (MCP) server for engram9.
//
// It exposes engram9 knowledge bundles as MCP tools that Claude, Codex, Pi,
// and other MCP-compatible agents can consume over stdio.
//
// Usage:
//
//	# Consume an OKF bundle directly (read-only)
//	engram9-mcp -bundle ./examples/repo-memory
//
//	# Consume the engram9 runtime data directory (read-write)
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
	dataDir := flag.String("data", "", "runtime data directory (same as engram9 HTTP server)")
	bundleDir := flag.String("bundle", "", "OKF bundle directory to consume (read-only)")
	mode := flag.String("mode", "consumption", "tool mode: consumption (default 5 tools) or agent (4 IntegrateTools)")
	flag.Parse()

	// Direct all log output to stderr so stdout is clean JSON-RPC.
	log.SetOutput(os.Stderr)

	if *dataDir == "" && *bundleDir == "" {
		fmt.Fprintln(os.Stderr, "error: specify either -data (runtime store) or -bundle (OKF bundle)")
		flag.Usage()
		os.Exit(1)
	}
	if *dataDir != "" && *bundleDir != "" {
		fmt.Fprintln(os.Stderr, "error: -data and -bundle are mutually exclusive")
		flag.Usage()
		os.Exit(1)
	}

	var store storage.Store
	var err error

	if *bundleDir != "" {
		store, err = storage.NewBundleFS(*bundleDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: init bundle store: %v\n", err)
			os.Exit(1)
		}
		log.Printf("engram9-mcp started in bundle mode (bundle: %s)", *bundleDir)
	} else {
		store, err = storage.NewFS(*dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: init runtime store: %v\n", err)
			os.Exit(1)
		}
		log.Printf("engram9-mcp started in runtime mode (data: %s)", *dataDir)
	}

	var serverMode mcp.ServerMode
	switch *mode {
	case "consumption", "":
		serverMode = mcp.ModeConsumption
	case "agent":
		serverMode = mcp.ModeAgent
	default:
		fmt.Fprintf(os.Stderr, "error: unknown mode %q (use 'consumption' or 'agent')\n", *mode)
		os.Exit(1)
	}

	server := mcp.NewServerWithMode(store, serverMode)
	log.Printf("engram9-mcp mode: %s", serverMode)
	if err := server.Serve(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
