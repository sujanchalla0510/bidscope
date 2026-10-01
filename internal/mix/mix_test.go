package mix

import (
	"math"
	"testing"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// req builds a synthetic bid request for mix tests: os/device-type/country
// on the device, site-vs-app inventory, and one imp per format in formats
// with the given floors and PMP settings.
func req(osName string, dt int, country string, site bool, formats []string, floor float64, pmp bool, deals int, private bool) *openrtb.BidRequest {
	br := &openrtb.BidRequest{
		ID: "r",
		Device: &openrtb.Device{
			OS:         osName,
			DeviceType: dt,
			Geo:        &openrtb.Geo{Country: country},
		},
		Imp: []openrtb.Imp{},
	}
	if site {
		br.Site = &openrtb.Site{Domain: "example.com"}
	} else {
		br.App = &openrtb.App{Bundle: "com.example.app"}
	}
	for _, f := range formats {
		imp := openrtb.Imp{ID: "imp", BidFloor: floor}
		switch f {
		case "banner":
			imp.Banner = &openrtb.Banner{}
		case "video":
			imp.Video = &openrtb.Video{}
		case "native":
			imp.Native = &openrtb.Native{}
		case "audio":
			imp.Audio = &openrtb.Audio{}
		}
		if pmp {
			imp.PMP = &openrtb.PMP{}
			if private {
				imp.PMP.PrivateAuction = 1
			}
			for i := 0; i < deals; i++ {
				imp.PMP.Deals = append(imp.PMP.Deals, openrtb.Deal{ID: "d"})
			}
		}
		br.Imp = append(br.Imp, imp)
	}
	return br
}

func TestEmptyEngine(t *testing.T) {
	e := NewEngine()
	r := e.Report()
	if r.Requests != 0 || r.Impressions != 0 {
		t.Fatalf("empty engine should report zeros, got %+v", r)
	}
	if r.SiteShare != 0 || r.PMPCoverage != 0 || r.FloorMedian != 0 || r.FloorP90 != 0 {
		t.Fatalf("empty engine shares/floors should be 0, got %+v", r)
	}
	if e.Count() != 0 || e.ImpCount() != 0 {
		t.Fatalf("empty engine counts wrong: %d requests, %d imps", e.Count(), e.ImpCount())
	}
}

func TestNilRequestCounted(t *testing.T) {
	e := NewEngine()
	e.Add(nil)
	e.Add(nil)
	if e.Count() != 2 {
		t.Fatalf("nil requests should be counted, got %d", e.Count())
	}
	r := e.Report()
	if r.Neither != 0 || r.Requests != 2 {
		t.Fatalf("nil requests should carry no inventory attrs, got %+v", r)
	}
}

func TestFormatsPerImpression(t *testing.T) {
	e := NewEngine()
	e.Add(req("iOS", 4, "US", true, []string{"banner", "video"}, 1.0, false, 0, false))
	if e.ImpCount() != 2 {
		t.Fatalf("expected 2 imps, got %d", e.ImpCount())
	}
	r := e.Report()
	got := map[string]int{}
	for _, it := range r.Formats {
		got[it.Label] = it.Count
	}
	if got["banner"] != 1 || got["video"] != 1 {
		t.Fatalf("format counts wrong: %+v", got)
	}
	for _, it := range r.Formats {
		if it.Share != 0.5 {
			t.Fatalf("format share should be 0.5 of 2 imps, got %+v", r.Formats)
		}
	}
}

func TestUnknownFormat(t *testing.T) {
	e := NewEngine()
	e.Add(req("android", 1, "US", true, []string{"other"}, 0, false, 0, false))
	r := e.Report()
	if len(r.Formats) != 1 || r.Formats[0].Label != "unknown" {
		t.Fatalf("imp with no format markers should be 'unknown', got %+v", r.Formats)
	}
}

func TestSiteVsApp(t *testing.T) {
	e := NewEngine()
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, false, 0, false))
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, false, 0, false))
	e.Add(req("android", 1, "GB", false, []string{"banner"}, 1, false, 0, false))
	r := e.Report()
	if r.Site != 2 || r.App != 1 || r.Neither != 0 {
		t.Fatalf("site/app/neither wrong: %+v", r)
	}
	if math.Abs(r.SiteShare-2.0/3.0) > 1e-9 || math.Abs(r.AppShare-1.0/3.0) > 1e-9 {
		t.Fatalf("site/app shares wrong: %f %f", r.SiteShare, r.AppShare)
	}
}

