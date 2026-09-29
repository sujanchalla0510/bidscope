package openrtb

import (
	"encoding/json"
	"testing"
)

// request25 is a synthetic OpenRTB 2.5-style bid request: banner + video,
// site inventory, schain under source.ext, consent via regs.ext.
const request25 = `{
  "id": "req-25-001",
  "at": 2,
  "tmax": 120,
  "imp": [
    {"id": "imp-1", "banner": {"format": [{"w": 300, "h": 250}]}, "bidfloor": 0.50, "bidfloorcur": "USD",
     "pmp": {"privateauction": 0, "deals": [{"id": "deal-9", "bidfloor": 2.00, "at": 3}]},
     "future_unknown_field": "ignored"},
    {"id": "imp-2", "video": {"mimes": ["video/mp4"], "w": 640, "h": 480}, "bidfloor": 3.10}
  ],
  "site": {"domain": "example-news.test", "page": "https://example-news.test/article/1", "publisher": {"id": "pub-42"}},
  "device": {"os": "android", "osv": "14", "ip": "203.0.113.7", "ua": "TestBrowser/1.0", "devicetype": 4,
             "geo": {"country": "USA", "region": "TX", "city": "Austin", "lat": 30.27, "lon": -97.74, "zip": "78701"}},
  "user": {"id": "user-abc", "buyeruid": "dsp-xyz", "eids": [{"source": "test-ssp.test", "uids": [{"id": "eid-1"}]}]},
  "source": {"fd": 1, "ext": {"schain": {"complete": 1, "ver": "1.0",
     "nodes": [{"asi": "test-ssp.test", "sid": "00001", "hp": 1}]}}},
  "regs": {"ext": {"gdpr": 1}}
}`

// request26 is a synthetic OpenRTB 2.6-style bid request: audio + rewarded,
// app inventory, regs.gpp, device.sua.
const request26 = `{
  "id": "req-26-001",
  "at": 1,
  "imp": [
    {"id": "imp-1", "audio": {"mimes": ["audio/mp4"]}, "bidfloor": 1.25, "rwdd": 1, "ssai": 1}
  ],
  "app": {"bundle": "com.test.app", "storeurl": "https://store.test/app/1", "publisher": {"id": "pub-7"}},
  "device": {"os": "ios", "osv": "18", "ifa": "00000000-0000-0000-0000-000000000000",
             "sua": {"browsers": [{"brand": "Test", "version": ["1"]}]}},
  "regs": {"gdpr": 1, "gpp": "DBABMA~CPXx"}
}`

func mustParse(t *testing.T, raw string) *BidRequest {
	t.Helper()
	var br BidRequest
	if err := json.Unmarshal([]byte(raw), &br); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	return &br
}

func TestParseRequest25(t *testing.T) {
	br := mustParse(t, request25)
	if br.ID != "req-25-001" {
		t.Errorf("id = %q", br.ID)
	}
	if len(br.Imp) != 2 {
		t.Fatalf("impressions = %d, want 2", len(br.Imp))
	}
	b := br.Imp[0].Banner
	if b == nil || len(b.Format) != 1 || b.Format[0].W != 300 || b.Format[0].H != 250 {
		t.Errorf("banner format not parsed: %+v", b)
	}
	if len(br.Imp[0].PMP.Deals) != 1 || br.Imp[0].PMP.Deals[0].ID != "deal-9" {
		t.Errorf("pmp deals not parsed: %+v", br.Imp[0].PMP)
	}
	if br.Imp[1].Video == nil || br.Imp[1].BidFloor != 3.10 {
		t.Errorf("video imp not parsed: %+v", br.Imp[1])
	}
	if br.Site == nil || br.Site.Domain != "example-news.test" {
		t.Errorf("site not parsed: %+v", br.Site)
	}
	if br.Device == nil || br.Device.IP != "203.0.113.7" || br.Device.Geo == nil || br.Device.Geo.City != "Austin" {
		t.Errorf("device/geo not parsed: %+v", br.Device)
	}
	if br.User == nil || br.User.BuyerUID != "dsp-xyz" || len(br.User.EIDs) != 1 {
		t.Errorf("user/eids not parsed: %+v", br.User)
	}
	sc := br.ParseSChain()
	if sc == nil || sc.Ver != "1.0" || len(sc.Nodes) != 1 || sc.Nodes[0].ASI != "test-ssp.test" {
		t.Errorf("schain not parsed: %+v", sc)
	}
	if br.TMax != 120 {
		t.Errorf("tmax = %d", br.TMax)
	}
}

func TestParseRequest26(t *testing.T) {
	br := mustParse(t, request26)
	if br.Imp[0].Audio == nil || br.Imp[0].Rwdd != 1 || br.Imp[0].Ssai != 1 {
		t.Errorf("2.6 audio/rwdd/ssai not parsed: %+v", br.Imp[0])
	}
	if br.App == nil || br.App.Bundle != "com.test.app" {
		t.Errorf("app not parsed: %+v", br.App)
	}
	if br.Regs == nil || br.Regs.GPP != "DBABMA~CPXx" || br.Regs.GDPR != 1 {
		t.Errorf("regs not parsed: %+v", br.Regs)
	}
	if sc := br.ParseSChain(); sc != nil {
		t.Errorf("ParseSChain should be nil without source.ext, got %+v", sc)
	}
}

func TestDetectVersion(t *testing.T) {
	if got := DetectVersion(nil); got != VersionUnknown {
		t.Errorf("nil = %q, want unknown", got)
	}
	if got := DetectVersion(mustParse(t, request25)); got != Version25 {
		t.Errorf("2.5-style = %q, want 2.5", got)
	}
	if got := DetectVersion(mustParse(t, request26)); got != Version26 {
		t.Errorf("2.6-style = %q, want 2.6", got)
	}
	// A 2.6 audio-only imp is enough to flip the heuristic.
	br := mustParse(t, `{"id":"x","imp":[{"id":"i","audio":{"mimes":["audio/mp4"]}}]}`)
	if got := DetectVersion(br); got != Version26 {
		t.Errorf("audio imp = %q, want 2.6", got)
	}
}

func TestParseSChainMalformed(t *testing.T) {
	br := mustParse(t, `{"id":"x","imp":[],"source":{"ext":{"schain":"a string, not an object"}}}`)
	if sc := br.ParseSChain(); sc != nil {
		t.Errorf("malformed schain should return nil, got %+v", sc)
	}
}

func TestMinimalRequest(t *testing.T) {
	// Nothing but ids: every optional pointer must stay nil, not panic.
	br := mustParse(t, `{"id":"bare","imp":[{"id":"i1"}]}`)
	if br.Device != nil || br.Site != nil || br.User != nil || br.Source != nil || br.Regs != nil {
		t.Errorf("optional sections should be nil: %+v", br)
	}
	if got := DetectVersion(br); got != Version25 {
		t.Errorf("bare request = %q, want 2.5 default", got)
	}
}
