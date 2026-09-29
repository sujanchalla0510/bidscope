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
}

func TestProfileInputJSON(t *testing.T) {
	code, stdout, _ := runForTest(t, "-in", "../ingest/testdata/sample.jsonl", "-json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{`"parsed":4`, `"malformed":1`, `"2.6":1`} {
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
