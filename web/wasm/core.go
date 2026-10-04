// Command bidscope-wasm compiles the BidScope profiling core to
// WebAssembly for the browser-based web tester. The exported helpers below
// are pure Go (no syscall/js) so they are unit-testable on any platform;
// main_js.go wires them to JavaScript under the js/wasm build tags.
package main

import (
	"encoding/json"
	"strings"

	"github.com/sujanchalla0510/bidscope/internal/generate"
	"github.com/sujanchalla0510/bidscope/internal/profile"
)

// ProfileJSONL profiles one JSONL sample and returns the combined result
// as a JSON document. qps is the stated QPS the sample was drawn from
// (0 = report the biddable share only). The sample never leaves the
// caller's machine: under WebAssembly this all runs in the browser.
func ProfileJSONL(jsonl string, qps float64) (string, error) {
	res, err := profile.RunReader(strings.NewReader(jsonl), "web", qps)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(res); err != nil {
		return "", err
	}
	return b.String(), nil
}

// GenerateJSONL emits n synthetic bid requests of the given profile
// ("clean", "mixed", or "dirty") as JSONL. It is the web tester's
// zero-setup sample data: deterministic for a fixed seed.
func GenerateJSONL(n int, seed int64, profileName string) (string, error) {
	cfg := generate.Config{N: n, Seed: seed, Profile: generate.Profile(profileName)}
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := generate.New(cfg).Emit(&b); err != nil {
		return "", err
	}
	return b.String(), nil
}
