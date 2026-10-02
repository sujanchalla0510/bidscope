package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runForTest(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersionFlag(t *testing.T) {
	code, stdout, _ := runForTest(t, "-version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "bidscope v") {
		t.Fatalf("stdout = %q, want 'bidscope v...' prefix", stdout)
	}
}

func TestHelpFlag(t *testing.T) {
	for _, f := range []string{"-h", "-help"} {
		code, stdout, _ := runForTest(t, f)
		if code != 0 {
			t.Fatalf("%s: exit code = %d, want 0", f, code)
		}
		if !strings.Contains(stdout, "Usage:") {
			t.Fatalf("%s: stdout missing usage text: %q", f, stdout)
		}
	}
}

func TestUnknownFlag(t *testing.T) {
	code, _, stderr := runForTest(t, "-bogus")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("stderr missing usage text: %q", stderr)
	}
}

func TestProfileInput(t *testing.T) {
	// M2 ingest lands: -in streams the fixture through the real reader.
	code, stdout, _ := runForTest(t, "-in", "../ingest/testdata/sample.jsonl")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "parsed 4 bid request(s)") {
		t.Fatalf("stdout = %q, want parse summary", stdout)
	}
	if !strings.Contains(stdout, "OpenRTB versions: 2.5=3, 2.6=1") {
		t.Fatalf("stdout = %q, want version mix", stdout)
	}
	if !strings.Contains(stdout, "1 malformed line(s) skipped") {
		t.Fatalf("stdout = %q, want malformed accounting", stdout)
	}
	// M3 signal completeness: table + composite score on the sample.
	// Fixture fill rates: os 3/4, ip 1/4, user.id 1/4, inventory.id 3/4,
	// geo 0, consent 1/4, schain 1/4, eids 0 -> score 31.3.
	if !strings.Contains(stdout, "Signal completeness:") {
		t.Fatalf("stdout = %q, want signal table", stdout)
	}
	if !strings.Contains(stdout, "signal score: 31.3/100") {
		t.Fatalf("stdout = %q, want signal score 31.3", stdout)
	}
	if !strings.Contains(stdout, "device.os") || !strings.Contains(stdout, "75.0%") {
		t.Fatalf("stdout = %q, want device.os row at 75.0%%", stdout)
	}
	// M5 quality signals: no duplicates, no datacenter IPs (fixture uses
	// TEST-NET addresses), 3 missing UAs, all tmax unset -> red flags.
	if !strings.Contains(stdout, "Quality signals:") {
		t.Fatalf("stdout = %q, want quality section", stdout)
	}
	if !strings.Contains(stdout, "duplicates: 0 exact (0 group(s)), 0 near-duplicate (0 group(s)), 0 reused request id(s)") {
		t.Fatalf("stdout = %q, want zero duplication", stdout)
	}
	if !strings.Contains(stdout, "datacenter IPs: 0 (0.0%)") {
		t.Fatalf("stdout = %q, want zero datacenter IPs", stdout)
	}
	if !strings.Contains(stdout, "3 missing, 0 suspicious, 0 ua/os mismatch") {
		t.Fatalf("stdout = %q, want UA anomaly counts", stdout)
	}
	if !strings.Contains(stdout, "tmax: 4 unset (no timeouts set)") {
		t.Fatalf("stdout = %q, want tmax accounting", stdout)
	}
	if !strings.Contains(stdout, "[warn] missing-ua") || !strings.Contains(stdout, "[warn] no-tmax") {
		t.Fatalf("stdout = %q, want missing-ua and no-tmax red flags", stdout)
	}
}

func TestProfileInputJSON(t *testing.T) {
	code, stdout, _ := runForTest(t, "-in", "../ingest/testdata/sample.jsonl", "-json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{`"parsed":4`, `"malformed":1`, `"2.6":1`, `"signals"`, `"score":31.3`, `"id":"device.os"`, `"fill_rate":0.75`, `"quality"`, `"exact_duplicates":0`, `"missing_ua":3`, `"tmax_unset":4`, `"red_flags"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("json stdout = %q, want %s", stdout, want)
		}
	}
}

func TestProfileInputMissing(t *testing.T) {
	code, _, stderr := runForTest(t, "-in", "does-not-exist.jsonl")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "bidscope:") {
		t.Fatalf("stderr = %q, want error notice", stderr)
	}
}

func TestNoInputFailsLoudly(t *testing.T) {
	code, _, stderr := runForTest(t)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "no input") {
		t.Fatalf("stderr = %q, want no-input notice", stderr)
	}
}

func TestGenerateNotYet(t *testing.T) {
	// --generate is parsed but lands in M7.
	code, _, stderr := runForTest(t, "-generate")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "M7") {
		t.Fatalf("stderr = %q, want M7 notice", stderr)
	}
}

func TestParse(t *testing.T) {
	cfg, err := Parse([]string{"-in", "a.jsonl.gz", "-json"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Input != "a.jsonl.gz" || !cfg.JSONOutput {
		t.Fatalf("parsed config = %+v, want Input=a.jsonl.gz JSONOutput=true", cfg)
	}

	cfg, err = Parse([]string{"-generate"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Generate {
		t.Fatalf("parsed config = %+v, want Generate=true", cfg)
	}
}
