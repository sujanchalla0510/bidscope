// Package generate synthesizes OpenRTB bid-request samples for testing and
// demos: `bidscope -generate | bidscope -in -` is the zero-setup way to see
// every analyzer light up, and the upcoming web tester uses it as its
// "try sample data" source.
//
// Everything this package emits is fake: TEST-NET IPs (198.51.100.0/24,
// 203.0.113.0/24), example.com inventory, random identifiers. Never feed it
// real data, never treat its output as real traffic, and never commit its
// output as a fixture of anything but tests. Deterministic: the same
// Config always produces the same bytes.
//
// Profiles control the degradation mix so the generator doubles as a
// profiler self-test: "clean" is a healthy SSP (every request biddable),
// "mixed" is a realistic stream with some rot, and "dirty" is junk
// (duplicates, datacenter IPs, bot user agents, missing signals) that
// should trip every quality gate.
package generate

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// Profile selects the degradation mix the generator emits.
type Profile string

// The supported generation profiles.
const (
	ProfileClean Profile = "clean" // healthy SSP: everything filled, no junk
	ProfileMixed Profile = "mixed" // realistic: mostly fine, some rot (default)
	ProfileDirty Profile = "dirty" // junk: duplicates, datacenter IPs, bot UAs, gaps
)

// Config controls synthetic generation.
type Config struct {
	N       int     // requests to emit; default 1000
	Seed    int64   // RNG seed; default 7
	Profile Profile // degradation mix; default ProfileMixed
}

// withDefaults fills unset fields with their defaults.
func (c Config) withDefaults() Config {
	if c.N < 0 {
		c.N = 0
	}
	if c.N == 0 {
		// Distinguish "unset" from "explicitly zero": both emit nothing;
		// the default only matters for CLI ergonomics.
	}
	if c.Profile == "" {
		c.Profile = ProfileMixed
	}
	return c
}

// DefaultN is the request count used when the CLI does not specify -n.
const DefaultN = 1000

// DefaultSeed is the RNG seed used when the CLI does not specify -seed.
const DefaultSeed = 7

// Validate reports whether the config is usable.
func (c Config) Validate() error {
	c = c.withDefaults()
	switch c.Profile {
	case ProfileClean, ProfileMixed, ProfileDirty:
		// ok
	default:
		return fmt.Errorf("unknown profile %q: want clean, mixed, or dirty", c.Profile)
	}
	return nil
}

// rates is the per-request degradation mix for one profile.
type rates struct {
	dupExact    float64 // emit a byte-identical replay of an earlier request
	dupNear     float64 // same device/user/inventory fingerprint, new id
	missGeo     float64 // device.geo dropped
	missIdent   float64 // device.ip/ifa and user.id dropped
	missUA      float64 // device.ua dropped
	suspicious  float64 // bot-like UA (curl, headless, ...)
	uaMismatch  float64 // UA family contradicts device.os
	datacenter  float64 // device.ip in a cloud range
	noInventory float64 // neither site nor app set
}

var profileRates = map[Profile]rates{
	ProfileClean: {},
	ProfileMixed: {
		dupExact: 0.08, dupNear: 0.04,
		missGeo: 0.06, missIdent: 0.04, missUA: 0.03,
		suspicious: 0.03, uaMismatch: 0.02, datacenter: 0.05,
		noInventory: 0.01,
	},
	ProfileDirty: {
		dupExact: 0.20, dupNear: 0.10,
		missGeo: 0.15, missIdent: 0.12, missUA: 0.05,
		suspicious: 0.08, uaMismatch: 0.05, datacenter: 0.12,
		noInventory: 0.05,
	},
}

// Generator emits synthetic bid requests.
type Generator struct {
	cfg  Config
	rng  *rand.Rand
	r    rates
	hist []*openrtb.BidRequest // emitted requests, for duplicate injection
}

// New returns a Generator for cfg, applying defaults. Validate the config
// first; an invalid profile yields zeroed rates, which is safe but useless.
func New(cfg Config) *Generator {
	cfg = cfg.withDefaults()
	return &Generator{cfg: cfg, rng: rand.New(rand.NewSource(cfg.Seed)), r: profileRates[cfg.Profile]}
}

// Emit writes cfg.N bid requests as JSONL to w.
func (g *Generator) Emit(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i := 0; i < g.cfg.N; i++ {
		if err := enc.Encode(g.Next()); err != nil {
			return err
		}
	}
	return nil
}

