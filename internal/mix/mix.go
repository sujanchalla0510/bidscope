// Package mix analyses the inventory mix of a bidstream sample.
//
// Where signal completeness (internal/signals) asks "do the requests carry
// the signals a DSP needs?", mix asks "what is this supply made of?":
// device/OS and geo distributions, the creative formats on offer, the
// site-vs-app split, how much traffic flows through PMP deals, and the
// bidfloor distribution. Together they tell a buyer whether the inventory
// is worth bidding on before they sign an SSP integration.
//
// Everything is counted per bid request except formats, PMP, and bid
// floors, which are counted per impression (a request usually carries one
// imp, but auction bundling means it can carry several).
//
// All payloads BidScope ships with are synthetic; this package never emits
// anything on the network — analysis is purely local.
package mix

import (
	"math"
	"sort"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// TopN caps how many rows distribution tables keep. The tail of these
// distributions is a long tail of one-offs that adds noise, not insight.
const TopN = 8

// Item is one row of a distribution table: a label, its count, and its
// share of the relevant denominator (0–1).
type Item struct {
	Label string  `json:"label"`
	Count int     `json:"count"`
	Share float64 `json:"share"`
}

// deviceTypeLabels maps OpenRTB device.devicetype codes to short labels.
var deviceTypeLabels = map[int]string{
	1: "mobile/tablet",
	2: "pc",
	3: "connected-tv",
	4: "phone",
	5: "tablet",
	6: "connected-device",
	7: "set-top-box",
}

// formatOf classifies one impression by its creative format. An imp can
// technically carry several formats; this reports each one it carries so
// the shares reflect supply composition, not an exclusive split.
func formatOf(imp *openrtb.Imp) []string {
	var out []string
	if imp.Banner != nil {
		out = append(out, "banner")
	}
	if imp.Video != nil {
		out = append(out, "video")
	}
	if imp.Native != nil {
		out = append(out, "native")
	}
	if imp.Audio != nil {
		out = append(out, "audio")
	}
	if len(out) == 0 {
		out = append(out, "unknown")
	}
	return out
}

// Engine accumulates mix statistics across a bidstream sample.
type Engine struct {
	n          int // bid requests folded in
	imps       int // impressions folded in
	formats    map[string]int
	os         map[string]int
	deviceType map[int]int
	country    map[string]int
	site       int
	app        int
	neither    int
	pmpImps    int
	dealImps   int
	privateImp int
	floors     []float64
}

// NewEngine returns an empty engine.
func NewEngine() *Engine {
	return &Engine{
		formats:    make(map[string]int),
		os:         make(map[string]int),
		deviceType: make(map[int]int),
		country:    make(map[string]int),
	}
}

// Add folds one bid request into the engine. A nil request is counted as a
// request with no inventory attributes (it still degrades every share).
func (e *Engine) Add(br *openrtb.BidRequest) {
	e.n++
	if br == nil {
		return
	}

	// Request-level attributes: device, geo, site-vs-app.
	if br.Device != nil {
		if br.Device.OS != "" {
			e.os[br.Device.OS]++
		}
		if br.Device.DeviceType != 0 {
			e.deviceType[br.Device.DeviceType]++
		}
		if br.Device.Geo != nil && br.Device.Geo.Country != "" {
			e.country[br.Device.Geo.Country]++
		}
	}
	switch {
	case br.Site != nil:
		e.site++
	case br.App != nil:
		e.app++
	default:
		e.neither++
	}

	// Impression-level attributes: formats, PMP, bid floors.
	for i := range br.Imp {
		imp := &br.Imp[i]
		e.imps++
		for _, f := range formatOf(imp) {
			e.formats[f]++
		}
		if imp.PMP != nil {
			e.pmpImps++
			if len(imp.PMP.Deals) > 0 {
				e.dealImps++
			}
			if imp.PMP.PrivateAuction == 1 {
				e.privateImp++
			}
		}
		if imp.BidFloor > 0 {
			e.floors = append(e.floors, imp.BidFloor)
		}
	}
}

// Count returns the number of bid requests folded in so far.
func (e *Engine) Count() int { return e.n }

// ImpCount returns the number of impressions folded in so far.
func (e *Engine) ImpCount() int { return e.imps }

// topRows turns a count map into a share-ranked, TopN-capped table.
func topRows[T comparable](counts map[T]int, denom int, label func(T) string) []Item {
	if denom == 0 {
		return nil
	}
	type kv struct {
		k T
		v int
	}
	all := make([]kv, 0, len(counts))
	for k, v := range counts {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return label(all[i].k) < label(all[j].k)
	})
	if len(all) > TopN {
		all = all[:TopN]
	}
	out := make([]Item, 0, len(all))
	for _, kv := range all {
		out = append(out, Item{
			Label: label(kv.k),
			Count: kv.v,
			Share: float64(kv.v) / float64(denom),
		})
	}
	return out
}

// Report is the full mix report over the folded sample.
type Report struct {
	Requests     int     `json:"requests"`
	Impressions  int     `json:"impressions"`
	Formats      []Item  `json:"formats"`       // per-impression creative formats
	TopOS        []Item  `json:"top_os"`        // per-request, TopN-capped
	TopCountries []Item  `json:"top_countries"` // per-request, TopN-capped
	DeviceTypes  []Item  `json:"device_types"`  // per-request device class mix
	Site         int     `json:"site"`
	App          int     `json:"app"`
	Neither      int     `json:"neither"`
	SiteShare    float64 `json:"site_share"`    // of requests
	AppShare     float64 `json:"app_share"`     // of requests
	PMPCoverage  float64 `json:"pmp_coverage"`  // share of imps with pmp set
	DealCoverage float64 `json:"deal_coverage"` // share of imps with >=1 deal
	PrivateShare float64 `json:"private_share"` // share of imps flagged privateauction
	BidFloors    int     `json:"bidfloors"`     // imps with bidfloor > 0
	FloorMedian  float64 `json:"floor_median"`
	FloorP90     float64 `json:"floor_p90"`
}

// percentile returns the q-th percentile (0–1, inclusive) of sorted vals
// using the nearest-rank method; 0 on empty input.
func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// Report returns the mix report. Shares are 0 with no denominator; the
// floor percentiles are 0 when no impression carried a bid floor.
func (e *Engine) Report() Report {
	r := Report{
		Requests:    e.n,
		Impressions: e.imps,
		Site:        e.site,
		App:         e.app,
		Neither:     e.neither,
		BidFloors:   len(e.floors),
	}
	if e.n > 0 {
		r.SiteShare = float64(e.site) / float64(e.n)
		r.AppShare = float64(e.app) / float64(e.n)
	}
	r.Formats = topRows(e.formats, e.imps, func(s string) string { return s })
	r.TopOS = topRows(e.os, e.n, func(s string) string { return s })
	r.TopCountries = topRows(e.country, e.n, func(s string) string { return s })
	r.DeviceTypes = topRows(e.deviceType, e.n, func(dt int) string {
		if l, ok := deviceTypeLabels[dt]; ok {
			return l
		}
		return "unknown"
	})
	if e.imps > 0 {
		r.PMPCoverage = float64(e.pmpImps) / float64(e.imps)
		r.DealCoverage = float64(e.dealImps) / float64(e.imps)
		r.PrivateShare = float64(e.privateImp) / float64(e.imps)
	}
	if len(e.floors) > 0 {
		f := append([]float64(nil), e.floors...)
		sort.Float64s(f)
		r.FloorMedian = percentile(f, 0.5)
		r.FloorP90 = percentile(f, 0.9)
	}
	return r
}
