// Package cli implements the BidScope command-line interface.
//
// The CLI is deliberately thin: flag parsing and exit codes live here so
// they are unit-testable. main.go only wires os.Args and the standard
// streams.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

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

	// M1 scaffold: the ingest + analysis engine lands in M2. Fail loudly
	// (exit 1) so scripts never mistake a stub run for a real profile.
	fmt.Fprintln(stderr, "bidscope: the profiling engine is not implemented yet (lands in M2 — ingest + parsing).")
	fmt.Fprintln(stderr, "Run 'bidscope -version' to verify the install, or see the roadmap at https://github.com/sujanchalla0510/bidscope.")
	return 1
}
