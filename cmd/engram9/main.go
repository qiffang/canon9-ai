package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/qiffang/engram9/internal/agent"
	"github.com/qiffang/engram9/internal/api"
)

func main() {
	addr := flag.String("addr", ":9090", "listen address")
	dataDir := flag.String("data", "./data", "data directory")
	model := flag.String("model", "", "Anthropic model (default: claude-sonnet-4-20250514)")
	flag.Parse()

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "error: ANTHROPIC_API_KEY environment variable is required")
		os.Exit(1)
	}

	llm := agent.NewAnthropicLLM(*model)
	handler, err := api.New(*dataDir, llm)
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	log.Printf("engram9 listening on %s (data: %s)", *addr, *dataDir)
	if err := http.ListenAndServe(*addr, handler.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
