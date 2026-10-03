package report

import (
	"fmt"
	"html/template"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/sujanchalla0510/bidscope/internal/biddable"
	"github.com/sujanchalla0510/bidscope/internal/mix"
	"github.com/sujanchalla0510/bidscope/internal/quality"
	"github.com/sujanchalla0510/bidscope/internal/signals"
)

// WriteHTML writes a self-contained HTML profiling report to w: inline CSS,
// no scripts, no external assets. All sample-derived strings pass through
// html/template escaping.
func WriteHTML(w io.Writer, d Data) error {
	var sum strings.Builder
	Summary(d, &sum)
	v := struct {
		Title     string
		Input     string
		Version   string
		Summary   string
		Lines     int
		Parsed    int
		Blank     int
		Malformed int
		Versions  []versionItem
		Score     float64
		Signals   []signals.SignalResult
		Mix       mix.Report
		Quality   quality.Report
		Biddable  biddable.Report
	}{
		Title:     "BidScope supply profile — " + displayName(d.Input),
		Input:     d.Input,
		Version:   d.Version,
		Summary:   sum.String(),
		Lines:     d.Lines,
		Parsed:    d.Parsed,
		Blank:     d.Blank,
		Malformed: d.Malformed,
		Versions:  sortedVersions(d.Versions),
		Score:     d.Signals.Score,
		Signals:   d.Signals.Signals,
		Mix:       d.Mix,
		Quality:   d.Quality,
		Biddable:  d.Biddable,
	}
	return htmlTmpl.Execute(w, v)
}

