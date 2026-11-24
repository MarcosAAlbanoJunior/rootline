// Command rootline analyzes recorded traces against a git repository (detectors-only mode).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/goindex"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/otlpfile"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/planfile"
	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `usage: rootline analyze --traces <file.jsonl> --repo <dir> --service <name> --operation "<METHOD> <route>" --bad-version <sha> [--baseline-version <sha>] [--plans <plans.jsonl>]`

// run executes the CLI and returns the exit code: 0 ok, 1 runtime failure, 2 usage error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "analyze" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	return analyze(args[1:], stdout, stderr)
}

type fileSource []trace.Span

func (f fileSource) Spans() ([]trace.Span, error) { return f, nil }

func analyze(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tracesPath := fs.String("traces", "", "OTLP JSON lines file")
	repo := fs.String("repo", "", "git repository directory")
	service := fs.String("service", "", "service name")
	operation := fs.String("operation", "", `entry operation, "<METHOD> <route>"`)
	bad := fs.String("bad-version", "", "SHA of the bad version")
	baseline := fs.String("baseline-version", "", "SHA of the reference version")
	plansPath := fs.String("plans", "", "recorded execution plans (plans.jsonl); without it the missing-index detector does not run")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "rootline: %v\n%s\n", err, usage)
		return 2
	}
	for name, v := range map[string]string{"--traces": *tracesPath, "--repo": *repo, "--service": *service,
		"--operation": *operation, "--bad-version": *bad} {
		if v == "" {
			fmt.Fprintf(stderr, "rootline: missing required argument %s\n%s\n", name, usage)
			return 2
		}
	}
	if err := gitcli.ValidateSHA(*bad); err != nil {
		fmt.Fprintf(stderr, "rootline: --bad-version: %v\n", err)
		return 2
	}
	if *baseline != "" {
		if err := gitcli.ValidateSHA(*baseline); err != nil {
			fmt.Fprintf(stderr, "rootline: --baseline-version: %v\n", err)
			return 2
		}
	}
	op, ok := trace.OperationOf(trace.Span{Kind: trace.KindServer, Name: *operation})
	if !ok {
		fmt.Fprintf(stderr, "rootline: --operation %q must be \"<METHOD> /<route>\"\n", *operation)
		return 2
	}

	op.RouteSource = "" // unknown until the traces are read; the analysis fills in the real source

	spans, err := otlpfile.ReadFile(*tracesPath)
	if err != nil {
		fmt.Fprintf(stderr, "rootline: %v\n", err)
		return 1
	}
	var plans analysis.Explainer
	if *plansPath != "" {
		p, err := planfile.ReadFile(*plansPath)
		if err != nil {
			fmt.Fprintf(stderr, "rootline: %v\n", err)
			return 1
		}
		plans = p
	}
	git := gitcli.New(*repo)
	res, err := analysis.Analyze(fileSource(spans), goindex.New(git), git, plans, analysis.Symptom{
		Service: *service, Operation: op, BadVersion: *bad, BaselineVersion: *baseline,
	})
	if err != nil {
		fmt.Fprintf(stderr, "rootline: %v\n", err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintf(stderr, "rootline: %v\n", err)
		return 1
	}
	return 0
}
