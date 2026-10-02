// Package quality analyses bidstream quality signals: duplication,
// suspicious traffic, and protocol red flags.
//
// Where internal/signals asks "do the requests carry the signals a DSP
// needs?" and internal/mix asks "what is this supply made of?", quality
// asks "is this stream trustworthy?": exact and near-duplicate payloads,
// datacenter IP share (static heuristic list), user-agent anomalies, the
// tmax (timeout budget) distribution, and a set of red-flag rules that
// point at traffic no buyer wants to pay for.
//
// Everything here is a heuristic over the sample — datacenter membership
// especially so (a static CIDR list can drift out of date, and cloud IPs
// can front legitimate traffic). Red flags are warnings for a human to
// investigate, never verdicts.
//
// All payloads BidScope ships with are synthetic; this package never emits
// anything on the network — analysis is purely local.
package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net"
	"sort"
	"strings"

	"github.com/sujanchalla0510/bidscope/internal/openrtb"
)

// datacenterCIDRs is the static heuristic list of cloud/datacenter IP
// ranges. Bid requests whose device.ip lands in one of these are counted
// as datacenter traffic: server-side generated traffic (IVT, proxy farms,
// accidental server-side testing) shows up in bidstreams as datacenter
// IPs, so a high share is a classic quality red flag.
//
// This list is intentionally coarse — it flags "looks like a cloud
// provider's range", not a verified ASN. AWS/GCP/Azure ranges are huge,
// so we keep representative supernets rather than chasing completeness.
var datacenterCIDRs = []string{
	// AWS EC2 and friends.
	"3.0.0.0/8", "13.0.0.0/8", "18.0.0.0/8", "52.0.0.0/8", "54.0.0.0/8",
	// Google Cloud.
	"34.0.0.0/8", "35.184.0.0/13",
	// Azure.
	"20.0.0.0/8", "40.0.0.0/8", "52.224.0.0/11",
	// DigitalOcean.
	"104.131.0.0/16", "104.236.0.0/14", "159.203.0.0/16", "159.65.0.0/16",
	"157.230.0.0/16", "167.71.0.0/16", "68.183.0.0/16", "143.198.0.0/16",
	"142.93.0.0/16", "165.22.0.0/16", "206.81.0.0/16", "174.138.0.0/16",
	"188.166.0.0/16", "139.59.0.0/16", "138.68.0.0/16", "64.225.0.0/16",
	// Hetzner.
	"95.216.0.0/16", "159.69.0.0/16", "168.119.0.0/16", "116.203.0.0/16",
	"157.90.0.0/16", "49.12.0.0/16", "88.99.0.0/16", "78.46.0.0/16",
	// OVHcloud.
	"51.38.0.0/15", "51.68.0.0/15", "51.77.0.0/16", "51.83.0.0/16",
	"51.89.0.0/16", "51.91.0.0/16", "51.195.0.0/16", "51.210.0.0/15",
	"51.222.0.0/16", "37.187.0.0/16", "94.23.0.0/16", "135.125.0.0/16",
	"137.74.0.0/16", "145.239.0.0/16", "146.59.0.0/16", "147.135.0.0/16",
	"148.113.0.0/16",
	// Linode.
	"45.33.0.0/16", "66.175.0.0/16", "104.200.0.0/14", "172.104.0.0/15",
	"173.255.0.0/16", "192.46.0.0/15",
	// Vultr.
	"104.156.0.0/14", "149.28.0.0/16", "108.61.0.0/16", "45.32.0.0/16",
	"45.76.0.0/16", "144.202.0.0/16", "140.82.0.0/16",
}

// datacenterNets is the parsed form of datacenterCIDRs. A range that fails
// to parse is dropped (a broken list entry must not break analysis).
var datacenterNets []*net.IPNet

func init() {
	for _, cidr := range datacenterCIDRs {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			datacenterNets = append(datacenterNets, n)
		}
	}
}

// IsDatacenterIP reports whether ip (device.ip or device.ipv6) falls in
// the static datacenter list. Garbage input returns false.
func IsDatacenterIP(ip string) bool {
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return false
	}
	for _, n := range datacenterNets {
		if n.Contains(addr) {
			return true
		}
	}
	return false
}

