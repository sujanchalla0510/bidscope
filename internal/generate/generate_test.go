package generate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sujanchalla0510/bidscope/internal/biddable"
	"github.com/sujanchalla0510/bidscope/internal/openrtb"
	"github.com/sujanchalla0510/bidscope/internal/quality"
	"github.com/sujanchalla0510/bidscope/internal/signals"
)

// emitLines generates n requests and returns them as raw JSONL lines.
func emitLines(t *testing.T, cfg Config) []string {
	t.Helper()
	g := New(cfg)
	var buf bytes.Buffer
	if err := g.Emit(&buf); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	return lines
}

func TestValidate(t *testing.T) {
	if err := (Config{}).Validate(); err != nil {
		t.Fatalf("zero config should validate: %v", err)
	}
	if err := (Config{Profile: "bogus"}).Validate(); err == nil {
		t.Fatal("unknown profile should fail validation")
	}
	for _, p := range []Profile{ProfileClean, ProfileMixed, ProfileDirty} {
		if err := (Config{Profile: p}).Validate(); err != nil {
			t.Fatalf("profile %q should validate: %v", p, err)
		}
	}
}

func TestDeterministic(t *testing.T) {
	cfg := Config{N: 300, Seed: 7, Profile: ProfileMixed}
	a := emitLines(t, cfg)
	b := emitLines(t, cfg)
	if len(a) != len(b) {
		t.Fatalf("line count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("line %d differs across identical runs", i)
		}
	}
}

func TestDifferentSeedsDiffer(t *testing.T) {
	a := emitLines(t, Config{N: 200, Seed: 7, Profile: ProfileMixed})
	b := emitLines(t, Config{N: 200, Seed: 8, Profile: ProfileMixed})
	same := 0
	for i := range a {
		if a[i] == b[i] {
			same++
		}
	}
	if same == len(a) {
		t.Fatal("different seeds produced identical output")
	}
}

func TestLinesAreValidBidRequests(t *testing.T) {
	for _, p := range []Profile{ProfileClean, ProfileMixed, ProfileDirty} {
		for _, line := range emitLines(t, Config{N: 200, Seed: 11, Profile: p}) {
			var br openrtb.BidRequest
			if err := json.Unmarshal([]byte(line), &br); err != nil {
				t.Fatalf("profile %q: invalid JSON: %v", p, err)
			}
			if br.ID == "" || len(br.Imp) == 0 {
				t.Fatalf("profile %q: request missing id/imp: %s", p, line)
			}
			if v := openrtb.DetectVersion(&br); v != openrtb.Version25 && v != openrtb.Version26 {
				t.Fatalf("profile %q: undetected version for %s", p, br.ID)
			}
		}
	}
}

func TestZeroNEmitsNothing(t *testing.T) {
	g := New(Config{N: 0, Seed: 7, Profile: ProfileMixed})
	var buf bytes.Buffer
	if err := g.Emit(&buf); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty output, got %d bytes", buf.Len())
	}
}

// TestCleanProfileIsFullyBiddable pins the core promise of the clean
// profile: a healthy SSP's sample passes every biddability gate.
func TestCleanProfileIsFullyBiddable(t *testing.T) {
	lines := emitLines(t, Config{N: 400, Seed: 7, Profile: ProfileClean})
	eng := biddable.NewEngine()
	for _, line := range lines {
		var br openrtb.BidRequest
		if err := json.Unmarshal([]byte(line), &br); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		eng.Add(&br)
	}
	rep := eng.Report(0)
	if rep.Biddable != rep.Requests {
		t.Fatalf("clean profile: %d/%d biddable", rep.Biddable, rep.Requests)
	}
	for _, gr := range rep.Gates {
		if gr.Failed != 0 {
			t.Fatalf("clean profile: gate %q failed %d request(s)", gr.ID, gr.Failed)
		}
	}
	qeng := quality.NewEngine()
	for _, line := range lines {
		var br openrtb.BidRequest
		_ = json.Unmarshal([]byte(line), &br)
		qeng.Add(&br)
	}
	qrep := qeng.Report()
	if qrep.ExactDuplicates != 0 || qrep.NearDuplicates != 0 {
		t.Fatalf("clean profile: duplicates present (exact=%d near=%d)",
			qrep.ExactDuplicates, qrep.NearDuplicates)
	}
	if qrep.DatacenterIPs != 0 || qrep.SuspiciousUA != 0 || qrep.UAOSMismatch != 0 {
		t.Fatal("clean profile: quality defects present")
	}
}

