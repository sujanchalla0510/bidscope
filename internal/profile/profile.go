// Package profile folds a JSONL bidstream sample through every BidScope
// analyzer and returns the combined result.
//
// It is the shared core behind the CLI and the WebAssembly web tester, so
// both surfaces always report identical numbers for the same input.
package profile

import (
	"errors"
	"io"

	"github.com/sujanchalla0510/bidscope/internal/biddable"
	"github.com/sujanchalla0510/bidscope/internal/ingest"
	"github.com/sujanchalla0510/bidscope/internal/mix"
	"github.com/sujanchalla0510/bidscope/internal/quality"
	"github.com/sujanchalla0510/bidscope/internal/signals"
)

// Result is the full profiling summary over one sample: ingest accounting,
// the OpenRTB version mix, the signal-completeness report with its composite
// signal score, the inventory mix analysis, the quality-signal report, and
// the biddable-QPS estimate.
type Result struct {
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

// Run folds every bid request in s through the four engines and returns the
// combined result. input names the sample (a file path, "-", or a label
// like "web"); inputQPS is the stated QPS the sample was drawn from and
// scales the biddable estimate (0 reports the share only). Malformed lines
// are skipped and counted, exactly like the CLI.
func Run(s *ingest.Stream, input string, inputQPS float64) (Result, error) {
	var res Result
	res.Input = input
	sigEng := signals.NewEngine()
	mixEng := mix.NewEngine()
	qEng := quality.NewEngine()
	bEng := biddable.NewEngine()
	for {
		br, err := s.Next()
		switch {
		case err == nil:
			sigEng.Add(br)
			mixEng.Add(br)
			qEng.Add(br)
			bEng.Add(br)
		case errors.Is(err, io.EOF):
			st := s.Stats()
			res.Lines, res.Parsed, res.Blank, res.Malformed = st.Lines, st.Parsed, st.Blank, st.Malformed
			res.Versions = s.VersionMix()
			res.Signals = sigEng.Report()
			res.Mix = mixEng.Report()
			res.Quality = qEng.Report()
			res.Biddable = bEng.Report(inputQPS)
			return res, nil
		case ingest.AsLineError(err):
			continue // malformed lines are skipped; Stats keeps count
		default:
			return res, err
		}
	}
}

// RunReader is Run over an in-memory reader (used by the web tester, where
// the sample arrives as a pasted string, never a file).
func RunReader(r io.Reader, input string, inputQPS float64) (Result, error) {
	return Run(ingest.NewStream(r), input, inputQPS)
}