// suspiciousUAPatterns are lowercase substrings that mark a user agent as
// bot, headless, or scripted traffic rather than a real browser or app:
// bot crawlers, headless test frameworks, and CLI/script HTTP clients
// all show up in junk bidstreams.
var suspiciousUAPatterns = []string{
	"headlesschrome", "phantomjs", "slimerjs",
	"curl/", "wget/", "python-requests", "go-http-client",
	"java/", "apache-httpclient", "libwww-perl", "scrapy", "httpie", "mechanize",
	"bot", "spider", "crawler", "slurp", "scraper",
}

// IsSuspiciousUA reports whether ua looks like bot/headless/scripted
// traffic. Matching is case-insensitive substring; an empty UA is not
// suspicious (it's counted separately as missing).
func IsSuspiciousUA(ua string) bool {
	l := strings.ToLower(ua)
	if l == "" {
		return false
	}
	for _, p := range suspiciousUAPatterns {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// uaOSFamily pairs a UA substring (lowercase, checked in order) with the
// device.os values it implies. First match wins, so narrower tokens come
// before broader ones ("iphone" before "mac os x", "android" before "linux"
// — Android UAs contain "Linux", iOS UAs contain "like Mac OS X").
var uaOSFamily = []struct {
	token string
	oss   map[string]struct{}
}{
	{"iphone", map[string]struct{}{"ios": {}}},
	{"ipad", map[string]struct{}{"ios": {}}},
	{"ipod", map[string]struct{}{"ios": {}}},
	{"android", map[string]struct{}{"android": {}}},
	{"windows nt", map[string]struct{}{"windows": {}}},
	{"win64", map[string]struct{}{"windows": {}}},
	{"mac os x", map[string]struct{}{"macos": {}}},
	{"cros", map[string]struct{}{"chromeos": {}}},
	{"linux", map[string]struct{}{"linux": {}}},
}

// UAOSMismatch reports whether ua and device.os disagree — e.g. a UA that
// says "Windows NT" on a request that claims device.os = "ios". Real
// bidstreams carry some mismatches (bad parsing, spoofing); a high share
// means the SSP's device data can't be trusted. When the UA carries no
// recognised OS family, or os is empty, there is nothing to contradict.
func UAOSMismatch(ua, os string) bool {
	l := strings.ToLower(ua)
	los := strings.ToLower(strings.TrimSpace(os))
	if l == "" || los == "" {
		return false
	}
	for _, fam := range uaOSFamily {
		if strings.Contains(l, fam.token) {
			_, ok := fam.oss[los]
			return !ok
		}
	}
	return false
}

// fingerprint identifies "the same actor requesting again": the device,
// user, and inventory fields that should be stable across auctions for one
// placement on one device. Auction specifics (request id, tmax, imp ids,
// floors) are deliberately excluded so replayed or repeated traffic still
// shares a fingerprint.
func fingerprint(br *openrtb.BidRequest) string {
	if br == nil {
		return ""
	}
	parts := make([]string, 0, 9)
	if d := br.Device; d != nil {
		parts = append(parts, norm(d.OS), norm(d.IP), norm(d.IPv6), norm(d.IFA), norm(d.UA))
		if g := d.Geo; g != nil {
			parts = append(parts, norm(g.Country))
		} else {
			parts = append(parts, "")
		}
	} else {
		parts = append(parts, "", "", "", "", "", "")
	}
	if u := br.User; u != nil {
		parts = append(parts, norm(u.ID))
	} else {
		parts = append(parts, "")
	}
	switch {
	case br.Site != nil:
		parts = append(parts, norm(br.Site.Domain))
	case br.App != nil:
		parts = append(parts, norm(br.App.Bundle))
	default:
		parts = append(parts, "")
	}
	return strings.Join(parts, "\x1f")
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// payloadHash is the SHA-256 of the canonical payload JSON. Struct
// marshaling in Go emits fields in declaration order, so this is stable
// across runs and requests that parse to identical structs hash alike.
func payloadHash(br *openrtb.BidRequest) string {
	raw, err := json.Marshal(br)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Red flag severities.
const (
	SeverityWarn = "warn"
	SeverityHigh = "high"
)

// RedFlag is one triggered quality rule: a machine-readable code, a
// severity ("warn" or "high"), and a human-readable detail line.
type RedFlag struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// Report is the full quality report over the folded sample.
type Report struct {
	Requests             int       `json:"requests"`
	ExactDuplicates      int       `json:"exact_duplicates"`       // requests byte-identical to another payload
	ExactDuplicateGroups int       `json:"exact_duplicate_groups"` // distinct payloads seen more than once
	NearDuplicates       int       `json:"near_duplicates"`        // same actor fingerprint, differing payload
	NearDuplicateGroups  int       `json:"near_duplicate_groups"`  // fingerprints with >1 distinct payload
	DuplicateIDs         int       `json:"duplicate_ids"`          // requests reusing an already-seen id
	DatacenterIPs        int       `json:"datacenter_ips"`
	DatacenterIPShare    float64   `json:"datacenter_ip_share"` // 0–1, of requests
	MissingUA            int       `json:"missing_ua"`
	SuspiciousUA         int       `json:"suspicious_ua"`
	UAOSMismatch         int       `json:"ua_os_mismatch"`
	TopUAShare           float64   `json:"top_ua_share"` // 0–1, of requests
	TMaxUnset            int       `json:"tmax_unset"`
	TMaxMedian           float64   `json:"tmax_median"` // ms; 0 when no request sets tmax
	TMaxDistinct         int       `json:"tmax_distinct"`
	RedFlags             []RedFlag `json:"red_flags"`
}

// Engine accumulates quality statistics across a bidstream sample.
type Engine struct {
	n int

	idCounts      map[string]int
	payloadCounts map[string]int
	fpCounts      map[string]int
	fpPayloads    map[string]map[string]struct{} // fingerprint -> distinct payload hashes

	datacenter int

	uaCounts   map[string]int
	missingUA  int
	suspicious int
	mismatch   int

	tmaxUnset    int
	tmaxValues   []float64
	tmaxDistinct map[int64]struct{}

	userID   int
	deviceIP int
	eids     int
}

// NewEngine returns an empty engine.
func NewEngine() *Engine {
	return &Engine{
		idCounts:      make(map[string]int),
		payloadCounts: make(map[string]int),
		fpCounts:      make(map[string]int),
		fpPayloads:    make(map[string]map[string]struct{}),
		uaCounts:      make(map[string]int),
		tmaxDistinct:  make(map[int64]struct{}),
	}
}

// Add folds one bid request into the engine. A nil request counts as a
// request with no quality attributes (it still degrades every share).
func (e *Engine) Add(br *openrtb.BidRequest) {
	e.n++
	if br == nil {
		return
	}

	if br.ID != "" {
		e.idCounts[br.ID]++
	}
	if h := payloadHash(br); h != "" {
		e.payloadCounts[h]++
		if set, ok := e.fpPayloads[fingerprint(br)]; ok {
			set[h] = struct{}{}
		} else {
			e.fpPayloads[fingerprint(br)] = map[string]struct{}{h: {}}
		}
	}
	e.fpCounts[fingerprint(br)]++

	if br.Device != nil {
		if br.Device.IP != "" {
			e.deviceIP++
			if IsDatacenterIP(br.Device.IP) {
				e.datacenter++
			}
		} else if br.Device.IPv6 != "" {
			e.deviceIP++
			if IsDatacenterIP(br.Device.IPv6) {
				e.datacenter++
			}
		}
		if br.Device.UA == "" {
			e.missingUA++
		} else {
			e.uaCounts[br.Device.UA]++
			if IsSuspiciousUA(br.Device.UA) {
				e.suspicious++
			}
			if UAOSMismatch(br.Device.UA, br.Device.OS) {
				e.mismatch++
			}
		}
	} else {
		e.missingUA++
	}

	if br.TMax == 0 {
		e.tmaxUnset++
	} else {
		e.tmaxValues = append(e.tmaxValues, float64(br.TMax))
		e.tmaxDistinct[br.TMax] = struct{}{}
	}

	if br.User != nil {
		if br.User.ID != "" {
			e.userID++
		}
		if len(br.User.EIDs) > 0 {
			e.eids++
		}
	}
}

// Count returns the number of bid requests folded in so far.
func (e *Engine) Count() int { return e.n }

func median(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// flag appends a red flag when cond holds.
func flag(flags *[]RedFlag, cond bool, code, severity, detail string) {
	if cond {
		*flags = append(*flags, RedFlag{Code: code, Severity: severity, Detail: detail})
	}
}

// Report returns the quality report: duplication stats, datacenter and UA
// anomalies, the tmax distribution, and the triggered red-flag rules.
func (e *Engine) Report() Report {
	r := Report{Requests: e.n, RedFlags: []RedFlag{}}

	// Exact duplicates: payloads seen more than once. Requests beyond the
	// first in each payload group are the duplicates.
	exactDupHashes := make(map[string]struct{})
	for h, c := range e.payloadCounts {
		if c > 1 {
			r.ExactDuplicateGroups++
			r.ExactDuplicates += c - 1
			exactDupHashes[h] = struct{}{}
		}
	}

	// Near duplicates: fingerprints with more than one distinct payload —
	// the same actor requesting again with a differing payload. Requests
	// already counted as exact duplicates are not double-counted.
	for fp := range e.fpCounts {
		if len(e.fpPayloads[fp]) <= 1 {
			continue
		}
		r.NearDuplicateGroups++
		for h := range e.fpPayloads[fp] {
			if _, isExact := exactDupHashes[h]; !isExact {
				r.NearDuplicates += e.payloadCounts[h]
			}
		}
		// Every request in the fingerprint belongs to exactly one payload
		// group, so this sum equals (fingerprint count) minus the
		// exact-duplicate requests already counted above.
	}

	for _, c := range e.idCounts {
		if c > 1 {
			r.DuplicateIDs += c - 1
		}
	}

	r.DatacenterIPs = e.datacenter
	r.MissingUA = e.missingUA
	r.SuspiciousUA = e.suspicious
	r.UAOSMismatch = e.mismatch
	r.TMaxUnset = e.tmaxUnset
	r.TMaxMedian = math.Round(median(append([]float64(nil), e.tmaxValues...)))
	r.TMaxDistinct = len(e.tmaxDistinct)

	n := float64(e.n)
	if e.n > 0 {
		r.DatacenterIPShare = float64(e.datacenter) / n
		top := 0
		for _, c := range e.uaCounts {
			if c > top {
				top = c
			}
		}
		r.TopUAShare = float64(top) / n
	}

	// Red-flag rules. Thresholds are deliberately round and documented
	// per rule; these are "go look at this", not pass/fail gates.
	flag(&r.RedFlags, r.DuplicateIDs > 0, "duplicate-request-ids", SeverityHigh,
		"request ids must be unique per auction — reused ids suggest replayed or logged traffic")
	flag(&r.RedFlags, e.n > 0 && float64(r.ExactDuplicates)/n > 0.05, "exact-duplicates", SeverityHigh,
		"over 5% of the stream is byte-identical replays")
	flag(&r.RedFlags, e.n > 0 && float64(r.NearDuplicates)/n > 0.2, "near-duplicates", SeverityWarn,
		"over 20% of requests come from actors already seen with a different payload")
	flag(&r.RedFlags, e.n > 0 && r.DatacenterIPShare > 0.1, "datacenter-traffic", SeverityWarn,
		"over 10% of requests carry datacenter IPs (server-side / IVT risk)")
	flag(&r.RedFlags, e.n > 0 && float64(e.missingUA)/n > 0.5, "missing-ua", SeverityWarn,
		"over half the stream has no user agent — UA targeting is unusable")
	flag(&r.RedFlags, e.n > 0 && float64(e.suspicious)/n > 0.05, "suspicious-ua", SeverityHigh,
		"over 5% of user agents look like bots, headless browsers, or scripts")
	flag(&r.RedFlags, e.n >= 20 && r.TopUAShare > 0.8, "uniform-ua", SeverityWarn,
		"one user agent covers over 80% of the stream — implausible for real traffic")
	flag(&r.RedFlags, e.n > 0 && float64(e.mismatch)/n > 0.05, "ua-os-mismatch", SeverityWarn,
		"over 5% of user agents contradict device.os — device data can't be trusted")
	flag(&r.RedFlags, e.n > 0 && e.tmaxUnset == e.n, "no-tmax", SeverityWarn,
		"no request carries a tmax — the stream offers no timeout budget")
	flag(&r.RedFlags, e.n >= 20 && r.TMaxDistinct == 1, "uniform-tmax", SeverityWarn,
		"every request carries the same tmax — suspicious uniformity")
	flag(&r.RedFlags, e.n >= 20 && e.userID == e.n && e.deviceIP == e.n && e.eids == e.n,
		"too-perfect", SeverityWarn,
		"user.id, device.ip, and eids are all 100% filled — real streams are never this complete")

	return r
}