func TestDistributions(t *testing.T) {
	e := NewEngine()
	// 3 iOS/US/phone, 1 android/GB/tablet
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, false, 0, false))
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, false, 0, false))
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, false, 0, false))
	e.Add(req("android", 5, "GB", true, []string{"banner"}, 1, false, 0, false))
	r := e.Report()
	if len(r.TopOS) != 2 || r.TopOS[0].Label != "iOS" || r.TopOS[0].Count != 3 {
		t.Fatalf("top OS wrong: %+v", r.TopOS)
	}
	if math.Abs(r.TopOS[0].Share-0.75) > 1e-9 {
		t.Fatalf("iOS share should be 0.75, got %f", r.TopOS[0].Share)
	}
	if len(r.TopCountries) != 2 || r.TopCountries[0].Label != "US" || r.TopCountries[0].Count != 3 {
		t.Fatalf("top countries wrong: %+v", r.TopCountries)
	}
	labels := map[string]bool{}
	for _, it := range r.DeviceTypes {
		labels[it.Label] = true
	}
	if !labels["phone"] || !labels["tablet"] {
		t.Fatalf("device type labels wrong: %+v", r.DeviceTypes)
	}
}

func TestPMPCoverage(t *testing.T) {
	e := NewEngine()
	// 4 imps: 1 no pmp, 1 pmp w/o deals, 1 pmp w/ 2 deals, 1 private w/ 1 deal
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, false, 0, false))
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, true, 0, false))
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, true, 2, false))
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 1, true, 1, true))
	r := e.Report()
	if math.Abs(r.PMPCoverage-0.75) > 1e-9 {
		t.Fatalf("pmp coverage should be 0.75, got %f", r.PMPCoverage)
	}
	if math.Abs(r.DealCoverage-0.5) > 1e-9 {
		t.Fatalf("deal coverage should be 0.5, got %f", r.DealCoverage)
	}
	if math.Abs(r.PrivateShare-0.25) > 1e-9 {
		t.Fatalf("private share should be 0.25, got %f", r.PrivateShare)
	}
}

func TestFloorPercentiles(t *testing.T) {
	e := NewEngine()
	// floors 1..10 across 10 single-imp requests; a zero floor is excluded
	for i := 1; i <= 10; i++ {
		e.Add(req("iOS", 4, "US", true, []string{"banner"}, float64(i), false, 0, false))
	}
	e.Add(req("iOS", 4, "US", true, []string{"banner"}, 0, false, 0, false))
	r := e.Report()
	if r.BidFloors != 10 {
		t.Fatalf("expected 10 floors (zero excluded), got %d", r.BidFloors)
	}
	// nearest-rank: median of 1..10 -> rank ceil(0.5*10)=5 -> value 5
	if r.FloorMedian != 5 {
		t.Fatalf("median should be 5, got %f", r.FloorMedian)
	}
	// p90 -> rank ceil(0.9*10)=9 -> value 9
	if r.FloorP90 != 9 {
		t.Fatalf("p90 should be 9, got %f", r.FloorP90)
	}
}

func TestPercentileEdgeCases(t *testing.T) {
	if percentile(nil, 0.5) != 0 {
		t.Fatal("empty percentile should be 0")
	}
	if percentile([]float64{7}, 0.99) != 7 || percentile([]float64{7}, 0) != 7 {
		t.Fatal("single-element percentile should be the element")
	}
	if percentile([]float64{2, 8}, 0.5) != 2 {
		t.Fatal("median of [2 8] nearest-rank should be 2")
	}
}

func TestTopNCapped(t *testing.T) {
	e := NewEngine()
	for i := 0; i < TopN+3; i++ {
		e.Add(req(osName(i), 4, "US", true, []string{"banner"}, 1, false, 0, false))
	}
	r := e.Report()
	if len(r.TopOS) != TopN {
		t.Fatalf("top OS should be capped at %d, got %d", TopN, len(r.TopOS))
	}
}

func osName(i int) string {
	return "os" + string(rune('a'+i%26))
}