// displayName shortens an input path to its final element for the title.
func displayName(path string) string {
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// versionItem is one OpenRTB version row.
type versionItem struct {
	Version string
	Count   int
}

func sortedVersions(m map[string]int) []versionItem {
	out := make([]versionItem, 0, len(m))
	for k, c := range m {
		out = append(out, versionItem{k, c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// commas renders an integer with thousands separators.
func commas(n int) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

var htmlTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"pct":    func(f float64) string { return fmt.Sprintf("%.1f%%", f*100) },
	"width":  func(f float64) string { return fmt.Sprintf("%.1f", f*100) },
	"commas": commas,
	"round":  func(f float64) int { return int(math.Round(f)) },
	"splitLines": func(s string) []string {
		var out []string
		for _, ln := range strings.Split(s, "\n") {
			if strings.TrimSpace(ln) != "" {
				out = append(out, ln)
			}
		}
		return out
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
  :root { --ink:#1a1d21; --muted:#5b636e; --line:#e3e6ea; --accent:#1f6feb; --warn:#b54708; --bad:#a40e26; --ok:#1a7f37; }
  body { font-family:-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif; color:var(--ink); max-width:960px; margin:2rem auto; padding:0 1.25rem; line-height:1.5; }
  h1 { font-size:1.6rem; margin:0 0 .25rem; }
  h2 { font-size:1.15rem; border-bottom:1px solid var(--line); padding-bottom:.35rem; margin:2rem 0 .75rem; }
  .meta { color:var(--muted); font-size:.9rem; margin-bottom:1rem; }
  .verdict { background:#f6f8fa; border:1px solid var(--line); border-left:4px solid var(--accent); border-radius:6px; padding:.9rem 1.1rem; }
  .verdict p { margin:.35rem 0; }
  .big { font-size:2rem; font-weight:700; }
  .big small { font-size:1rem; font-weight:400; color:var(--muted); }
  table { border-collapse:collapse; width:100%; margin:.5rem 0; font-size:.92rem; }
  th, td { text-align:left; padding:.4rem .6rem; border-bottom:1px solid var(--line); }
  th { color:var(--muted); font-weight:600; text-transform:uppercase; font-size:.75rem; letter-spacing:.04em; }
  td.num, th.num { text-align:right; font-variant-numeric:tabular-nums; }
  .bar { background:var(--accent); height:.65rem; border-radius:3px; min-width:2px; }
  .bartrack { background:#eef1f4; border-radius:3px; width:100%; }
  .sevwarn { color:var(--warn); font-weight:700; } .sevhigh { color:var(--bad); font-weight:700; }
  code { background:#f6f8fa; padding:.1rem .35rem; border-radius:4px; font-size:.85em; }
  .foot { color:var(--muted); font-size:.8rem; margin-top:2.5rem; border-top:1px solid var(--line); padding-top:.75rem; }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<div class="meta">profiled <code>{{.Input}}</code> &middot; {{commas .Parsed}} bid requests parsed{{if .Malformed}} ({{commas .Malformed}} malformed skipped){{end}}{{if .Blank}} ({{commas .Blank}} blank ignored){{end}} &middot; OpenRTB {{range $i, $v := .Versions}}{{if $i}}, {{end}}{{$v.Version}} ({{commas $v.Count}}){{end}}</div>

<div class="verdict">
  <div class="big">{{pct .Biddable.BiddableShare}} <small>biddable</small></div>
  {{range $p := splitLines .Summary}}<p>{{$p}}</p>{{end}}
</div>

<h2>Signal completeness <span class="meta">score {{printf "%.1f" .Score}}/100</span></h2>
<table>
<tr><th>signal</th><th class="num">present</th><th class="num">fill</th><th style="width:40%">bar</th></tr>
{{range .Signals}}<tr><td><code>{{.Label}}</code></td><td class="num">{{commas .Present}}/{{commas .Total}}</td><td class="num">{{pct .FillRate}}</td><td><div class="bartrack"><div class="bar" style="width:{{width .FillRate}}%"></div></div></td></tr>{{end}}
</table>

<h2>Mix analysis</h2>
<table>
<tr><th>dimension</th><th>composition</th></tr>
<tr><td>inventory</td><td>{{commas .Mix.Site}} site ({{pct .Mix.SiteShare}}) &middot; {{commas .Mix.App}} app ({{pct .Mix.AppShare}}){{if .Mix.Neither}} &middot; {{commas .Mix.Neither}} neither{{end}}</td></tr>
<tr><td>formats</td><td>{{range $i, $f := .Mix.Formats}}{{if $i}}, {{end}}{{$f.Label}} {{pct $f.Share}}{{end}}</td></tr>
<tr><td>top OS</td><td>{{range $i, $f := .Mix.TopOS}}{{if $i}}, {{end}}{{$f.Label}} {{pct $f.Share}}{{end}}</td></tr>
<tr><td>top geo</td><td>{{range $i, $f := .Mix.TopCountries}}{{if $i}}, {{end}}{{$f.Label}} {{pct $f.Share}}{{end}}</td></tr>
<tr><td>devices</td><td>{{range $i, $f := .Mix.DeviceTypes}}{{if $i}}, {{end}}{{$f.Label}} {{pct $f.Share}}{{end}}</td></tr>
<tr><td>PMP</td><td>{{pct .Mix.PMPCoverage}} of impressions carry PMP &middot; {{pct .Mix.DealCoverage}} carry deals &middot; {{pct .Mix.PrivateShare}} private auction</td></tr>
<tr><td>bid floors</td><td>{{if gt .Mix.BidFloors 0}}n={{commas .Mix.BidFloors}}, median {{printf "%.2f" .Mix.FloorMedian}}, p90 {{printf "%.2f" .Mix.FloorP90}}{{else}}none set{{end}}</td></tr>
</table>

<h2>Quality signals</h2>
<table>
<tr><th>check</th><th class="num">result</th></tr>
<tr><td>exact duplicates</td><td class="num">{{commas .Quality.ExactDuplicates}} ({{commas .Quality.ExactDuplicateGroups}} groups)</td></tr>
<tr><td>near duplicates</td><td class="num">{{commas .Quality.NearDuplicates}} ({{commas .Quality.NearDuplicateGroups}} groups)</td></tr>
<tr><td>reused request ids</td><td class="num">{{commas .Quality.DuplicateIDs}}</td></tr>
<tr><td>datacenter IPs</td><td class="num">{{commas .Quality.DatacenterIPs}} ({{pct .Quality.DatacenterIPShare}})</td></tr>
<tr><td>user agents</td><td class="num">{{commas .Quality.MissingUA}} missing &middot; {{commas .Quality.SuspiciousUA}} suspicious &middot; {{commas .Quality.UAOSMismatch}} ua/os mismatch &middot; top UA {{pct .Quality.TopUAShare}}</td></tr>
<tr><td>tmax</td><td class="num">{{commas .Quality.TMaxUnset}} unset{{if gt .Quality.TMaxDistinct 0}} &middot; median {{printf "%.0f" .Quality.TMaxMedian}} ms ({{commas .Quality.TMaxDistinct}} distinct){{end}}</td></tr>
</table>
{{if .Quality.RedFlags}}
<h2>Red flags</h2>
<table>
<tr><th>severity</th><th>code</th><th>detail</th></tr>
{{range .Quality.RedFlags}}<tr><td class="sev{{.Severity}}">{{.Severity}}</td><td><code>{{.Code}}</code></td><td>{{.Detail}}</td></tr>{{end}}
</table>
{{end}}

<h2>Biddable QPS</h2>
<table>
<tr><th>gate</th><th class="num">failed</th><th class="num">fail rate</th><th style="width:35%">bar</th></tr>
{{range .Biddable.Gates}}<tr><td>{{.Label}}</td><td class="num">{{commas .Failed}}</td><td class="num">{{pct .FailRate}}</td><td><div class="bartrack"><div class="bar" style="width:{{width .FailRate}}%"></div></div></td></tr>{{end}}
</table>
<p><strong>{{commas .Biddable.Biddable}} of {{commas .Biddable.Requests}} requests pass every gate ({{pct .Biddable.BiddableShare}}).</strong>
{{if gt .Biddable.InputQPS 0.0}}At a stated {{printf "%.0f" .Biddable.InputQPS}} QPS, that is <strong>~{{commas (round .Biddable.BiddableQPS)}} biddable QPS</strong>.{{else}}Pass <code>-qps</code> with the SSP's stated QPS to scale this share into an absolute biddable-QPS figure.{{end}}</p>

<div class="foot">generated by bidscope v{{.Version}} &middot; local-first: this report was computed entirely on your machine, nothing was uploaded &middot; <a href="https://github.com/sujanchalla0510/bidscope">github.com/sujanchalla0510/bidscope</a></div>
</body>
</html>`))
