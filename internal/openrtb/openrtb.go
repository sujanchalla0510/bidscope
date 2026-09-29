// Package openrtb provides OpenRTB 2.5/2.6 bid-request structures for BidScope.
//
// It is intentionally a focused subset of the spec: only the fields the
// profiler's analyses need (signal completeness, mix, quality signals,
// biddable-QPS). Unknown fields are ignored by encoding/json, so samples
// from real exchanges parse fine.
//
// All payloads BidScope ships with are synthetic; this package never emits
// anything on the network — parsing is purely local.
package openrtb

import "encoding/json"

// Versions BidScope recognises. Everything is parsed with one unified
// struct set; DetectVersion tells you which spec generation a payload
// looks like.
const (
	VersionUnknown = "unknown"
	Version25      = "2.5"
	Version26      = "2.6"
)

// BidRequest is an OpenRTB 2.5/2.6 bid request. Field comments note the
// spec version that introduced each field; fields without a note exist in
// both 2.5 and 2.6.
type BidRequest struct {
	ID     string          `json:"id"`
	Imp    []Imp           `json:"imp"`
	Site   *Site           `json:"site,omitempty"`
	App    *App            `json:"app,omitempty"`
	Device *Device         `json:"device,omitempty"`
	User   *User           `json:"user,omitempty"`
	At     int             `json:"at"`
	TMax   int64           `json:"tmax,omitempty"`
	Source *Source         `json:"source,omitempty"`
	Regs   *Regs           `json:"regs,omitempty"`
	Ext    json.RawMessage `json:"ext,omitempty"`
}

// Imp is one impression offered in the auction.
type Imp struct {
	ID          string          `json:"id"`
	Banner      *Banner         `json:"banner,omitempty"`
	Video       *Video          `json:"video,omitempty"`
	Native      *Native         `json:"native,omitempty"`
	Audio       *Audio          `json:"audio,omitempty"` // 2.6
	BidFloor    float64         `json:"bidfloor,omitempty"`
	BidFloorCur string          `json:"bidfloorcur,omitempty"`
	PMP         *PMP            `json:"pmp,omitempty"`
	Rwdd        int             `json:"rwdd,omitempty"` // 2.6: rewarded inventory flag
	Ssai        int             `json:"ssai,omitempty"` // 2.6: server-side ad insertion
	Ext         json.RawMessage `json:"ext,omitempty"`
}

// Banner is a banner impression. Format lists the creative sizes offered.
type Banner struct {
	Format []Format `json:"format,omitempty"`
}

// Format is one allowed banner creative size.
type Format struct {
	W int `json:"w"`
	H int `json:"h"`
}

// Video is a video impression.
type Video struct {
	Mimes []string `json:"mimes,omitempty"`
	W     int      `json:"w,omitempty"`
	H     int      `json:"h,omitempty"`
}

// Native is a native impression (payload is an opaque request string).
type Native struct {
	Request string `json:"request,omitempty"`
}

// Audio is an audio impression (2.6).
type Audio struct {
	Mimes []string `json:"mimes,omitempty"`
}

// PMP is private-marketplace configuration: deals the impression carries.
type PMP struct {
	PrivateAuction int    `json:"privateauction,omitempty"`
	Deals          []Deal `json:"deals,omitempty"`
}

// Deal is one PMP deal attached to an impression.
type Deal struct {
	ID       string  `json:"id"`
	BidFloor float64 `json:"bidfloor,omitempty"`
	At       int     `json:"at"`
}

// Site describes the web property carrying the impression.
type Site struct {
	Domain    string          `json:"domain,omitempty"`
	Page      string          `json:"page,omitempty"`
	Publisher *Publisher      `json:"publisher,omitempty"`
	Ext       json.RawMessage `json:"ext,omitempty"`
}

// App describes the mobile app carrying the impression.
type App struct {
	Bundle    string          `json:"bundle,omitempty"`
	StoreURL  string          `json:"storeurl,omitempty"`
	Publisher *Publisher      `json:"publisher,omitempty"`
	Ext       json.RawMessage `json:"ext,omitempty"`
}

// Publisher identifies the seller's publisher.
type Publisher struct {
	ID string `json:"id,omitempty"`
}

