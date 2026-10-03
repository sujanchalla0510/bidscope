// Package report turns profiling results into human-readable output: a
// plain-language summary paragraph and a self-contained HTML report.
//
// The HTML is a single file with inline CSS and no external dependencies,
// so it can be opened from disk or attached to a vendor evaluation email
// without a server. All embedded data goes through html/template, so
// inventory domains or user agents from the sample can never break out
// of the markup.
package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sujanchalla0510/bidscope/internal/biddable"
	"github.com/sujanchalla0510/bidscope/internal/mix"
	"github.com/sujanchalla0510/bidscope/internal/quality"
	"github.com/sujanchalla0510/bidscope/internal/signals"
)

// Data is everything a report renders.
type Data struct {
	Input     string
	Lines     int
	Parsed    int
	Blank     int
	Malformed int
	Versions  map[string]int
	Signals   signals.Report
	Mix       mix.Report
	Quality   quality.Report
	Biddable  biddable.Report
	Version   string // bidscope version that produced the report
}

// Summary writes a plain-language executive summary of the profile into b:
// the money line first (what share of the stream is biddable), then signal
// health, mix character, and the loudest quality warnings.
func Summary(d Data, b *strings.Builder) {
	fmt.Fprintf(b, "BidScope verdict: %d of %d sampled requests (%.1f%%) are biddable",
		d.Biddable.Biddable, d.Biddable.Requests, d.Biddable.BiddableShare*100)
	if d.Biddable.InputQPS > 0 {
		fmt.Fprintf(b, " — at a stated %.0f QPS that is ~%.0f biddable QPS",
			d.Biddable.InputQPS, d.Biddable.BiddableQPS)
	}
	fmt.Fprintln(b, ".")

	fmt.Fprintf(b, "Signal completeness scores %.1f/100", d.Signals.Score)
	weak := weakestSignals(d.Signals, 3)
	if len(weak) > 0 {
		fmt.Fprintf(b, "; weakest signals: %s", strings.Join(weak, ", "))
	}
	fmt.Fprintln(b, ".")

	mx := d.Mix
	fmt.Fprintf(b, "Inventory is %.0f%% site / %.0f%% app", mx.SiteShare*100, mx.AppShare*100)
	if len(mx.TopCountries) > 0 {
		top := mx.TopCountries[0]
		fmt.Fprintf(b, ", led by %s (%.0f%%)", top.Label, top.Share*100)
	}
	if mx.PMPCoverage > 0 {
		fmt.Fprintf(b, "; %.0f%% of impressions carry PMP", mx.PMPCoverage*100)
	}
	fmt.Fprintln(b, ".")

	q := d.Quality
	var warns []string
	if q.ExactDuplicates > 0 {
		warns = append(warns, fmt.Sprintf("%d exact-duplicate replays (%.1f%% of requests)",
			q.ExactDuplicates, float64(q.ExactDuplicates)/float64(max1(q.Requests))*100))
	}
	if q.DatacenterIPShare > 0.05 {
		warns = append(warns, fmt.Sprintf("%.1f%% datacenter IPs", q.DatacenterIPShare*100))
	}
	for _, f := range q.RedFlags {
		if f.Severity == quality.SeverityHigh {
			warns = append(warns, "red flag: "+f.Code)
		}
	}
	if len(warns) == 0 {
		fmt.Fprintln(b, "No major quality warnings in the sample.")
	} else {
		fmt.Fprintf(b, "Quality warnings: %s.\n", strings.Join(warns, "; "))
	}

	// Where the non-biddable QPS goes, worst gate first.
	type gf struct {
		label string
		rate  float64
	}
	var gates []gf
	for _, g := range d.Biddable.Gates {
		if g.Failed > 0 {
			gates = append(gates, gf{g.Label, g.FailRate})
		}
	}
	sort.Slice(gates, func(i, j int) bool { return gates[i].rate > gates[j].rate })
	if len(gates) > 0 {
		var parts []string
		for _, g := range gates {
			parts = append(parts, fmt.Sprintf("%s (%.1f%%)", g.label, g.rate*100))
		}
		fmt.Fprintf(b, "Biddability is lost to: %s.\n", strings.Join(parts, ", "))
	}
}

// weakestSignals returns "label 12.3%" strings for the n lowest fill-rate
// signals, skipping signals at 100%.
func weakestSignals(r signals.Report, n int) []string {
	type s struct {
		label string
		fill  float64
	}
	var all []s
	for _, sg := range r.Signals {
		if sg.FillRate < 1 {
			all = append(all, s{sg.Label, sg.FillRate})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].fill < all[j].fill })
	if len(all) > n {
		all = all[:n]
	}
	out := make([]string, 0, len(all))
	for _, x := range all {
		out = append(out, fmt.Sprintf("%s %.1f%%", x.label, x.fill*100))
	}
	return out
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
