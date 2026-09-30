# BidScope

**BidScope** is an open-source bidstream profiler. Feed it a sample of OpenRTB
bid requests → get a supply-quality report answering **"should I integrate /
keep this SSP?"**

DSPs decide SSP integrations on vibes today: a sales deck claims 100k QPS of
"premium supply", and engineering discovers six weeks later that 40% of
requests are missing device IDs, 15% are duplicates, and the geo mix doesn't
match the IO. That evaluation is currently hand-rolled scripts and
spreadsheets at every DSP. BidScope makes it a one-liner.

## What it analyzes

1. **Signal completeness** — fill rates for device.os/ip, user.id,
   site.domain / app.bundle, geo, consent (regs.gdpr/gpp), schain, eids.
   Composite signal score.
2. **Mix analysis** — geo/device/OS/format distributions, site vs app,
   PMP/deal share, bidfloor median/p90.
3. **Quality signals** — exact + near-duplicate detection, datacenter
   IP/ASN ratio, user-agent anomalies, red-flag rules.
4. **Biddable QPS estimate** — the money line: what % of the stream is
   actually biddable, extrapolated to stated QPS. *"They sell 100k QPS;
   ~52k is biddable."*

## Local-first

Everything runs on your machine — **no telemetry, no uploads, ever**. Bid
payloads carry user IDs, geo, and commercial terms; that data never leaves
the box. All sample fixtures in this repo are synthetic.

## Status

Early build — v0.1 target:

- [x] **M1** — scaffold: Go module, CLI skeleton, CI, Apache-2.0 license
- [x] **M2** — ingest: streaming JSONL reader (gzip), OpenRTB 2.5/2.6 structs
- [x] **M3** — signal completeness engine (fill-rate table + composite signal score, text and `--json`)
- [ ] **M4** — mix analysis
- [ ] **M5** — quality signals
- [ ] **M6** — biddable-QPS estimate + HTML report → **v0.1, public launch**

## Quickstart

```sh
git clone https://github.com/sujanchalla0510/bidscope
cd bidscope
go build ./...
./bidscope -version
```

(Repo is private until the v0.1 launch; the public launch ships a worked
example profiling a synthetic SSP sample.)

## Roadmap

- **v0.2**: web tester (Go→WASM, local-first), GitHub Action, MCP server,
  synthetic generator (`bidscope -generate`)
- **Later**: drift mode (A/B diff), bid-response pairing, schain ↔
  ads.txt/sellers.json cross-check, live tap, OpenRTB 3.0

## License

Apache-2.0. See [LICENSE](LICENSE).
