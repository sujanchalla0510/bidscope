// Package cli implements the BidScope command-line interface.
//
// The CLI is deliberately thin: flag parsing and exit codes live here so
// they are unit-testable. main.go only wires os.Args and the standard
// streams.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sujanchalla0510/bidscope/internal/ingest"
	"github.com/sujanchalla0510/bidscope/internal/signals"
	"github.com/sujanchalla0510/bidscope/internal/version"
)

// Config holds the parsed command-line options.
type Config struct {
	ShowVersion bool
	JSONOutput  bool
	Input       string
	Generate    bool
}

// Parse parses args (excluding the program name) into a Config.
// It returns flag.ErrHelp when -h/-help is requested.
func Parse(args []string) (Config, error) {
	var cfg Config
	fs := flag.NewFlagSet("bidscope", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // Run controls where usage goes.
	fs.BoolVar(&cfg.ShowVersion, "version", false, "print the BidScope version and exit")
	fs.BoolVar(&cfg.JSONOutput, "json", false, "emit machine-readable JSON output")
	fs.StringVar(&cfg.Input, "in", "", "path to a JSONL file of OpenRTB bid requests ('-' for stdin)")
	fs.BoolVar(&cfg.Generate, "generate", false, "emit a synthetic bidstream sample instead of reading input")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// usage writes the help text to w.
func usage(w io.Writer) {
	fmt.Fprintln(w, "bidscope — open-source bidstream profiler: score SSP supply quality from OpenRTB bid request samples.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  bidscope -in requests.jsonl       profile a JSONL sample of bid requests")
	fmt.Fprintln(w, "  bidscope -in requests.jsonl.gz    same, gzip-compressed")
	fmt.Fprintln(w, "  bidscope -generate                emit a synthetic bidstream sample (try it with no data)")
	fmt.Fprintln(w, "  bidscope -version                 print the version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -in string    input file ('-' reads stdin)")
	fmt.Fprintln(w, "  -generate     emit synthetic traffic instead of reading input")
	fmt.Fprintln(w, "  -json         machine-readable JSON output")
	fmt.Fprintln(w, "  -version      print the version and exit")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "https://github.com/sujanchalla0510/bidscope")
}

// Run executes the CLI, writing to stdout/stderr and returning the process
// exit code: 0 on success, 1 on runtime failure, 2 on flag misuse.
func Run(args []string, stdout, stderr io.Writer) int {
	cfg, err := Parse(args)
	switch {
	case err == nil:
		// continue below
	case errors.Is(err, flag.ErrHelp):
		usage(stdout)
		return 0
	default:
		usage(stderr)
		return 2
	}

	if cfg.ShowVersion {
		fmt.Fprintf(stdout, "bidscope v%s\n", version.Version)
		return 0
	}

	if cfg.Input != "" {
		return runProfile(cfg, stdout, stderr)
	}
	if cfg.Generate {
		// M7 synthetic generator: flag is parsed, engine not built yet.
		fmt.Fprintln(stderr, "bidscope: --generate lands in M7 (synthetic generator).")
		return 1
	}

	// No input and no --generate: nothing to do. Fail loudly (exit 1) so
	// scripts never mistake an empty run for a successful profile.
	fmt.Fprintln(stderr, "bidscope: no input — pass -in <file.jsonl> (or '-' for stdin).")
	usage(stderr)
	return 1
}

// profileResult is the M3 profiling summary: ingest accounting, the
// OpenRTB version mix, and the signal-completeness report with its
// composite signal score.
type profileResult struct {
	Input     string         `json:"input"`
	Lines     int            `json:"lines"`
	Parsed    int            `json:"parsed"`
	Blank     int            `json:"blank"`
	Malformed int            `json:"malformed"`
	Versions  map[string]int `json:"versions"`
	Signals   signals.Report `json:"signals"`
}

// runProfile streams the input through the ingest reader, folds every bid
// request into the signal-completeness engine, and reports the results.
// Exit 0 on success, 1 on read failure.
func runProfile(cfg Config, stdout, stderr io.Writer) int {
	s, err := ingest.Open(cfg.Input)
	if err != nil {
		fmt.Fprintf(stderr, "bidscope: %v\n", err)
		return 1
	}
	defer s.Close()

	eng := signals.NewEngine()
	for {
		br, err := s.Next()
		switch {
		case err == nil:
			eng.Add(br)
		case errors.Is(err, io.EOF):
			goto done
		case ingest.AsLineError(err):
			continue // malformed lines are skipped; Stats keeps count
		default:
			fmt.Fprintf(stderr, "bidscope: %v\n", err)
			return 1
		}
	}
done:
	st := s.Stats()
	res := profileResult{
		Input:     cfg.Input,
		Lines:     st.Lines,
		Parsed:    st.Parsed,
		Blank:     st.Blank,
		Malformed: st.Malformed,
		Versions:  s.VersionMix(),
		Signals:   eng.Report(),
	}

	if cfg.JSONOutput {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(stderr, "bidscope: encode output: %v\n", err)
			return 1
		}
		return 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "bidscope: parsed %d bid request(s) from %s", res.Parsed, res.Input)
	switch {
	case res.Malformed > 0 && res.Blank > 0:
		fmt.Fprintf(&b, " (%d malformed line(s) skipped, %d blank line(s) ignored)", res.Malformed, res.Blank)
	case res.Malformed > 0:
		fmt.Fprintf(&b, " (%d malformed line(s) skipped)", res.Malformed)
	case res.Blank > 0:
		fmt.Fprintf(&b, " (%d blank line(s) ignored)", res.Blank)
	}
	fmt.Fprintln(&b)
	keys := make([]string, 0, len(res.Versions))
	for k := range res.Versions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%d", k, res.Versions[k]))
	}
	fmt.Fprintf(&b, "OpenRTB versions: %s\n", strings.Join(pairs, ", "))

	fmt.Fprintln(&b, "\nSignal completeness:")
	fmt.Fprintln(&b, "  signal         present   fill")
	for _, sig := range res.Signals.Signals {
		fmt.Fprintf(&b, "  %-14s %4d/%-4d  %5.1f%%\n",
			sig.Label, sig.Present, sig.Total, sig.FillRate*100)
	}
	fmt.Fprintf(&b, "  signal score: %.1f/100\n", res.Signals.Score)
	fmt.Fprint(stdout, b.String())
	return 0
}
