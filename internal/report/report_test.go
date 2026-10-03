package report

import (
	"strings"
	"testing"

	"github.com/sujanchalla0510/bidscope/internal/biddable"
	"github.com/sujanchalla0510/bidscope/internal/mix"
	"github.com/sujanchalla0510/bidscope/internal/openrtb"
	"github.com/sujanchalla0510/bidscope/internal/quality"
	"github.com/sujanchalla0510/bidscope/internal/signals"
)

// sampleData folds a handful of synthetic requests through every engine.
func sampleData(t *testing.T, inputQPS float64) Data {
	t.Helper()
	reqs := []*openrtb.BidRequest{
		{
			ID:  "r1",
			Imp: []openrtb.Imp{{ID: "i1", Banner: &openrtb.Banner{}}},
			Device: &openrtb.Device{
				OS:  "android",
				IP:  "198.51.100.23",
				IFA: "ifa-1",
				UA:  "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36",
				Geo: &openrtb.Geo{Country: "USA"},
			},
			User: &openrtb.User{ID: "u1"},
			Site: &openrtb.Site{Domain: "synthetic.test"},
		},
		{
			ID:  "r2",
			Imp: []openrtb.Imp{{ID: "i1", Video: &openrtb.Video{}}},
			Device: &openrtb.Device{
				OS: "ios",
				IP: "203.0.113.7",
				UA: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			},
			App: &openrtb.App{Bundle: "com.synthetic.app"},
		},
	}
	se, me, qe, be := signals.NewEngine(), mix.NewEngine(), quality.NewEngine(), biddable.NewEngine()
	for _, br := range reqs {
		se.Add(br)
		me.Add(br)
		qe.Add(br)
		be.Add(br)
	}
	return Data{
		Input:    "sample.jsonl",
		Lines:    2,
		Parsed:   2,
		Versions: map[string]int{"2.5": 2},
		Signals:  se.Report(),
		Mix:      me.Report(),
		Quality:  qe.Report(),
		Biddable: be.Report(inputQPS),
		Version:  "test",
	}
}

func TestSummaryMentionsBiddableShare(t *testing.T) {
	d := sampleData(t, 0)
	var b strings.Builder
	Summary(d, &b)
	s := b.String()
	if !strings.Contains(s, "BidScope verdict") {
		t.Fatalf("summary missing verdict line: %q", s)
	}
	if !strings.Contains(s, "biddable") {
		t.Fatalf("summary missing biddability: %q", s)
	}
	// r1 is fully biddable, r2 fails identity (no ifa/user.id/ipv6... has IP
	// though) — at minimum the summary must name where QPS is lost.
	if !strings.Contains(s, "Biddability is lost to") {
		t.Fatalf("summary missing loss breakdown: %q", s)
	}
}

func TestSummaryScalesQPS(t *testing.T) {
	d := sampleData(t, 100000)
	var b strings.Builder
	Summary(d, &b)
	if !strings.Contains(b.String(), "biddable QPS") {
		t.Fatalf("summary missing QPS scaling: %q", b.String())
	}
}

func TestHTMLRendersAndEscapes(t *testing.T) {
	d := sampleData(t, 50000)
	// Poison the input name: it must come out escaped, not as markup.
	d.Input = `evil"><script>alert(1)</script>.jsonl`
	var b strings.Builder
	if err := WriteHTML(&b, d); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	html := b.String()
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatalf("HTML output not escaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag in output")
	}
	for _, want := range []string{
		"Signal completeness", "Mix analysis", "Quality signals", "Biddable QPS",
		"biddable QPS", // the absolute figure line
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML missing section %q", want)
		}
	}
}

func TestHTMLZeroRequests(t *testing.T) {
	d := Data{Input: "empty.jsonl", Versions: map[string]int{}, Version: "test"}
	d.Signals = signals.NewEngine().Report()
	d.Mix = mix.NewEngine().Report()
	d.Quality = quality.NewEngine().Report()
	d.Biddable = biddable.NewEngine().Report(0)
	var b strings.Builder
	if err := WriteHTML(&b, d); err != nil {
		t.Fatalf("WriteHTML on empty data: %v", err)
	}
	if !strings.Contains(b.String(), "0.0%") {
		t.Fatalf("empty HTML should show 0.0%% biddable")
	}
}

func TestCommas(t *testing.T) {
	for in, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -42: "-42"} {
		if got := commas(in); got != want {
			t.Fatalf("commas(%d) = %q, want %q", in, got, want)
		}
	}
}
