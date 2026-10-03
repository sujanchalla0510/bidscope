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
	"os"
	"sort"
	"strings"

	"github.com/sujanchalla0510/bidscope/internal/biddable"
	"github.com/sujanchalla0510/bidscope/internal/ingest"
	"github.com/sujanchalla0510/bidscope/internal/mix"
	"github.com/sujanchalla0510/bidscope/internal/quality"
	"github.com/sujanchalla0510/bidscope/internal/report"
	"github.com/sujanchalla0510/bidscope/internal/signals"
	"github.com/sujanchalla0510/bidscope/internal/version"
)

// Config holds the parsed command-line options.
type Config struct {
	ShowVersion bool
	JSONOutput  bool
	Input       string
	Generate    bool
	InputQPS    float64
	HTMLPath    string
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
	fs.Float64Var(&cfg.InputQPS, "qps", 0, "stated input QPS the sample was drawn from (scales the biddable estimate)")
	fs.StringVar(&cfg.HTMLPath, "html", "", "write a self-contained HTML report to this path")
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
	fmt.Fprintln(w, "  -qps float    stated input QPS the sample was drawn from (scales the biddable estimate)")
	fmt.Fprintln(w, "  -html string  write a self-contained HTML report to this path")
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

// profileResult is the M3+M4+M5+M6 profiling summary: ingest accounting,
// the OpenRTB version mix, the signal-completeness report with its
// composite signal score, the inventory mix analysis, the quality-signal
// report, and the biddable-QPS estimate.
type profileResult struct {
	Input     string          `json:"input"`
	Lines     int             `json:"lines"`
	Parsed    int             `json:"parsed"`
	Blank     int             `json:"blank"`
	Malformed int             `json:"malformed"`
	Versions  map[string]int  `json:"versions"`
	Signals   signals.Report  `json:"signals"`
	Mix       mix.Report      `json:"mix"`
	Quality   quality.Report  `json:"quality"`
	Biddable  biddable.Report `json:"biddable"`
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
	mixEng := mix.NewEngine()
	qEng := quality.NewEngine()
	bEng := biddable.NewEngine()
	for {
		br, err := s.Next()
		switch {
		case err == nil:
			eng.Add(br)
			mixEng.Add(br)
			qEng.Add(br)
			bEng.Add(br)
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
		Mix:       mixEng.Report(),
		Quality:   qEng.Report(),
		Biddable:  bEng.Report(cfg.InputQPS),
	}

	var b strings.Builder
	if cfg.JSONOutput {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(stderr, "bidscope: encode output: %v\n", err)
			return 1
		}
	} else {
		writeTextReport(res, &b)
	}

	if cfg.HTMLPath != "" {
		if err := writeHTMLReport(cfg.HTMLPath, res); err != nil {
			fmt.Fprintf(stderr, "bidscope: write HTML report: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "bidscope: HTML report written to %s\n", cfg.HTMLPath)
	}
	fmt.Fprint(stdout, b.String())
	return 0
}

// writeTextReport renders the human-readable profile into b.
func writeTextReport(res profileResult, b *strings.Builder) {
	fmt.Fprintf(b, "bidscope: parsed %d bid request(s) from %s", res.Parsed, res.Input)
	switch {
	case res.Malformed > 0 && res.Blank > 0:
		fmt.Fprintf(b, " (%d malformed line(s) skipped, %d blank line(s) ignored)", res.Malformed, res.Blank)
	case res.Malformed > 0:
		fmt.Fprintf(b, " (%d malformed line(s) skipped)", res.Malformed)
	case res.Blank > 0:
		fmt.Fprintf(b, " (%d blank line(s) ignored)", res.Blank)
	}
	fmt.Fprintln(b)
	keys := make([]string, 0, len(res.Versions))
	for k := range res.Versions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%d", k, res.Versions[k]))
	}
	fmt.Fprintf(b, "OpenRTB versions: %s\n", strings.Join(pairs, ", "))

	fmt.Fprintln(b, "\nSignal completeness:")
	fmt.Fprintln(b, "  signal         present   fill")
	for _, sig := range res.Signals.Signals {
		fmt.Fprintf(b, "  %-14s %4d/%-4d  %5.1f%%\n",
			sig.Label, sig.Present, sig.Total, sig.FillRate*100)
	}
	fmt.Fprintf(b, "  signal score: %.1f/100\n", res.Signals.Score)

	mx := res.Mix
	fmt.Fprintf(b, "\nMix analysis (%d request(s), %d impression(s)):\n", mx.Requests, mx.Impressions)
	fmt.Fprintf(b, "  inventory: %d site (%.1f%%), %d app (%.1f%%)\n",
		mx.Site, mx.SiteShare*100, mx.App, mx.AppShare*100)
	fmt.Fprintf(b, "  formats:   %s\n", joinItems(mx.Formats))
	fmt.Fprintf(b, "  top os:    %s\n", joinItems(mx.TopOS))
	fmt.Fprintf(b, "  top geo:   %s\n", joinItems(mx.TopCountries))
	fmt.Fprintf(b, "  devices:   %s\n", joinItems(mx.DeviceTypes))
	fmt.Fprintf(b, "  pmp: %.1f%% of imps carry pmp, %.1f%% carry deals, %.1f%% private auction\n",
		mx.PMPCoverage*100, mx.DealCoverage*100, mx.PrivateShare*100)
	if mx.BidFloors > 0 {
		fmt.Fprintf(b, "  bid floors (n=%d): median %.2f, p90 %.2f\n",
			mx.BidFloors, mx.FloorMedian, mx.FloorP90)
	} else {
		fmt.Fprintln(b, "  bid floors: none set")
	}

	q := res.Quality
	fmt.Fprintln(b, "\nQuality signals:")
	fmt.Fprintf(b, "  duplicates: %d exact (%d group(s)), %d near-duplicate (%d group(s)), %d reused request id(s)\n",
		q.ExactDuplicates, q.ExactDuplicateGroups, q.NearDuplicates, q.NearDuplicateGroups, q.DuplicateIDs)
	fmt.Fprintf(b, "  datacenter IPs: %d (%.1f%%)\n", q.DatacenterIPs, q.DatacenterIPShare*100)
	fmt.Fprintf(b, "  user agents: %d missing, %d suspicious, %d ua/os mismatch; top UA share %.1f%%\n",
		q.MissingUA, q.SuspiciousUA, q.UAOSMismatch, q.TopUAShare*100)
	if q.TMaxDistinct > 0 {
		fmt.Fprintf(b, "  tmax: %d unset, median %.0f ms (%d distinct)\n",
			q.TMaxUnset, q.TMaxMedian, q.TMaxDistinct)
	} else {
		fmt.Fprintf(b, "  tmax: %d unset (no timeouts set)\n", q.TMaxUnset)
	}
	if len(q.RedFlags) == 0 {
		fmt.Fprintln(b, "  red flags: none")
	} else {
		fmt.Fprintln(b, "  red flags:")
		for _, f := range q.RedFlags {
			fmt.Fprintf(b, "    [%s] %s: %s\n", f.Severity, f.Code, f.Detail)
		}
	}
	bd := res.Biddable
	fmt.Fprintln(b, "\nBiddable QPS:")
	fmt.Fprintf(b, "  biddable: %d/%d requests pass every gate (%.1f%%)\n",
		bd.Biddable, bd.Requests, bd.BiddableShare*100)
	for _, g := range bd.Gates {
		fmt.Fprintf(b, "    %-24s %6d failed (%5.1f%%)\n", g.Label, g.Failed, g.FailRate*100)
	}
	if bd.InputQPS > 0 {
		fmt.Fprintf(b, "  at %.0f QPS in: ~%.0f biddable QPS\n", bd.InputQPS, bd.BiddableQPS)
	} else {
		fmt.Fprintln(b, "  (pass -qps with the SSP's stated QPS to scale this share into biddable QPS)")
	}

	fmt.Fprintln(b)
	report.Summary(reportData(res), b)
}

// reportData converts a profile result into the report package's input.
func reportData(res profileResult) report.Data {
	return report.Data{
		Input:     res.Input,
		Lines:     res.Lines,
		Parsed:    res.Parsed,
		Blank:     res.Blank,
		Malformed: res.Malformed,
		Versions:  res.Versions,
		Signals:   res.Signals,
		Mix:       res.Mix,
		Quality:   res.Quality,
		Biddable:  res.Biddable,
		Version:   version.Version,
	}
}

// writeHTMLReport renders the full profile as a self-contained HTML file.
func writeHTMLReport(path string, res profileResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return report.WriteHTML(f, reportData(res))
}

// table renders as "—" so missing attributes read as missing, not zero.
func joinItems(items []mix.Item) string {
	if len(items) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s %.1f%% (%d)", it.Label, it.Share*100, it.Count))
	}
	return strings.Join(parts, ", ")
}
