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

func TestIngestNotImplementedYet(t *testing.T) {
	code, _, stderr := runForTest(t, "-in", "sample.jsonl")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "not implemented yet") {
		t.Fatalf("stderr = %q, want unimplemented notice", stderr)
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
