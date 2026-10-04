// Package ingest streams OpenRTB bid requests out of JSONL input.
//
// The input is one JSON object per line (optionally gzip-compressed); "-"
// reads stdin. Reading is lazy and bounded: only one line is in memory at a
// time, so multi-million-line samples never balloon the heap.
//
// Malformed lines are surfaced as *LineError so the caller can keep going
// (trial-traffic dumps from real exchanges routinely contain a few bad
// lines). Stats() always reports the full accounting afterwards.
package ingest

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// LineError reports a single malformed input line without aborting the
// stream. Callers treat it as "skip and continue".
type LineError struct {
	Line int   // 1-based line number in the input
	Err  error // the underlying decode failure
}

func (e *LineError) Error() string {
	return fmt.Sprintf("line %d: %v", e.Line, e.Err)
}

func (e *LineError) Unwrap() error { return e.Err }

// Stats is the read accounting for a stream.
type Stats struct {
	Lines     int // total lines seen (including blank)
	Parsed    int // lines that decoded into a bid request
	Blank     int // empty/whitespace-only lines, skipped silently
	Malformed int // lines that failed to decode
}

// Stream iterates over the bid requests in one input.
type Stream struct {
	scanner *bufio.Scanner
	closer  io.Closer // nil for stdin
	line    int
	stats   Stats
	version map[string]int // OpenRTB version mix, keyed by openrtb version string
	closed  bool
}

// NewStream returns a Stream reading JSONL bid requests from r (plain,
// never gzip — callers sniffing compression should decompress first).
func NewStream(r io.Reader) *Stream {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024) // long lines happen; cap at 16MB
	return &Stream{scanner: sc, version: map[string]int{}}
}

// Open prepares a Stream for path ("-" reads stdin). Gzip is detected from
// the ".gz" suffix. The file is not read until the first Next call.
func Open(path string) (*Stream, error) {
	var (
		r      io.Reader
		closer io.Closer
	)
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("ingest: open %q: %w", path, err)
		}
		closer = f
		r = f
		if strings.HasSuffix(strings.ToLower(path), ".gz") {
			gz, err := gzip.NewReader(f)
			if err != nil {
				f.Close()
				return nil, fmt.Errorf("ingest: gzip %q: %w", path, err)
			}
			r = gz
			closer = multiCloser{[]io.Closer{gz, f}}
		}
	}
	s := NewStream(r)
	s.closer = closer
	return s, nil
}

// Next returns the next bid request, a *LineError for a malformed line
// (call Next again to continue), or io.EOF when the input is exhausted.
func (s *Stream) Next() (*openrtb.BidRequest, error) {
	for s.scanner.Scan() {
		s.line++
		s.stats.Lines++
		raw := strings.TrimSpace(s.scanner.Text())
		if raw == "" {
			s.stats.Blank++
			continue
		}
		var br openrtb.BidRequest
		if err := json.Unmarshal([]byte(raw), &br); err != nil {
			s.stats.Malformed++
			return nil, &LineError{Line: s.line, Err: err}
		}
		s.stats.Parsed++
		s.version[openrtb.DetectVersion(&br)]++
		return &br, nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("ingest: read: %w", err)
	}
	return nil, io.EOF
}

// Stats returns the read accounting so far.
func (s *Stream) Stats() Stats { return s.stats }

// VersionMix returns how many parsed requests looked like each OpenRTB
// version ("2.5", "2.6", "unknown").
func (s *Stream) VersionMix() map[string]int {
	out := make(map[string]int, len(s.version))
	for k, v := range s.version {
		out[k] = v
	}
	return out
}

// Close releases the underlying file. Safe to call more than once.
func (s *Stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

// multiCloser closes several resources in order, returning the first error.
type multiCloser struct {
	cs []io.Closer
}

func (m multiCloser) Close() error {
	var first error
	for _, c := range m.cs {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// ReadAll is a convenience helper: drain a Stream into a slice, skipping
// malformed lines, and return the aggregate stats. It always closes the
// stream. Intended for small samples (tests, the --generate path later);
// the streaming Next loop is the API for real samples.
func ReadAll(s *Stream) ([]*openrtb.BidRequest, Stats, error) {
	defer s.Close()
	var out []*openrtb.BidRequest
	for {
		br, err := s.Next()
		switch {
		case err == nil:
			out = append(out, br)
		case errors.Is(err, io.EOF):
			return out, s.Stats(), nil
		case AsLineError(err):
			continue // skip malformed, keep the accounting in Stats
		default:
			return out, s.Stats(), err
		}
	}
}

// AsLineError reports whether err is a *LineError.
func AsLineError(err error) bool {
	var le *LineError
	return errors.As(err, &le)
}
