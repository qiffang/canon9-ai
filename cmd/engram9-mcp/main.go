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

	"github.com/qiffang/engram9/internal/agent"
	"github.com/qiffang/engram9/internal/dream"
	"github.com/qiffang/engram9/internal/mcp"
	"github.com/qiffang/engram9/internal/storage"
)

func main() {
	dataDir := flag.String("data", "", "runtime data directory (same as engram9 HTTP server)")
	bundleDir := flag.String("bundle", "", "OKF bundle directory to consume (read-only)")
	enableDream := flag.Bool("dream", false, "enable dream tool in -data mode using configured LLM provider")
	model := flag.String("model", "", "LLM model name for -dream")
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
	if *enableDream && *bundleDir != "" {
		fmt.Fprintln(os.Stderr, "error: -dream requires -data; bundles are read-only")
		flag.Usage()
		os.Exit(1)
	}

	var store storage.Store
	var err error
	var opts []mcp.Option

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

	if *enableDream {
		llm, err := newLLM(*model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: init dream LLM: %v\n", err)
			os.Exit(1)
		}
		executor := agent.NewToolExecutor(store)
		compiler := agent.NewCompileAgent(llm, executor)
		opts = append(opts, mcp.WithDreamer(dream.NewRunner(store, compiler)))
		log.Print("engram9-mcp dream tool enabled")
	}

	server := mcp.NewServer(store, opts...)
	if err := server.Serve(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func newLLM(model string) (agent.LLM, error) {
	switch os.Getenv("LLM_PROVIDER") {
	case "openai":
		if os.Getenv("OPENAI_API_KEY") == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required when LLM_PROVIDER=openai")
		}
		log.Printf("using OpenAI-compatible provider for dream (base: %s)", os.Getenv("OPENAI_BASE_URL"))
		return agent.NewOpenAILLM(model), nil
	default:
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is required")
		}
		log.Print("using Anthropic provider for dream")
		return agent.NewAnthropicLLM(model), nil
	}
}
