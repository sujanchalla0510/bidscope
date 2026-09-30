// Package signals measures signal completeness across a bidstream sample.
//
// A DSP's bid decision is only as good as the signals the SSP's bid
// requests carry. This package computes, per signal, the fraction of bid
// requests that carry it (the "fill rate"), and rolls the eight core
// signals into one composite signal score so supply quality is comparable
// across SSPs at a glance.
//
// Signals tracked:
//
//	device.os       device.os is set (OS targeting / device bid shading)
//	device.ip       device.ip or device.ipv6 is set (geo + IP-based IVT)
//	user.id         user.id is set (SSP-side user id, no cookie needed)
//	inventory.id    site.domain or app.bundle is set (where the ad runs)
//	geo             device.geo with a country/region/city or lat+lon pair
//	consent         regs carries gdpr=1, a GPP string, or a non-empty ext
//	schain          a parseable supply-chain object (source.ext.schain)
//	eids            user.eids carry at least one extended identifier
//
// Scoring honesty: OpenRTB's gdpr field means 1 = subject to GDPR,
// 0 = not subject, absent = unknown. With encoding/json these are
// indistinguishable from each other (0 vs. absent), so the consent
// signal counts only gdpr=1, a GPP string, or a non-empty regs.ext —
// the actionable, verifiable subset a buyer can actually bid on.
package signals

import (
	"math"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// SignalID identifies one tracked signal.
type SignalID string

// The eight tracked signals, in display order.
const (
	DeviceOS     SignalID = "device.os"
	DeviceIP     SignalID = "device.ip"
	UserID       SignalID = "user.id"
	InventoryID  SignalID = "inventory.id"
	Geo          SignalID = "geo"
	Consent      SignalID = "consent"
	SChain       SignalID = "schain"
	EIDs         SignalID = "eids"
)

// Definition describes one signal: its id, a human-readable label, and
// the predicate that decides whether a bid request carries it.
type Definition struct {
	ID      SignalID
	Label   string
	Present func(*openrtb.BidRequest) bool
}

// Definitions lists all tracked signals in display order.
var Definitions = []Definition{
	{DeviceOS, "device.os", func(br *openrtb.BidRequest) bool {
		return br.Device != nil && br.Device.OS != ""
	}},
	{DeviceIP, "device.ip", func(br *openrtb.BidRequest) bool {
		return br.Device != nil && (br.Device.IP != "" || br.Device.IPv6 != "")
	}},
	{UserID, "user.id", func(br *openrtb.BidRequest) bool {
		return br.User != nil && br.User.ID != ""
	}},
	{InventoryID, "inventory.id", func(br *openrtb.BidRequest) bool {
		if br.Site != nil && br.Site.Domain != "" {
			return true
		}
		return br.App != nil && br.App.Bundle != ""
	}},
	{Geo, "geo", func(br *openrtb.BidRequest) bool {
		if br.Device == nil || br.Device.Geo == nil {
			return false
		}
		g := br.Device.Geo
		if g.Country != "" || g.Region != "" || g.City != "" || g.Zip != "" {
			return true
		}
		return g.Lat != 0 && g.Lon != 0
	}},
	{Consent, "consent", func(br *openrtb.BidRequest) bool {
		if br.Regs == nil {
			return false
		}
		if br.Regs.GDPR == 1 || br.Regs.GPP != "" {
			return true
		}
		return nonEmptyJSON(br.Regs.Ext)
	}},
	{SChain, "schain", func(br *openrtb.BidRequest) bool {
		return br.ParseSChain() != nil
	}},
	{EIDs, "eids", func(br *openrtb.BidRequest) bool {
		return br.User != nil && len(br.User.EIDs) > 0
	}},
}

// nonEmptyJSON reports whether raw is a JSON value other than an absent or
// empty object/array. It exists for consent detection: regs.ext = {"gdpr":1}
// is an actionable consent signal even when gdpr/gpp are unset.
func nonEmptyJSON(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	for _, c := range raw {
		switch c {
		case ' ', '\t', '\n', '\r', '{', '}', '[', ']', ':':
			continue
		default:
			return true
		}
	}
	return false
}

// Engine accumulates signal presence across a bidstream sample.
type Engine struct {
	n       int
	present map[SignalID]int
}

// NewEngine returns an empty engine.
func NewEngine() *Engine {
	return &Engine{present: make(map[SignalID]int)}
}

// Add folds one bid request into the engine. A nil request is counted as a
// request with no signals present (it still degrades every fill rate).
func (e *Engine) Add(br *openrtb.BidRequest) {
	e.n++
	if br == nil {
		return
	}
	for _, d := range Definitions {
		if d.Present(br) {
			e.present[d.ID]++
		}
	}
}

// Count returns the number of bid requests folded in so far.
func (e *Engine) Count() int { return e.n }

// Present returns how many requests carried the signal.
func (e *Engine) Present(id SignalID) int { return e.present[id] }

// FillRate returns the fraction of requests carrying the signal (0–1),
// or 0 when no requests have been added.
func (e *Engine) FillRate(id SignalID) float64 {
	if e.n == 0 {
		return 0
	}
	return float64(e.present[id]) / float64(e.n)
}

// SignalResult is one row of a completeness report.
type SignalResult struct {
	ID       SignalID `json:"id"`
	Label    string   `json:"label"`
	Present  int      `json:"present"`
	Total    int      `json:"total"`
	FillRate float64  `json:"fill_rate"` // 0–1
}

// Report is the full completeness report over the folded sample.
type Report struct {
	Requests int            `json:"requests"`
	Score    float64        `json:"score"` // composite 0–100
	Signals  []SignalResult `json:"signals"`
}

// Report returns the completeness report: per-signal fill rates plus the
// composite signal score — the unweighted mean of the eight fill rates,
// scaled to 0–100 and rounded to one decimal. With no requests the score
// is 0.
func (e *Engine) Report() Report {
	r := Report{Requests: e.n, Signals: make([]SignalResult, 0, len(Definitions))}
	for _, d := range Definitions {
		r.Signals = append(r.Signals, SignalResult{
			ID:       d.ID,
			Label:    d.Label,
			Present:  e.present[d.ID],
			Total:    e.n,
			FillRate: e.FillRate(d.ID),
		})
	}
	if e.n > 0 {
		var sum float64
		for _, s := range r.Signals {
			sum += s.FillRate
		}
		r.Score = math.Round(sum/float64(len(r.Signals))*1000) / 10
	}
	return r
}

// Score is a shorthand for Report().Score.
func (e *Engine) Score() float64 { return e.Report().Score }
