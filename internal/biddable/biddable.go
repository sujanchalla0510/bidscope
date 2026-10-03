// Package biddable estimates the share of a bidstream sample that is
// actually biddable — and, given a stated input QPS, turns that share into
// an absolute biddable-QPS figure.
//
// A bid request is biddable when a DSP could plausibly spend on it: it is
// not a replay of an earlier payload, it identifies the inventory, it
// carries a usable identity and geo signal, its user agent is real, and
// its IP is not datacenter traffic. Each gate is deliberately narrow and
// documented: the per-gate failure counts show exactly where a sample's
// QPS is lost, which is the diagnostic a DSP runs before signing an SSP
// integration.
//
// The estimate is a model over the sample, not a guarantee — and it is
// computed entirely locally: no data ever leaves the machine.
package biddable

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
	"github.com/sujanchalla0510/bidscope/internal/quality"
)

// GateID identifies one biddability gate.
type GateID string

// The six gates, in evaluation order.
const (
	Duplicate  GateID = "duplicate"  // exact replay of an earlier payload
	Inventory  GateID = "inventory"  // site.domain or app.bundle identifies the slot
	Identity   GateID = "identity"   // device.ip/ipv6, device.ifa, or user.id
	Geo        GateID = "geo"        // device.geo with country+ or lat+lon
	UserAgent  GateID = "useragent"  // real, consistent user agent
	Datacenter GateID = "datacenter" // IP outside the datacenter ranges
)

// Gate pairs a gate id with its human label and its predicate. A request
// is biddable when no gate's predicate reports a failure.
type Gate struct {
	ID    GateID
	Label string
	Fails func(br *openrtb.BidRequest) bool
}

// Gates lists the gates in evaluation order.
var Gates = []Gate{
	{Duplicate, "exact duplicate payload", nil}, // stateful; handled by the engine
	{Inventory, "site.domain / app.bundle", failsInventory},
	{Identity, "device.ip/ifa or user.id", failsIdentity},
	{Geo, "device.geo", failsGeo},
	{UserAgent, "user agent present and sane", failsUserAgent},
	{Datacenter, "non-datacenter IP", failsDatacenter},
}

// hasIP reports whether the request carries a device IP (v4 or v6).
func hasIP(br *openrtb.BidRequest) bool {
	return br != nil && br.Device != nil && (br.Device.IP != "" || br.Device.IPv6 != "")
}

// failsInventory: no identifiable inventory — a DSP cannot bid blind.
func failsInventory(br *openrtb.BidRequest) bool {
	if br == nil {
		return true
	}
	if br.Site != nil && br.Site.Domain != "" {
		return false
	}
	return !(br.App != nil && br.App.Bundle != "")
}

// failsIdentity: no stable identifier — no frequency capping or bid
// shading is possible.
func failsIdentity(br *openrtb.BidRequest) bool {
	if br == nil {
		return true
	}
	if br.Device != nil && (br.Device.IP != "" || br.Device.IPv6 != "" || br.Device.IFA != "") {
		return false
	}
	return !(br.User != nil && br.User.ID != "")
}

// failsGeo: no geo signal a buyer can target on.
func failsGeo(br *openrtb.BidRequest) bool {
	if br == nil || br.Device == nil || br.Device.Geo == nil {
		return true
	}
	g := br.Device.Geo
	if g.Country != "" || g.Region != "" || g.City != "" || g.Zip != "" {
		return false
	}
	return !(g.Lat != 0 && g.Lon != 0)
}

// failsUserAgent: missing, bot-like, or OS-inconsistent user agent.
func failsUserAgent(br *openrtb.BidRequest) bool {
	if br == nil || br.Device == nil || br.Device.UA == "" {
		return true
	}
	if quality.IsSuspiciousUA(br.Device.UA) {
		return true
	}
	return quality.UAOSMismatch(br.Device.UA, br.Device.OS)
}

// failsDatacenter: server-side traffic — IVT risk, not biddable.
// A request with no IP cannot be datacenter traffic.
func failsDatacenter(br *openrtb.BidRequest) bool {
	if !hasIP(br) {
		return false
	}
	if quality.IsDatacenterIP(br.Device.IP) {
		return true
	}
	return quality.IsDatacenterIP(br.Device.IPv6)
}

// payloadHash returns the canonical hash of a bid request payload, or ""
// when it cannot be serialized (such a request is never marked duplicate).
func payloadHash(br *openrtb.BidRequest) string {
	raw, err := json.Marshal(br)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Engine accumulates biddability across a bidstream sample.
type Engine struct {
	n        int
	biddable int
	fails    map[GateID]int
	seen     map[string]struct{}
}

// NewEngine returns an empty engine.
func NewEngine() *Engine {
	return &Engine{fails: make(map[GateID]int), seen: make(map[string]struct{})}
}

// Add folds one bid request into the engine. A nil request counts as a
// request failing every gate it can fail (it is never biddable).
func (e *Engine) Add(br *openrtb.BidRequest) {
	e.n++
	var failed []GateID
	if h := payloadHash(br); br != nil && h != "" {
		if _, dup := e.seen[h]; dup {
			failed = append(failed, Duplicate)
		} else {
			e.seen[h] = struct{}{}
		}
	}
	for _, g := range Gates {
		if g.ID == Duplicate || g.Fails == nil {
			continue
		}
		if g.Fails(br) {
			failed = append(failed, g.ID)
		}
	}
	for _, id := range failed {
		e.fails[id]++
	}
	if len(failed) == 0 {
		e.biddable++
	}
}

// Count returns the number of bid requests folded in so far.
func (e *Engine) Count() int { return e.n }

// BiddableCount returns how many folded requests passed every gate.
func (e *Engine) BiddableCount() int { return e.biddable }

// GateFails returns how many requests failed the given gate.
func (e *Engine) GateFails(id GateID) int { return e.fails[id] }

// GateResult is one row of a biddability report.
type GateResult struct {
	ID       GateID  `json:"id"`
	Label    string  `json:"label"`
	Failed   int     `json:"failed"`
	FailRate float64 `json:"fail_rate"` // 0–1, of all folded requests
}

// Report is the full biddability report over the folded sample.
type Report struct {
	Requests      int          `json:"requests"`
	Biddable      int          `json:"biddable"`
	BiddableShare float64      `json:"biddable_share"` // 0–1
	Gates         []GateResult `json:"gates"`          // in evaluation order
	InputQPS      float64      `json:"input_qps"`      // 0 when not supplied
	BiddableQPS   float64      `json:"biddable_qps"`   // InputQPS * share; 0 when input unset
}

// Report returns the biddability report. The inputQPS argument is the
// stated QPS the sample was drawn from; pass 0 when unknown, in which
// case only the share is reported.
func (e *Engine) Report(inputQPS float64) Report {
	r := Report{Requests: e.n, Gates: make([]GateResult, 0, len(Gates)), InputQPS: inputQPS}
	for _, g := range Gates {
		f := e.fails[g.ID]
		var rate float64
		if e.n > 0 {
			rate = float64(f) / float64(e.n)
		}
		r.Gates = append(r.Gates, GateResult{ID: g.ID, Label: g.Label, Failed: f, FailRate: rate})
	}
	r.Biddable = e.biddable
	if e.n > 0 {
		r.BiddableShare = float64(e.biddable) / float64(e.n)
	}
	if inputQPS > 0 {
		r.BiddableQPS = inputQPS * r.BiddableShare
	}
	return r
}
