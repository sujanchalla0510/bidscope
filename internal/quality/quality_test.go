package quality

import (
	"strings"
	"testing"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

func req(id string) *openrtb.BidRequest {
	return &openrtb.BidRequest{
		ID:  id,
		Imp: []openrtb.Imp{{ID: "i1", Banner: &openrtb.Banner{}}},
		Device: &openrtb.Device{
			OS: "android",
			IP: "198.51.100.23", // TEST-NET-2: never a datacenter IP
			UA: "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36",
		},
		User: &openrtb.User{ID: "user-" + id},
		Site: &openrtb.Site{Domain: "synthetic.test"},
	}
}

func TestEmptyReport(t *testing.T) {
	r := NewEngine().Report()
	if r.Requests != 0 || r.ExactDuplicates != 0 || r.NearDuplicates != 0 {
		t.Fatalf("empty report = %+v, want zeros", r)
	}
	if len(r.RedFlags) != 0 {
		t.Fatalf("empty report has red flags: %+v", r.RedFlags)
	}
	if r.TopUAShare != 0 || r.DatacenterIPShare != 0 || r.TMaxMedian != 0 {
		t.Fatalf("empty shares not zero: %+v", r)
	}
}

func TestExactDuplicates(t *testing.T) {
	e := NewEngine()
	a, b := req("dup-1"), req("dup-1")
	// Identical payloads, identical ids: exact duplicate + reused id.
	e.Add(a)
	e.Add(b)
	e.Add(req("other"))

	r := e.Report()
	if r.Requests != 3 {
		t.Fatalf("requests = %d, want 3", r.Requests)
	}
	if r.ExactDuplicates != 1 || r.ExactDuplicateGroups != 1 {
		t.Fatalf("exact dup = %d/%d, want 1/1", r.ExactDuplicates, r.ExactDuplicateGroups)
	}
	if r.DuplicateIDs != 1 {
		t.Fatalf("duplicate ids = %d, want 1", r.DuplicateIDs)
	}
	// No near duplicates: the two dupes share one payload hash, so their
	// fingerprint carries a single distinct payload.
	if r.NearDuplicates != 0 || r.NearDuplicateGroups != 0 {
		t.Fatalf("near dup = %d/%d, want 0/0", r.NearDuplicates, r.NearDuplicateGroups)
	}
}

func TestNearDuplicates(t *testing.T) {
	e := NewEngine()
	// Same actor (device/user/inventory), different request ids and no
	// exact payload match: same fingerprint, two payloads.
	a := req("nd-1")
	b := req("nd-2")
	b.TMax = 250          // auction-specific field differs; fingerprint ignores it
	b.User.ID = a.User.ID // same actor, different auction
	e.Add(a)
	e.Add(b)

	r := e.Report()
	if r.ExactDuplicates != 0 {
		t.Fatalf("exact dup = %d, want 0", r.ExactDuplicates)
	}
	if r.NearDuplicates != 2 || r.NearDuplicateGroups != 1 {
		t.Fatalf("near dup = %d/%d, want 2/1", r.NearDuplicates, r.NearDuplicateGroups)
	}
}

func TestDatacenterIPs(t *testing.T) {
	if !IsDatacenterIP("52.10.20.30") { // AWS range
		t.Fatal("52.10.20.30 should be flagged as datacenter")
	}
	if !IsDatacenterIP("18.200.0.1") { // AWS range
		t.Fatal("18.200.0.1 should be flagged as datacenter")
	}
	if !IsDatacenterIP("95.216.44.10") { // Hetzner range
		t.Fatal("95.216.44.10 should be flagged as datacenter")
	}
	for _, ip := range []string{"198.51.100.23", "203.0.113.9", "192.0.2.1", "not-an-ip", ""} {
		if IsDatacenterIP(ip) {
			t.Fatalf("%q should not be flagged as datacenter", ip)
		}
	}

	e := NewEngine()
	dc := req("dc-1")
	dc.Device.IP = "52.10.20.30"
	e.Add(dc)
	e.Add(req("res-1"))

	r := e.Report()
	if r.DatacenterIPs != 1 {
		t.Fatalf("datacenter ips = %d, want 1", r.DatacenterIPs)
	}
	if r.DatacenterIPShare != 0.5 {
		t.Fatalf("datacenter share = %v, want 0.5", r.DatacenterIPShare)
	}
}

func TestUAAnomalies(t *testing.T) {
	if !IsSuspiciousUA("Mozilla/5.0 (compatible; Googlebot/2.1)") {
		t.Fatal("Googlebot should be suspicious")
	}
	if !IsSuspiciousUA("HeadlessChrome/120.0.0.0 Safari/537.36") {
		t.Fatal("HeadlessChrome should be suspicious")
	}
	if !IsSuspiciousUA("curl/8.0.1") {
		t.Fatal("curl should be suspicious")
	}
	if IsSuspiciousUA("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)") {
		t.Fatal("real iPhone UA should not be suspicious")
	}
	if IsSuspiciousUA("") {
		t.Fatal("empty UA should not be suspicious")
	}

	// UA/OS mismatches.
	if !UAOSMismatch("Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "ios") {
		t.Fatal("Windows UA with ios device.os should mismatch")
	}
	if UAOSMismatch("Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "windows") {
		t.Fatal("Windows UA with windows device.os should not mismatch")
	}
	// iPhone UAs contain "like Mac OS X" — the ios token must win.
	if UAOSMismatch("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)", "ios") {
		t.Fatal("iPhone UA with ios device.os should not mismatch")
	}
	// Android UAs contain "Linux" — the android token must win.
	if UAOSMismatch("Mozilla/5.0 (Linux; Android 13; Pixel 7)", "android") {
		t.Fatal("Android UA with android device.os should not mismatch")
	}
	if !UAOSMismatch("Mozilla/5.0 (Linux; Android 13; Pixel 7)", "ios") {
		t.Fatal("Android UA with ios device.os should mismatch")
	}
	// No recognised OS token, or no os set: nothing to contradict.
	if UAOSMismatch("SyntheticAgent/1.0", "android") {
		t.Fatal("unrecognised UA should not mismatch")
	}
	if UAOSMismatch("Mozilla/5.0 (Windows NT 10.0)", "") {
		t.Fatal("empty device.os should not mismatch")
	}

	e := NewEngine()
	bot := req("bot-1")
	bot.Device.UA = "HeadlessChrome/120.0.0.0"
	bot.Device.OS = ""
	e.Add(bot)
	mm := req("mm-1")
	mm.Device.UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
	mm.Device.OS = "ios"
	e.Add(mm)
	plain := req("plain-1")
	plain.Device.UA = ""
	e.Add(plain)

	r := e.Report()
	if r.MissingUA != 1 {
		t.Fatalf("missing ua = %d, want 1", r.MissingUA)
	}
	if r.SuspiciousUA != 1 {
		t.Fatalf("suspicious ua = %d, want 1", r.SuspiciousUA)
	}
	if r.UAOSMismatch != 1 {
		t.Fatalf("ua/os mismatch = %d, want 1", r.UAOSMismatch)
	}
	if r.TopUAShare != 1.0/3 {
		t.Fatalf("top ua share = %v, want 1/3", r.TopUAShare)
	}
}

func TestTMaxDistribution(t *testing.T) {
	e := NewEngine()
	for i, tm := range []int64{100, 200, 300} {
		br := req("t" + string(rune('0'+i)))
		br.TMax = tm
		e.Add(br)
	}
	e.Add(req("t-unset")) // tmax 0 = unset

	r := e.Report()
	if r.TMaxUnset != 1 {
		t.Fatalf("tmax unset = %d, want 1", r.TMaxUnset)
	}
	if r.TMaxMedian != 200 {
		t.Fatalf("tmax median = %v, want 200", r.TMaxMedian)
	}
	if r.TMaxDistinct != 3 {
		t.Fatalf("tmax distinct = %d, want 3", r.TMaxDistinct)
	}
}

func TestRedFlagRules(t *testing.T) {
	flags := func(r Report) map[string]string {
		m := map[string]string{}
		for _, f := range r.RedFlags {
			m[f.Code] = f.Severity
		}
		return m
	}

	// Reused request ids are a high-severity flag on their own.
	e := NewEngine()
	e.Add(req("x"))
	e.Add(req("x"))
	if f := flags(e.Report()); f["duplicate-request-ids"] != SeverityHigh {
		t.Fatalf("flags = %v, want duplicate-request-ids=high", f)
	}

	// No tmax anywhere on a small sample: no-tmax warns.
	e = NewEngine()
	e.Add(req("a"))
	if f := flags(e.Report()); f["no-tmax"] != SeverityWarn {
		t.Fatalf("flags = %v, want no-tmax=warn", f)
	}

	// Missing UA on >50% of a 4-request sample.
	e = NewEngine()
	for _, id := range []string{"u1", "u2", "u3", "u4"} {
		br := req(id)
		if id != "u1" {
			br.Device.UA = ""
		}
		e.Add(br)
	}
	if f := flags(e.Report()); f["missing-ua"] != SeverityWarn {
		t.Fatalf("flags = %v, want missing-ua=warn", f)
	}

	// Too-perfect fill needs n>=20 and full user.id/device.ip/eids.
	e = NewEngine()
	for i := 0; i < 25; i++ {
		br := req("p" + strings.Repeat("0", i%3) + string(rune('a'+i%26)) + string(rune('0'+i/26)))
		br.User.EIDs = []openrtb.EID{{Source: "synthetic.test", IDs: []openrtb.EIDUID{{ID: "e1"}}}}
		br.TMax = 200
		e.Add(br)
	}
	if f := flags(e.Report()); f["too-perfect"] != SeverityWarn {
		t.Fatalf("flags = %v, want too-perfect=warn", f)
	}

	// A realistic-ish healthy sample: no flags at all.
	e = NewEngine()
	uas := []string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)",
		"Mozilla/5.0 (Linux; Android 13; Pixel 7)",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
	}
	oss := []string{"ios", "android", "windows"}
	for i := 0; i < 9; i++ {
		br := req("h" + string(rune('a'+i)))
		br.Device.UA = uas[i%3]
		br.Device.OS = oss[i%3]
		br.Device.IP = "198.51.100." + string(rune('1'+i))
		br.User.ID = ""
		br.TMax = int64(150 + (i%4)*25)
		e.Add(br)
	}
	if f := flags(e.Report()); len(f) != 0 {
		t.Fatalf("healthy sample has flags: %v", f)
	}
}

func TestNilRequest(t *testing.T) {
	e := NewEngine()
	e.Add(nil)
	e.Add(nil)
	r := e.Report()
	if r.Requests != 2 {
		t.Fatalf("requests = %d, want 2", r.Requests)
	}
	if r.NearDuplicates != 0 || r.ExactDuplicates != 0 {
		t.Fatalf("nil requests must not form dup groups: %+v", r)
	}
}
