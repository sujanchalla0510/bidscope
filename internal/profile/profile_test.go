package profile

import (
	"strings"
	"testing"
)

// fullReq carries all eight completeness signals; sparseReq carries almost
// none; junk is not a bid request at all.
const fullReq = `{"id":"prof-full-1","at":2,"imp":[{"id":"i1","banner":{"format":[{"w":300,"h":250}]}}],"site":{"domain":"synthetic-news.test"},"device":{"os":"android","ip":"203.0.113.10","geo":{"country":"USA"}},"user":{"id":"syn-user-1","eids":[{"source":"ssp.test","uids":[{"id":"eid-1"}]}]},"source":{"ext":{"schain":{"complete":1,"ver":"1.0","nodes":[{"asi":"ssp.test","sid":"1","hp":1}]}}},"regs":{"gdpr":1,"gpp":"DBABMA~SYNTHETIC"}}`

const sparseReq = `{"id":"prof-sparse-1","at":2,"imp":[{"id":"i1","video":{"mimes":["video/mp4"]}}],"app":{"bundle":"com.synthetic.app"},"device":{"os":"ios"}}`

func TestRunReaderCounts(t *testing.T) {
	// Two valid requests, one malformed line, one blank line.
	in := fullReq + "\nnot json at all\n\n" + sparseReq + "\n"
	res, err := RunReader(strings.NewReader(in), "test", 0)
	if err != nil {
		t.Fatalf("RunReader: %v", err)
	}
	if res.Parsed != 2 {
		t.Errorf("Parsed = %d, want 2", res.Parsed)
	}
	if res.Malformed != 1 {
		t.Errorf("Malformed = %d, want 1", res.Malformed)
	}
	if res.Blank != 1 {
		t.Errorf("Blank = %d, want 1", res.Blank)
	}
	if res.Lines != 4 {
		t.Errorf("Lines = %d, want 4", res.Lines)
	}
	if res.Input != "test" {
		t.Errorf("Input = %q, want %q", res.Input, "test")
	}
}

func TestRunReaderReports(t *testing.T) {
	in := fullReq + "\n" + sparseReq + "\n"
	res, err := RunReader(strings.NewReader(in), "test", 2000)
	if err != nil {
		t.Fatalf("RunReader: %v", err)
	}
	// All eight signals present on one of two requests: the composite score
	// lands strictly between 0 and 100.
	if res.Signals.Score <= 0 || res.Signals.Score >= 100 {
		t.Errorf("Signals.Score = %v, want in (0, 100)", res.Signals.Score)
	}
	if len(res.Signals.Signals) != 8 {
		t.Errorf("len(Signals.Signals) = %d, want 8", len(res.Signals.Signals))
	}
	if res.Mix.Requests != 2 {
		t.Errorf("Mix.Requests = %d, want 2", res.Mix.Requests)
	}
	if res.Mix.Site != 1 || res.Mix.App != 1 {
		t.Errorf("Mix site/app = %d/%d, want 1/1", res.Mix.Site, res.Mix.App)
	}
	if res.Quality.Requests != 2 {
		t.Errorf("Quality.Requests = %d, want 2", res.Quality.Requests)
	}
	if res.Biddable.InputQPS != 2000 {
		t.Errorf("Biddable.InputQPS = %v, want 2000", res.Biddable.InputQPS)
	}
	if res.Biddable.BiddableQPS != 2000*res.Biddable.BiddableShare {
		t.Errorf("BiddableQPS = %v, want InputQPS*share", res.Biddable.BiddableQPS)
	}
	if len(res.Versions) == 0 {
		t.Error("Versions is empty, want at least one entry")
	}
}

func TestRunReaderEmpty(t *testing.T) {
	res, err := RunReader(strings.NewReader(""), "empty", 0)
	if err != nil {
		t.Fatalf("RunReader: %v", err)
	}
	if res.Parsed != 0 || res.Signals.Score != 0 {
		t.Errorf("empty input: Parsed=%d Score=%v, want 0/0", res.Parsed, res.Signals.Score)
	}
}
