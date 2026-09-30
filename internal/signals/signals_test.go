package signals

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// full is a synthetic request carrying all eight signals: os+ip, user id,
// site domain, geo, gdpr+gpp consent, schain, and eids.
const full = `{
  "id": "sig-full-1", "at": 2,
  "imp": [{"id": "i1", "banner": {"format": [{"w": 300, "h": 250}]}}],
  "site": {"domain": "synthetic-news.test"},
  "device": {"os": "android", "ip": "203.0.113.10",
             "geo": {"country": "USA", "lat": 30.27, "lon": -97.74}},
  "user": {"id": "syn-user-1",
           "eids": [{"source": "ssp.test", "uids": [{"id": "eid-1"}]}]},
  "source": {"ext": {"schain": {"complete": 1, "ver": "1.0",
     "nodes": [{"asi": "ssp.test", "sid": "1", "hp": 1}]}}},
  "regs": {"gdpr": 1, "gpp": "DBABMA~SYNTHETIC"}
}`

// sparse is a synthetic request carrying only OS and inventory identity:
// everything else a DSP would want is missing.
const sparse = `{
  "id": "sig-sparse-1", "at": 2,
  "imp": [{"id": "i1", "video": {"mimes": ["video/mp4"]}}],
  "app": {"bundle": "com.synthetic.app"},
  "device": {"os": "ios"}
}`

func mustParse(t *testing.T, raw string) *openrtb.BidRequest {
	t.Helper()
	var br openrtb.BidRequest
	if err := json.Unmarshal([]byte(raw), &br); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	return &br
}

func TestEmptyEngine(t *testing.T) {
	e := NewEngine()
	if e.Count() != 0 {
		t.Errorf("Count = %d, want 0", e.Count())
	}
	if e.Score() != 0 {
		t.Errorf("Score = %v, want 0", e.Score())
	}
	for _, d := range Definitions {
		if e.FillRate(d.ID) != 0 {
			t.Errorf("FillRate(%s) = %v, want 0", d.ID, e.FillRate(d.ID))
		}
	}
}

func TestAllSignals(t *testing.T) {
	e := NewEngine()
	e.Add(mustParse(t, full))
	if e.Count() != 1 {
		t.Fatalf("Count = %d, want 1", e.Count())
	}
	for _, d := range Definitions {
		if got := e.Present(d.ID); got != 1 {
			t.Errorf("Present(%s) = %d, want 1", d.ID, got)
		}
		if got := e.FillRate(d.ID); got != 1 {
			t.Errorf("FillRate(%s) = %v, want 1", d.ID, got)
		}
	}
	if e.Score() != 100 {
		t.Errorf("Score = %v, want 100", e.Score())
	}
}

func TestSparseSignals(t *testing.T) {
	e := NewEngine()
	e.Add(mustParse(t, sparse))
	want := map[SignalID]bool{
		DeviceOS:    true,
		InventoryID: true, // app.bundle counts
	}
	for _, d := range Definitions {
		got := e.Present(d.ID) == 1
		if got != want[d.ID] {
			t.Errorf("Present(%s) = %v, want %v", d.ID, got, want[d.ID])
		}
	}
	// 2 of 8 signals => score 25.0.
	if s := e.Score(); s != 25 {
		t.Errorf("Score = %v, want 25", s)
	}
}

func TestMixedSampleScoreMath(t *testing.T) {
	e := NewEngine()
	e.Add(mustParse(t, full))   // 8/8
	e.Add(mustParse(t, sparse)) // 2/8
	e.Add(mustParse(t, full))   // 8/8
	if e.Count() != 3 {
		t.Fatalf("Count = %d, want 3", e.Count())
	}
	if got := e.FillRate(DeviceOS); got != 1 {
		t.Errorf("FillRate(device.os) = %v, want 1", got)
	}
	if got := e.FillRate(UserID); got != 2.0/3 {
		t.Errorf("FillRate(user.id) = %v, want 2/3", got)
	}
	// Score = mean of fill rates: device.os 1, device.ip 2/3, user.id 2/3,
	// inventory.id 1, geo 2/3, consent 2/3, schain 2/3, eids 2/3.
	// = (1 + 1 + 6*(2/3)) / 8 * 100 = (2 + 4)/8*100 = 75.
	if s := e.Score(); s != 75 {
		t.Errorf("Score = %v, want 75", s)
	}
}

func TestNilRequestCountsAsEmpty(t *testing.T) {
	e := NewEngine()
	e.Add(mustParse(t, full))
	e.Add(nil)
	if e.Count() != 2 {
		t.Fatalf("Count = %d, want 2", e.Count())
	}
	for _, d := range Definitions {
		if got := e.FillRate(d.ID); got != 0.5 {
			t.Errorf("FillRate(%s) = %v, want 0.5", d.ID, got)
		}
	}
	if s := e.Score(); s != 50 {
		t.Errorf("Score = %v, want 50", s)
	}
}

