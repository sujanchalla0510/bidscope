package biddable

import (
	"testing"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// req builds a fully biddable bid request.
func req(id string) *openrtb.BidRequest {
	return &openrtb.BidRequest{
		ID:  id,
		Imp: []openrtb.Imp{{ID: "i1", Banner: &openrtb.Banner{}}},
		Device: &openrtb.Device{
			OS:  "android",
			IP:  "198.51.100.23", // TEST-NET-2: never a datacenter IP
			IFA: "ifa-" + id,
			UA:  "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36",
			Geo: &openrtb.Geo{Country: "USA"},
		},
		User: &openrtb.User{ID: "user-" + id},
		Site: &openrtb.Site{Domain: "synthetic.test"},
	}
}

func TestEmptyReport(t *testing.T) {
	r := NewEngine().Report(0)
	if r.Requests != 0 || r.Biddable != 0 || r.BiddableShare != 0 || r.BiddableQPS != 0 {
		t.Fatalf("empty report = %+v, want zeros", r)
	}
	if len(r.Gates) != len(Gates) {
		t.Fatalf("gates = %d, want %d", len(r.Gates), len(Gates))
	}
	for _, g := range r.Gates {
		if g.Failed != 0 || g.FailRate != 0 {
			t.Fatalf("empty gate %s = %+v, want zeros", g.ID, g)
		}
	}
}

func TestFullyBiddable(t *testing.T) {
	e := NewEngine()
	for _, id := range []string{"a", "b", "c"} {
		e.Add(req(id))
	}
	r := e.Report(10000)
	if r.Biddable != 3 || r.Requests != 3 {
		t.Fatalf("biddable = %d/%d, want 3/3", r.Biddable, r.Requests)
	}
	if r.BiddableShare != 1 {
		t.Fatalf("share = %v, want 1", r.BiddableShare)
	}
	if r.BiddableQPS != 10000 {
		t.Fatalf("biddable QPS = %v, want 10000", r.BiddableQPS)
	}
}

func TestEachGateRejects(t *testing.T) {
	e := NewEngine()

	dup := req("x") // exact duplicate pair: second is a replay
	e.Add(dup)
	e.Add(req("x"))

	noInv := req("noinv")
	noInv.Site = nil
	e.Add(noInv)

	noID := req("noid")
	noID.Device.IP, noID.Device.IFA, noID.User.ID = "", "", ""
	e.Add(noID)

	noGeo := req("nogeo")
	noGeo.Device.Geo = nil
	e.Add(noGeo)

	noUA := req("noua")
	noUA.Device.UA = ""
	e.Add(noUA)

	dc := req("dc")
	dc.Device.IP = "3.5.140.1" // inside AWS ranges in the quality static list
	e.Add(dc)

	r := e.Report(0)
	// dup-pair contributes 1 duplicate failure; every other request is
	// otherwise clean, so each gate should fail exactly once.
	want := map[GateID]int{
		Duplicate: 1, Inventory: 1, Identity: 1, Geo: 1, UserAgent: 1, Datacenter: 1,
	}
	if r.Requests != 7 {
		t.Fatalf("requests = %d, want 7", r.Requests)
	}
	for _, g := range r.Gates {
		if g.Failed != want[g.ID] {
			t.Fatalf("gate %s failed %d, want %d", g.ID, g.Failed, want[g.ID])
		}
	}
	if r.Biddable != 1 {
		t.Fatalf("biddable = %d, want 1 (only the first of the dup pair)", r.Biddable)
	}
}

func TestSuspiciousUARejected(t *testing.T) {
	e := NewEngine()
	b := req("bot")
	b.Device.UA = "python-requests/2.31"
	e.Add(b)
	if e.GateFails(UserAgent) != 1 {
		t.Fatalf("suspicious UA not flagged")
	}
}

func TestNilNeverBiddable(t *testing.T) {
	e := NewEngine()
	e.Add(nil)
	r := e.Report(0)
	if r.Biddable != 0 || r.Requests != 1 {
		t.Fatalf("nil request: biddable = %d/%d, want 0/1", r.Biddable, r.Requests)
	}
}

func TestQPSUnsetMeansShareOnly(t *testing.T) {
	e := NewEngine()
	e.Add(req("q"))
	r := e.Report(0)
	if r.BiddableQPS != 0 || r.InputQPS != 0 {
		t.Fatalf("unset QPS must leave absolute estimate at 0, got %+v", r)
	}
}

func TestPartialShareScalesQPS(t *testing.T) {
	e := NewEngine()
	e.Add(req("ok"))
	bad := req("bad")
	bad.Site = nil // fails inventory only
	e.Add(bad)
	r := e.Report(40000)
	if r.Biddable != 1 || r.Requests != 2 {
		t.Fatalf("biddable = %d/%d, want 1/2", r.Biddable, r.Requests)
	}
	if r.BiddableQPS != 20000 {
		t.Fatalf("biddable QPS = %v, want 20000", r.BiddableQPS)
	}
}