// TestDirtyProfileTripsGates verifies the dirty profile actually exercises
// the duplicate, datacenter, UA, and signal-gap machinery.
func TestDirtyProfileTripsGates(t *testing.T) {
	lines := emitLines(t, Config{N: 600, Seed: 7, Profile: ProfileDirty})
	beng := biddable.NewEngine()
	qeng := quality.NewEngine()
	seng := signals.NewEngine()
	var missGeo int
	for _, line := range lines {
		var br openrtb.BidRequest
		if err := json.Unmarshal([]byte(line), &br); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		beng.Add(&br)
		qeng.Add(&br)
		seng.Add(&br)
		if br.Device == nil || br.Device.Geo == nil {
			missGeo++
		}
	}
	brep := beng.Report(0)
	if brep.BiddableShare >= 0.75 {
		t.Fatalf("dirty profile too clean: biddable share %.3f", brep.BiddableShare)
	}
	if got := beng.GateFails(biddable.Duplicate); got == 0 {
		t.Fatal("dirty profile: expected exact-duplicate gate failures")
	}
	if got := beng.GateFails(biddable.Datacenter); got == 0 {
		t.Fatal("dirty profile: expected datacenter gate failures")
	}
	if got := beng.GateFails(biddable.UserAgent); got == 0 {
		t.Fatal("dirty profile: expected user-agent gate failures")
	}
	qrep := qeng.Report()
	if qrep.ExactDuplicates == 0 {
		t.Fatal("dirty profile: expected exact duplicates")
	}
	if qrep.NearDuplicateGroups == 0 {
		t.Fatal("dirty profile: expected near-duplicate groups")
	}
	if qrep.DatacenterIPs == 0 {
		t.Fatal("dirty profile: expected datacenter IPs")
	}
	if qrep.SuspiciousUA == 0 {
		t.Fatal("dirty profile: expected suspicious user agents")
	}
	if missGeo == 0 {
		t.Fatal("dirty profile: expected missing geo")
	}
	if got := seng.FillRate("geo"); got >= 1.0 {
		t.Fatalf("dirty profile: geo fill rate %.3f, expected < 1", got)
	}
}

// TestMixedProfileBetweenCleanAndDirty orders the profiles by biddability.
func TestMixedProfileBetweenCleanAndDirty(t *testing.T) {
	share := func(p Profile) float64 {
		eng := biddable.NewEngine()
		for _, line := range emitLines(t, Config{N: 600, Seed: 7, Profile: p}) {
			var br openrtb.BidRequest
			_ = json.Unmarshal([]byte(line), &br)
			eng.Add(&br)
		}
		return eng.Report(0).BiddableShare
	}
	clean, mixed, dirty := share(ProfileClean), share(ProfileMixed), share(ProfileDirty)
	if !(clean >= mixed && mixed >= dirty) {
		t.Fatalf("profile ordering broken: clean=%.3f mixed=%.3f dirty=%.3f", clean, mixed, dirty)
	}
	if clean != 1.0 {
		t.Fatalf("clean profile share %.3f, want 1.0", clean)
	}
	if dirty >= 0.75 {
		t.Fatalf("dirty profile share %.3f, want < 0.75", dirty)
	}
}

// TestCleanHasNoUAOSMismatch guards the UA/os pairing logic: the clean
// profile must never emit a mismatched pair (dirty may, deliberately).
func TestCleanHasNoUAOSMismatch(t *testing.T) {
	for _, line := range emitLines(t, Config{N: 400, Seed: 7, Profile: ProfileClean}) {
		var br openrtb.BidRequest
		_ = json.Unmarshal([]byte(line), &br)
		if br.Device == nil {
			t.Fatal("clean profile: request without device")
		}
		if quality.UAOSMismatch(br.Device.UA, br.Device.OS) {
			t.Fatalf("clean profile: UA/os mismatch: %q / %q", br.Device.UA, br.Device.OS)
		}
		if quality.IsSuspiciousUA(br.Device.UA) {
			t.Fatalf("clean profile: suspicious UA: %q", br.Device.UA)
		}
		if quality.IsDatacenterIP(br.Device.IP) {
			t.Fatalf("clean profile: datacenter IP: %q", br.Device.IP)
		}
	}
}

// TestSyntheticMarkers documents the fixture contract: generator IPs are
// TEST-NET documentation ranges, inventory is example.com.
func TestSyntheticMarkers(t *testing.T) {
	for _, line := range emitLines(t, Config{N: 200, Seed: 7, Profile: ProfileClean}) {
		var br openrtb.BidRequest
		_ = json.Unmarshal([]byte(line), &br)
		ip := br.Device.IP
		if !strings.HasPrefix(ip, "198.51.100.") && !strings.HasPrefix(ip, "203.0.113.") {
			t.Fatalf("non-synthetic IP emitted: %q", ip)
		}
		if br.Site != nil && !strings.HasSuffix(br.Site.Domain, ".example") {
			t.Fatalf("non-synthetic domain emitted: %q", br.Site.Domain)
		}
		if br.App != nil && !strings.HasPrefix(br.App.Bundle, "com.example.") {
			t.Fatalf("non-synthetic bundle emitted: %q", br.App.Bundle)
		}
	}
}
