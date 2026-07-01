package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/qiffang/engram9/internal/agent"
	"github.com/qiffang/engram9/internal/api"
	"github.com/qiffang/engram9/internal/okf"
	"github.com/qiffang/engram9/internal/repo"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "validate":
			os.Exit(runValidate(os.Args[2:]))
		case "migrate-okf":
			os.Exit(runMigrateOKF(os.Args[2:]))
		case "repo":
			os.Exit(runRepo(os.Args[2:]))
		}
	}
	runServe(os.Args[1:])
}

func runServe(args []string) {
	flags := flag.NewFlagSet("engram9", flag.ExitOnError)
	addr := flags.String("addr", ":9090", "listen address")
	dataDir := flags.String("data", "./data", "data directory")
	model := flags.String("model", "", "LLM model name")
	compileInterval := flags.Duration("compile-interval", 30*time.Minute, "auto-compile interval (0 to disable)")
	_ = flags.Parse(args)

	var llm agent.LLM
	switch os.Getenv("LLM_PROVIDER") {
	case "openai":
		if os.Getenv("OPENAI_API_KEY") == "" {
			fmt.Fprintln(os.Stderr, "error: OPENAI_API_KEY environment variable is required when LLM_PROVIDER=openai")
			os.Exit(1)
		}
		llm = agent.NewOpenAILLM(*model)
		log.Printf("using OpenAI-compatible provider (base: %s)", os.Getenv("OPENAI_BASE_URL"))
	default:
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			fmt.Fprintln(os.Stderr, "error: ANTHROPIC_API_KEY environment variable is required")
			os.Exit(1)
		}
		llm = agent.NewAnthropicLLM(*model)
		log.Print("using Anthropic provider")
	}
	handler, err := api.New(*dataDir, llm)
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	if *compileInterval > 0 {
		handler.StartAutoCompile(context.Background(), *compileInterval)
	}

	log.Printf("engram9 listening on %s (data: %s)", *addr, *dataDir)
	if err := http.ListenAndServe(*addr, handler.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func runValidate(args []string) int {
	flags := flag.NewFlagSet("engram9 validate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	strict := flags.Bool("strict", false, "treat warnings as validation failure")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: engram9 validate [--strict] <bundle-dir>")
		return 2
	}

	result, err := okf.ValidateBundle(flags.Arg(0), *strict)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate: %v\n", err)
		return 2
	}
	for _, issue := range result.Issues {
		fmt.Fprintf(os.Stderr, "%s %s: %s\n", issue.Severity, issue.Path, issue.Message)
	}

	errors := result.ErrorCount()
	warnings := result.WarningCount()
	if errors > 0 || (*strict && warnings > 0) {
		if *strict && warnings > 0 {
			fmt.Fprintf(os.Stderr, "OKF validation failed: %d file(s), %d error(s), %d warning(s); strict mode treats warnings as failure\n", result.FilesChecked, errors, warnings)
		} else {
			fmt.Fprintf(os.Stderr, "OKF validation failed: %d file(s), %d error(s), %d warning(s)\n", result.FilesChecked, errors, warnings)
		}
		return 1
	}
	fmt.Fprintf(os.Stdout, "OKF validation passed: %d file(s), %d warning(s)\n", result.FilesChecked, warnings)
	return 0
}

func runMigrateOKF(args []string) int {
	flags := flag.NewFlagSet("engram9 migrate-okf", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	write := flags.Bool("write", false, "rewrite files in place")
	check := flags.Bool("check", false, "exit 1 if migration would change files")
	backup := flags.Bool("backup", true, "create .bak files before rewriting when --write is set")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: engram9 migrate-okf [--write] [--check] [--backup=false] <bundle-dir>")
		return 2
	}
	if *write && *check {
		fmt.Fprintln(os.Stderr, "migrate-okf: --write and --check cannot be used together")
		return 2
	}

	result, err := okf.MigrateLegacyBundle(flags.Arg(0), okf.MigrationOptions{Write: *write, Backup: *backup})
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate-okf: %v\n", err)
		return 2
	}
	for _, change := range result.Changes {
		if *write {
			fmt.Fprintf(os.Stdout, "migrated %s\n", change.Path)
		} else {
			fmt.Fprintf(os.Stdout, "would migrate %s\n", change.Path)
		}
	}
	if *write {
		fmt.Fprintf(os.Stdout, "OKF migration wrote: %d file(s), %d change(s)\n", result.FilesChecked, result.ChangedCount())
	} else {
		fmt.Fprintf(os.Stdout, "OKF migration dry-run: %d file(s), %d change(s)\n", result.FilesChecked, result.ChangedCount())
	}
	if *check && result.ChangedCount() > 0 {
		return 1
	}
	return 0
}

func runRepo(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: engram9 repo <scan|compile> [options]")
		return 2
	}
	switch args[0] {
	case "scan":
		return runRepoScan(args[1:])
	case "compile":
		return runRepoCompile(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown repo subcommand: %s\n", args[0])
		return 2
	}
}

func runRepoScan(args []string) int {
	flags := flag.NewFlagSet("engram9 repo scan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	repoPath := flags.String("path", ".", "git repository path")
	scope := flags.String("scope", ".", "repo-relative scope to scan")
	since := flags.String("since", "", "base commit for incremental scan")
	outDir := flags.String("out", "", "output directory; writes manifest.json, facts.jsonl, and snippets.jsonl (default: JSON to stdout)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: engram9 repo scan [--path <repo>] [--scope <repo-rel-path>] [--since <commit>] [--out <dir>]")
		return 2
	}
	bundle, err := repo.Scan(repo.ScanOptions{
		RepoPath: *repoPath,
		Scope:    *scope,
		Since:    *since,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "repo scan: %v\n", err)
		return 1
	}
	if err := repo.WriteBundle(bundle, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "repo scan: write output: %v\n", err)
		return 1
	}
	if *outDir != "" {
		fmt.Fprintf(os.Stdout, "repo scan wrote %d fact(s), %d file(s): %s\n", len(bundle.Facts), len(bundle.Manifest.Files), *outDir)
	}
	return 0
}

func runRepoCompile(args []string) int {
	flags := flag.NewFlagSet("engram9 repo compile", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	scanDir := flags.String("scan-dir", "", "directory containing facts.jsonl, snippets.jsonl, manifest.json from repo scan")
	outputDir := flags.String("output", "./repo-wiki", "output directory for compiled concept pages")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *scanDir == "" {
		fmt.Fprintln(os.Stderr, "error: --scan-dir is required")
		fmt.Fprintln(os.Stderr, "usage: engram9 repo compile --scan-dir <dir> [--output <dir>]")
		return 2
	}

	input, err := repo.LoadInput(*scanDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading scan data: %v\n", err)
		return 1
	}

	compiler := repo.NewCompiler(*outputDir)
	output, err := compiler.Compile(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error compiling: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stdout, "repo compile: %d concept pages generated in %s (commit: %.8s, scope: %s)\n",
		output.PageCount, *outputDir, output.Manifest.HeadSHA, output.Manifest.Scope)
	for _, page := range output.Pages {
		fmt.Fprintf(os.Stdout, "  %s — %s\n", page.Slug, page.Description)
	}
	return 0
}