func TestConsentVariants(t *testing.T) {
	cases := []struct {
		name string
		regs string
		want bool
	}{
		{"gdpr1", `{"gdpr": 1}`, true},
		{"gpp", `{"gpp": "DBABMA~X"}`, true},
		{"regs-ext-gdpr", `{"ext": {"gdpr": 1}}`, true},
		{"no-regs", ``, false},
		{"empty-regs", `{}`, false},
		// gdpr:0 decodes the same as absent (omitempty int8) and carries
		// no actionable buyer signal, so it does not count.
		{"gdpr0", `{"gdpr": 0}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"id":"c","at":2,"imp":[{"id":"i1"}]`
			if tc.regs != "" {
				raw += `,"regs":` + tc.regs
			}
			raw += `}`
			br := mustParse(t, raw)
			found := false
			for _, d := range Definitions {
				if d.ID == Consent {
					found = d.Present(br)
				}
			}
			if found != tc.want {
				t.Errorf("consent present = %v, want %v (regs=%s)", found, tc.want, tc.regs)
			}
		})
	}
}

func TestGeoVariants(t *testing.T) {
	cases := []struct {
		name string
		geo  string
		want bool
	}{
		{"country", `{"country": "USA"}`, true},
		{"city-only", `{"city": "Austin"}`, true},
		{"zip-only", `{"zip": "78701"}`, true},
		{"latlon", `{"lat": 30.27, "lon": -97.74}`, true},
		{"lat-only", `{"lat": 30.27}`, false},
		{"empty", `{}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"id":"g","at":2,"imp":[{"id":"i1"}],"device":{"os":"x","geo":` + tc.geo + `}}`
			br := mustParse(t, raw)
			for _, d := range Definitions {
				if d.ID == Geo {
					if got := d.Present(br); got != tc.want {
						t.Errorf("geo present = %v, want %v (geo=%s)", got, tc.want, tc.geo)
					}
				}
			}
		})
	}
}

func TestIPVariants(t *testing.T) {
	for name, dev := range map[string]string{
		"ipv4": `{"os":"x","ip":"203.0.113.5"}`,
		"ipv6": `{"os":"x","ipv6":"2001:db8::1"}`,
		"none": `{"os":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			br := mustParse(t, `{"id":"ip","at":2,"imp":[{"id":"i1"}],"device":`+dev+`}`)
			for _, d := range Definitions {
				if d.ID == DeviceIP {
					if got := d.Present(br); got != (name != "none") {
						t.Errorf("device.ip present = %v for %s", got, name)
					}
				}
			}
		})
	}
}

func TestBrokenSChainIsAbsent(t *testing.T) {
	// Malformed schain payloads must degrade the schain fill rate, not crash.
	raw := `{"id":"s","at":2,"imp":[{"id":"i1"}],
	         "source":{"ext":{"schain":"not-an-object"}}}`
	br := mustParse(t, raw)
	e := NewEngine()
	e.Add(br)
	if got := e.Present(SChain); got != 0 {
		t.Errorf("Present(schain) = %d, want 0", got)
	}
	if br.ParseSChain() != nil {
		t.Error("ParseSChain should return nil for a malformed schain")
	}
}

func TestReportJSONShape(t *testing.T) {
	e := NewEngine()
	e.Add(mustParse(t, full))
	e.Add(mustParse(t, sparse))
	r := e.Report()
	if r.Requests != 2 {
		t.Errorf("Requests = %d, want 2", r.Requests)
	}
	if len(r.Signals) != len(Definitions) {
		t.Fatalf("Signals = %d entries, want %d", len(r.Signals), len(Definitions))
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"requests", "score", "signals"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("report JSON missing key %q", key)
		}
	}
	first := decoded["signals"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "label", "present", "total", "fill_rate"} {
		if _, ok := first[key]; !ok {
			t.Errorf("signal JSON missing key %q", key)
		}
	}
	// Score stays within bounds and matches the engine.
	if s, ok := decoded["score"].(float64); !ok || math.Abs(s-e.Score()) > 1e-9 {
		t.Errorf("score = %v, want %v", s, e.Score())
	}
}

func TestScoreRounding(t *testing.T) {
	// 1 of 3 requests => 33.3 after rounding, not a long float tail.
	e := NewEngine()
	e.Add(mustParse(t, full))
	e.Add(nil)
	e.Add(nil)
	r := e.Report()
	if got := r.Signals[0].FillRate; math.Abs(got-1.0/3) > 1e-12 {
		t.Errorf("fill rate = %v, want 1/3", got)
	}
	if r.Score != 33.3 {
		t.Errorf("Score = %v, want 33.3", r.Score)
	}
}