// replayCap bounds the duplicate-injection history so huge generations
// don't pin every request in memory.
const replayCap = 8192

// trimHistory drops the oldest half of the replay pool once it exceeds the
// cap. Length-gated (never content-gated) so generation stays deterministic.
func (g *Generator) trimHistory() {
	if len(g.hist) > replayCap {
		g.hist = append([]*openrtb.BidRequest(nil), g.hist[len(g.hist)-replayCap/2:]...)
	}
}

// Next builds one synthetic bid request.
func (g *Generator) Next() *openrtb.BidRequest {
	if len(g.hist) > 0 {
		switch {
		case g.rng.Float64() < g.r.dupExact:
			return deepCopy(g.hist[g.rng.Intn(len(g.hist))])
		case g.rng.Float64() < g.r.dupNear:
			return g.nearDuplicate()
		}
	}
	br := g.base(len(g.hist))
	g.applyDegradations(br)
	g.hist = append(g.hist, br)
	g.trimHistory()
	return br
}

// nearDuplicate re-emits an earlier request's actor fingerprint (device,
// user, inventory) with fresh auction specifics: the quality engine should
// count it as a near-duplicate, not an exact one.
func (g *Generator) nearDuplicate() *openrtb.BidRequest {
	src := g.hist[g.rng.Intn(len(g.hist))]
	br := deepCopy(src)
	i := len(g.hist)
	br.ID = fmt.Sprintf("req-%d", i)
	br.TMax = pickInt64(g.rng, []int64{100, 120, 120, 150, 200})
	for k := range br.Imp {
		br.Imp[k].ID = fmt.Sprintf("imp-%d", i)
		br.Imp[k].BidFloor = round2(0.2 + g.rng.Float64()*5.8)
	}
	g.hist = append(g.hist, br)
	g.trimHistory()
	return br
}

// deepCopy clones a request through JSON so replays are byte-identical on
// re-marshal (the duplicate gates hash the serialized payload).
func deepCopy(br *openrtb.BidRequest) *openrtb.BidRequest {
	raw, err := json.Marshal(br)
	if err != nil {
		return &openrtb.BidRequest{}
	}
	out := &openrtb.BidRequest{}
	if err := json.Unmarshal(raw, out); err != nil {
		return &openrtb.BidRequest{}
	}
	return out
}

// base builds a healthy request; degradations are applied separately.
func (g *Generator) base(i int) *openrtb.BidRequest {
	android := g.rng.Float64() < 0.6
	br := &openrtb.BidRequest{
		ID:   fmt.Sprintf("req-%d", i),
		At:   1,
		TMax: pickInt64(g.rng, []int64{100, 120, 120, 150, 200}),
		Imp:  []openrtb.Imp{g.imp(i)},
	}

	// Inventory: mostly site, some app.
	if g.rng.Float64() < 0.85 {
		domain := pick(g.rng, []string{"news.example", "sports.example", "shop.example", "weather.example"})
		br.Site = &openrtb.Site{
			Domain:    domain,
			Page:      fmt.Sprintf("https://%s/article-%d", domain, i),
			Publisher: &openrtb.Publisher{ID: "pub-1"},
		}
	} else {
		br.App = &openrtb.App{
			Bundle:    pick(g.rng, []string{"com.example.news", "com.example.sports", "com.example.shop"}),
			Publisher: &openrtb.Publisher{ID: "pub-1"},
		}
	}

	os := "android"
	ua := androidUA
	osv := "13"
	if !android {
		os = "ios"
		ua = iosUA
		osv = "17"
	}
	geo := &openrtb.Geo{Country: pickWeighted(g.rng,
		[]string{"USA", "GBR", "DEU"}, []float64{50, 30, 20})}
	if g.rng.Float64() < 0.3 {
		geo.Lat = round2(-90 + g.rng.Float64()*180)
		geo.Lon = round2(-180 + g.rng.Float64()*360)
	}
	br.Device = &openrtb.Device{
		OS:         os,
		OSV:        osv,
		IP:         fmt.Sprintf("%s.%d", pick(g.rng, []string{"198.51.100", "203.0.113"}), 1+g.rng.Intn(240)),
		IFA:        fmt.Sprintf("ifa-%d", i),
		UA:         ua,
		DeviceType: 4,
		Geo:        geo,
	}
	if g.rng.Float64() < 0.05 {
		br.Device.DeviceType = 5 // tablet
	}

	br.User = &openrtb.User{ID: fmt.Sprintf("u-%d", i)}
	if g.rng.Float64() < 0.3 {
		br.User.EIDs = []openrtb.EID{{
			Source: "liveramp.example",
			IDs:    []openrtb.EIDUID{{ID: fmt.Sprintf("eid-%d", i)}},
		}}
	}

	if g.rng.Float64() < 0.45 {
		br.Regs = &openrtb.Regs{GDPR: 1, GPP: "DBABMA~BAAAAA~1~AAAAA"}
	}
	if g.rng.Float64() < 0.4 {
		ext, _ := json.Marshal(map[string]any{"schain": map[string]any{
			"complete": 1,
			"ver":      "1.0",
			"nodes": []map[string]any{
				{"asi": "ssp.example", "sid": "pub-1", "hp": 1},
			},
		}})
		br.Source = &openrtb.Source{FD: 1, Ext: ext}
	}
	return br
}