// Device describes the user's device.
type Device struct {
	OS         string          `json:"os,omitempty"`
	OSV        string          `json:"osv,omitempty"`
	IP         string          `json:"ip,omitempty"`
	IPv6       string          `json:"ipv6,omitempty"`
	IFA        string          `json:"ifa,omitempty"`
	UA         string          `json:"ua,omitempty"`
	DeviceType int             `json:"devicetype,omitempty"`
	Geo        *Geo            `json:"geo,omitempty"`
	SUA        json.RawMessage `json:"sua,omitempty"` // 2.6: structured user agent
	Ext        json.RawMessage `json:"ext,omitempty"`
}

// Geo is the device's approximate location.
type Geo struct {
	Country string  `json:"country,omitempty"`
	Region  string  `json:"region,omitempty"`
	City    string  `json:"city,omitempty"`
	Zip     string  `json:"zip,omitempty"`
	Lat     float64 `json:"lat,omitempty"`
	Lon     float64 `json:"lon,omitempty"`
	Type    int     `json:"type,omitempty"`
}

// User describes the impression's user.
type User struct {
	ID       string          `json:"id,omitempty"`
	BuyerUID string          `json:"buyeruid,omitempty"`
	EIDs     []EID           `json:"eids,omitempty"`
	Ext      json.RawMessage `json:"ext,omitempty"`
}

// EID is one extended (cookie-less / SSP-synced) user identifier.
type EID struct {
	Source string   `json:"source,omitempty"`
	IDs    []EIDUID `json:"uids,omitempty"`
}

// EIDUID is a single identifier inside an EID.
type EIDUID struct {
	ID string `json:"id,omitempty"`
}

// Source carries auction metadata, notably the supply chain object.
type Source struct {
	FD  int             `json:"fd,omitempty"`
	Ext json.RawMessage `json:"ext,omitempty"`
}

// SChain is a parsed supply-chain object (source.ext.schain).
type SChain struct {
	Complete int          `json:"complete"`
	Ver      string       `json:"ver"`
	Nodes    []SChainNode `json:"nodes"`
}

// SChainNode is one hop in a supply chain.
type SChainNode struct {
	ASI string `json:"asi"`
	SID string `json:"sid"`
	RID string `json:"rid,omitempty"`
	HP  int    `json:"hp,omitempty"`
}

// ParseSChain extracts the supply-chain object from source.ext, or nil when
// the request carries none. Malformed schain payloads return nil rather than
// an error: schain is a signal, not a parse gate.
func (br *BidRequest) ParseSChain() *SChain {
	if br.Source == nil || len(br.Source.Ext) == 0 {
		return nil
	}
	var wrapper struct {
		SChain SChain `json:"schain"`
	}
	if err := json.Unmarshal(br.Source.Ext, &wrapper); err != nil {
		return nil
	}
	if wrapper.SChain.Ver == "" && len(wrapper.SChain.Nodes) == 0 {
		return nil
	}
	return &wrapper.SChain
}

// Regs carries regulatory/consent signals.
type Regs struct {
	GDPR int8            `json:"gdpr,omitempty"`
	GPP  string          `json:"gpp,omitempty"` // 2.6: Global Privacy Platform consent string
	Ext  json.RawMessage `json:"ext,omitempty"`
}

// DetectVersion reports which OpenRTB spec generation req looks like.
//
// The unified structs parse both generations, so this is a heuristic: a
// request is classified 2.6 when it sets a field that only exists in 2.6
// (imp.rwdd, imp.ssai, imp.audio, device.sua, regs.gpp); otherwise 2.5.
// A nil request reports "unknown".
func DetectVersion(req *BidRequest) string {
	if req == nil {
		return VersionUnknown
	}
	for i := range req.Imp {
		imp := &req.Imp[i]
		if imp.Rwdd != 0 || imp.Ssai != 0 || imp.Audio != nil {
			return Version26
		}
	}
	if req.Device != nil && len(req.Device.SUA) > 0 {
		return Version26
	}
	if req.Regs != nil && req.Regs.GPP != "" {
		return Version26
	}
	return Version25
}
