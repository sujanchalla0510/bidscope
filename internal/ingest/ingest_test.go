package ingest

import (
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// zipSample writes a gzipped copy of testdata/sample.jsonl next to the test
// run (t.TempDir) so the fixture stays small in the repo.
func zipSample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "sample.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out := filepath.Join(t.TempDir(), "sample.jsonl.gz")
	f, err := os.Create(out)
	if err != nil {
		t.Fatalf("create gz: %v", err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write(raw); err != nil {
		t.Fatalf("write gz: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gz: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return out
}

func drain(t *testing.T, s *Stream) ([]*openrtb.BidRequest, []int) {
	t.Helper()
	var reqs []*openrtb.BidRequest
	var badLines []int
	for {
		br, err := s.Next()
		switch {
		case err == nil:
			reqs = append(reqs, br)
		case errors.Is(err, io.EOF):
			return reqs, badLines
		default:
			var le *LineError
			if !errors.As(err, &le) {
				t.Fatalf("unexpected error type: %v", err)
			}
			badLines = append(badLines, le.Line)
		}
	}
}

func TestStreamSample(t *testing.T) {
	s, err := Open(filepath.Join("testdata", "sample.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	reqs, badLines := drain(t, s)
	if len(reqs) != 4 {
		t.Fatalf("parsed = %d, want 4", len(reqs))
	}
	if len(badLines) != 1 || badLines[0] != 5 {
		t.Fatalf("malformed lines = %v, want [5]", badLines)
	}
	if reqs[0].ID != "syn-001" || reqs[3].ID != "syn-004" {
		t.Errorf("ids out of order: %s %s", reqs[0].ID, reqs[3].ID)
	}
	if reqs[3].ParseSChain() == nil {
		t.Errorf("syn-004 schain should parse")
	}

	st := s.Stats()
	if st.Lines != 6 || st.Parsed != 4 || st.Blank != 1 || st.Malformed != 1 {
		t.Errorf("stats = %+v, want {6 4 1 1}", st)
	}
	mix := s.VersionMix()
	if mix[openrtb.Version26] != 1 || mix[openrtb.Version25] != 3 {
		t.Errorf("version mix = %v, want {2.5:3 2.6:1}", mix)
	}
}

func TestStreamGzip(t *testing.T) {
	s, err := Open(zipSample(t))
	if err != nil {
		t.Fatalf("open gz: %v", err)
	}
	reqs, badLines := drain(t, s)
	s.Close()
	if len(reqs) != 4 || len(badLines) != 1 {
		t.Fatalf("gz: parsed=%d malformed=%d, want 4/1", len(reqs), len(badLines))
	}
}

func TestOpenMissing(t *testing.T) {
	if _, err := Open(filepath.Join("testdata", "nope.jsonl")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestOpenBadGzip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.jsonl.gz")
	if err := os.WriteFile(p, []byte("not gzip data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Error("expected error for corrupt gzip")
	}
}

func TestEmptyFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("empty file: Next = %v, want EOF", err)
	}
	if st := s.Stats(); st != (Stats{}) {
		t.Errorf("empty stats = %+v", st)
	}
}

func TestReadAllSkipsMalformed(t *testing.T) {
	s, err := Open(filepath.Join("testdata", "sample.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	reqs, st, err := ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(reqs) != 4 || st.Malformed != 1 {
		t.Errorf("ReadAll: parsed=%d malformed=%d, want 4/1", len(reqs), st.Malformed)
	}
}

func TestLineErrorMessage(t *testing.T) {
	le := &LineError{Line: 7, Err: errors.New("boom")}
	if !strings.Contains(le.Error(), "line 7") {
		t.Errorf("message = %q", le.Error())
	}
	if !AsLineError(le) || AsLineError(io.EOF) {
		t.Error("AsLineError classification wrong")
	}
}

func TestCloseIdempotent(t *testing.T) {
	s, err := Open(filepath.Join("testdata", "sample.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestNewStreamReader(t *testing.T) {
	// Reader-based streams behave exactly like file-based ones: valid lines
	// parse, malformed lines surface as *LineError, and Stats accounts.
	in := `{"id":"ns-1","imp":[]}` + "\n" + "junk\n" + `{"id":"ns-2","imp":[]}` + "\n"
	s := NewStream(strings.NewReader(in))
	n := 0
	for {
		br, err := s.Next()
		switch {
		case err == nil:
			n++
			if br.ID != "ns-1" && br.ID != "ns-2" {
				t.Errorf("unexpected id %q", br.ID)
			}
		case AsLineError(err):
			continue
		case errors.Is(err, io.EOF):
			goto done
		default:
			t.Fatalf("Next: %v", err)
		}
	}
done:
	if n != 2 {
		t.Errorf("parsed %d requests, want 2", n)
	}
	if st := s.Stats(); st.Parsed != 2 || st.Malformed != 1 || st.Lines != 3 {
		t.Errorf("Stats = %+v, want Parsed=2 Malformed=1 Lines=3", st)
	}
}