// imp builds one healthy impression: banner mostly, some video/native;
// a few carry rewarded/SSAI flags so 2.6 version detection gets exercised.
func (g *Generator) imp(i int) openrtb.Imp {
	imp := openrtb.Imp{
		ID:          "imp-1",
		BidFloor:    round2(0.2 + g.rng.Float64()*5.8),
		BidFloorCur: "USD",
	}
	switch r := g.rng.Float64(); {
	case r < 0.85:
		size := [2]int{300, 250}
		if g.rng.Float64() < 0.25 {
			size = pick(g.rng, [][2]int{{728, 90}, {320, 50}, {160, 600}})
		}
		imp.Banner = &openrtb.Banner{Format: []openrtb.Format{{W: size[0], H: size[1]}}}
	case r < 0.95:
		imp.Video = &openrtb.Video{Mimes: []string{"video/mp4"}, W: 640, H: 480}
	default:
		imp.Native = &openrtb.Native{Request: fmt.Sprintf("{\"native\":%d}", i)}
	}
	if g.rng.Float64() < 0.12 {
		imp.PMP = &openrtb.PMP{Deals: []openrtb.Deal{{
			ID:       fmt.Sprintf("deal-%d", 1+g.rng.Intn(5)),
			BidFloor: round2(1.0 + g.rng.Float64()*4.0),
			At:       1,
		}}}
		if g.rng.Float64() < 0.5 {
			imp.PMP.PrivateAuction = 1
		}
	}
	if g.rng.Float64() < 0.05 {
		imp.Rwdd = 1
	}
	if g.rng.Float64() < 0.03 {
		imp.Ssai = 1
	}
	return imp
}

// applyDegradations rolls each defect independently against the healthy
// base. A request can carry several defects at once.
func (g *Generator) applyDegradations(br *openrtb.BidRequest) {
	r := g.r
	if g.rng.Float64() < r.noInventory {
		br.Site, br.App = nil, nil
	}
	if br.Device == nil {
		return
	}
	if g.rng.Float64() < r.missGeo {
		br.Device.Geo = nil
	}
	if g.rng.Float64() < r.missIdent {
		br.Device.IP, br.Device.IPv6, br.Device.IFA = "", "", ""
		br.User = nil
	}
	if g.rng.Float64() < r.missUA {
		br.Device.UA = ""
	} else if g.rng.Float64() < r.suspicious {
		br.Device.UA = pick(g.rng, []string{"curl/8.0.1", "python-requests/2.31", "HeadlessChrome/120.0"})
	} else if g.rng.Float64() < r.uaMismatch {
		// UA family contradicts device.os: android UA on an "ios" device.
		br.Device.OS = "ios"
		br.Device.UA = androidUA
	}
	if g.rng.Float64() < r.datacenter {
		br.Device.IP = fmt.Sprintf("3.%d.%d.%d", g.rng.Intn(256), g.rng.Intn(256), 1+g.rng.Intn(254))
	}
}

const androidUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"
const iosUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

func pick[T any](rng *rand.Rand, xs []T) T { return xs[rng.Intn(len(xs))] }

func pickInt64(rng *rand.Rand, xs []int64) int64 { return xs[rng.Intn(len(xs))] }

func pickWeighted(rng *rand.Rand, xs []string, weights []float64) string {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	x := rng.Float64() * total
	for i, w := range weights {
		x -= w
		if x <= 0 {
			return xs[i]
		}
	}
	return xs[len(xs)-1]
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
