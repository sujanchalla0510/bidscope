package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The bridge helpers are pure Go, so the whole WASM surface is testable
// without a browser: these tests pin the JSON contract the site relies on.

func TestGenerateJSONLRoundTrip(t *testing.T) {
	sample, err := GenerateJSONL(50, 7, "mixed")
	if err != nil {
		t.Fatalf("GenerateJSONL: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(sample), "\n")
	if len(lines) != 50 {
		t.Fatalf("generated %d lines, want 50", len(lines))
	}
	var probe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &probe); err != nil || probe.ID == "" {
		t.Fatalf("first line is not a bid request: %v", err)
	}
	// Deterministic for a fixed seed: same seed, same bytes.
	again, err := GenerateJSONL(50, 7, "mixed")
	if err != nil {
		t.Fatalf("GenerateJSONL: %v", err)
	}
	if again != sample {
		t.Error("same seed produced different bytes")
	}
}

func TestGenerateJSONLBadProfile(t *testing.T) {
	if _, err := GenerateJSONL(10, 7, "bogus"); err == nil {
		t.Error("bogus profile: want error, got nil")
	}
}

func TestProfileJSONLContract(t *testing.T) {
	sample, err := GenerateJSONL(100, 7, "mixed")
	if err != nil {
		t.Fatalf("GenerateJSONL: %v", err)
	}
	out, err := ProfileJSONL(sample, 0)
	if err != nil {
		t.Fatalf("ProfileJSONL: %v", err)
	}
	var res struct {
		Parsed  int `json:"parsed"`
		Signals struct {
			Score float64 `json:"score"`
		} `json:"signals"`
		Biddable struct {
			BiddableShare float64 `json:"biddable_share"`
		} `json:"biddable"`
		Mix struct {
			Requests int `json:"requests"`
		} `json:"mix"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if res.Parsed != 100 || res.Mix.Requests != 100 {
		t.Errorf("parsed=%d mix.requests=%d, want 100/100", res.Parsed, res.Mix.Requests)
	}
	if res.Signals.Score <= 0 {
		t.Errorf("signal score = %v, want > 0 for mixed traffic", res.Signals.Score)
	}
	if res.Biddable.BiddableShare <= 0 || res.Biddable.BiddableShare > 1 {
		t.Errorf("biddable share = %v, want in (0, 1]", res.Biddable.BiddableShare)
	}
}

func TestProfileJSONLEmpty(t *testing.T) {
	out, err := ProfileJSONL("", 0)
	if err != nil {
		t.Fatalf("ProfileJSONL: %v", err)
	}
	var res struct {
		Parsed int `json:"parsed"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if res.Parsed != 0 {
		t.Errorf("parsed = %d, want 0", res.Parsed)
	}
}
