package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/otlpfile"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/textreport"
	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
)

// symptomsOutput is the JSON form of the symptoms command: the report plus the ready command.
type symptomsOutput struct {
	analysis.SymptomsReport
	AnalyzeCommand string `json:"analyze_command,omitempty"`
}

func symptoms(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("symptoms", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tracesPath := fs.String("traces", "", "OTLP JSON lines file")
	service := fs.String("service", "", "service name; inferred when the file has only one")
	repo := fs.String("repo", "", "git repository directory, to order the versions by its history")
	baseline := fs.String("baseline-version", "", "version of the reference")
	bad := fs.String("bad-version", "", "version that regressed")
	format := fs.String("format", formatText, "output format: text (Markdown, the default) or json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "rootline: %v\n%s\n", err, usage)
		return 2
	}
	if *tracesPath == "" {
		fmt.Fprintf(stderr, "rootline: missing required argument --traces\n%s\n", usage)
		return 2
	}
	if *format != formatText && *format != formatJSON {
		fmt.Fprintf(stderr, "rootline: --format must be text or json, got %q\n", *format)
		return 2
	}

	spans, err := otlpfile.ReadFile(*tracesPath)
	if err != nil {
		fmt.Fprintf(stderr, "rootline: %v\n", err)
		return 1
	}
	var history analysis.History
	if *repo != "" {
		history = gitcli.New(*repo)
	}
	rep, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{Service: *service, BaselineVersion: *baseline, BadVersion: *bad}, history)
	if err != nil {
		var svc *analysis.ServiceAmbiguityError
		var ver *analysis.VersionAmbiguityError
		if errors.As(err, &svc) || errors.As(err, &ver) {
			fmt.Fprintf(stderr, "rootline: %v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "rootline: %v\n", err)
		return 1
	}

	out := symptomsOutput{SymptomsReport: rep}
	if rep.Suggested != nil {
		out.AnalyzeCommand = analyzeCommand(*tracesPath, *repo, rep)
	}
	if *format == formatJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(out)
	} else {
		err = textreport.RenderSymptoms(stdout, rep, out.AnalyzeCommand)
	}
	if err != nil {
		fmt.Fprintf(stderr, "rootline: %v\n", err)
		return 1
	}
	return 0
}

// analyzeCommand is the analyze invocation for the suggested operation, ready to paste in a POSIX
// shell. Without --repo it leaves a placeholder, since analyze needs the repository.
func analyzeCommand(tracesPath, repo string, r analysis.SymptomsReport) string {
	repoArg := "<repo-dir>"
	if repo != "" {
		repoArg = shellQuote(repo)
	}
	return strings.Join([]string{
		"rootline analyze",
		"--traces " + shellQuote(tracesPath),
		"--repo " + repoArg,
		"--service " + shellQuote(r.Service),
		"--operation " + shellQuote(r.Suggested.Method+" "+r.Suggested.Route),
		"--baseline-version " + shellQuote(r.BaselineVersion),
		"--bad-version " + shellQuote(r.BadVersion),
	}, " ")
}

// shellQuote quotes s for a POSIX shell when it has anything beyond a safe set of characters.
func shellQuote(s string) string {
	safe := func(c rune) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_./:=@,+", c)
	}
	if s != "" && strings.IndexFunc(s, func(c rune) bool { return !safe(c) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
