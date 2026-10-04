# BidScope

**Score SSP supply quality before you sign the integration.** Feed BidScope a
sample of OpenRTB bid requests — get a supply-quality report answering
*"they sell 100k QPS… how much of it is actually biddable?"*

DSPs decide SSP integrations on vibes today: a sales deck claims 100k QPS of
"premium supply", and engineering discovers six weeks later that 40% of
requests are missing device IDs, 15% are duplicates, and the geo mix doesn't
match the IO. That evaluation is hand-rolled scripts and spreadsheets at
every DSP. BidScope makes it a one-liner.

Supports OpenRTB 2.5 and 2.6 (auto-detected). Apache-2.0.

## Web tester

No install, no data handoff: **[sujanchalla0510.github.io/bidscope](https://sujanchalla0510.github.io/bidscope/)**
runs the profiler as WebAssembly **entirely in your browser** — paste a JSONL
sample (or generate a synthetic SSP stream in one click) and get the full
report. Nothing is uploaded; scores are identical to the CLI for the same
input.

## What it measures

| Engine | Answers |
|---|---|
| **Signal completeness** | Fill rates for device.os/ip, user.id, site.domain/app.bundle, geo, consent (regs.gdpr/gpp), schain, eids — rolled into one 0–100 signal score |
| **Mix analysis** | Geo / device / OS / format distributions, site-vs-app, PMP & deal share, bidfloor median/p90 |
| **Quality signals** | Exact + near-duplicate detection, datacenter-IP share, UA anomalies (missing, bot-like, UA↔OS mismatch), tmax distribution, red-flag rules |
| **Biddable QPS** | The money line: what share of the stream passes every biddability gate, scaled to the SSP's stated QPS |

A request is **biddable** when a DSP could plausibly spend on it — it passes
all six gates:

1. **exact duplicate payload** — not a replay of an earlier request
2. **site.domain / app.bundle** — the inventory is identifiable
3. **device.ip/ifa or user.id** — a usable identity for capping and shading
4. **device.geo** — a targetable geo signal
5. **user agent present and sane** — real UA, no UA↔OS contradiction
6. **non-datacenter IP** — not server-side/IVT-risk traffic

## Quickstart

```sh
git clone https://github.com/sujanchalla0510/bidscope
cd bidscope
go build -o bidscope ./cmd/bidscope

# no data? generate a synthetic sample and profile it in one pipe
./bidscope -generate | ./bidscope -in - -qps 120000 -html report.html

# reproduce the worked example below (fully synthetic sample)
python3 examples/generate_sample.py > ssp-sample.jsonl
./bidscope -in ssp-sample.jsonl -qps 120000 -html report.html
```

Flags: `-in` (JSONL file, `.gz` ok, `-` for stdin) · `-qps` (stated input QPS
— scales the biddable share into absolute QPS) · `-html` (self-contained HTML
report) · `-json` (machine-readable output) · `-version`.
`-generate` emits a synthetic bidstream sample instead of reading input:
`-n` (how many, default 1000), `-seed` (deterministic; same seed = same
bytes, default 7), `-profile` (`clean` · `mixed` · `dirty`). `dirty` is a
junk SSP that trips every quality gate — useful for demos and for testing
your own integrations against BidScope.

## GitHub Action

Run BidScope as a supply-quality gate in CI — e.g. nightly against a fresh
sample from each SSP, or on pull requests that touch traffic shaping:

```yaml
- name: BidScope SSP gate
  id: bidscope
  uses: sujanchalla0510/bidscope@v0.1.0
  with:
    input: ssp-sample.jsonl   # JSONL (or .gz) of OpenRTB bid requests
    qps: "120000"             # stated sample QPS (optional)
    min-signal-score: "60"    # fail below this score (optional)
    fail-on-red-flags: "true" # fail on any red flag (default true)
```

Outputs: `signal-score`, `biddable-share`, `biddable-qps`,
`red-flag-count`, `requests-parsed`, `json-report`, `html-report`. The JSON
and HTML reports are uploaded as the `bidscope-report` artifact. The job
fails when any red flag is raised or the signal score falls below
`min-signal-score`.

## Worked example

A synthetic 1,000-request SSP sample (seeded generator above — TEST-NET IPs,
example.com inventory, nothing real). The SSP claims **120,000 QPS**:

```
bidscope: parsed 1000 bid request(s) from ssp-sample.jsonl
OpenRTB versions: 2.5=535, 2.6=465

Signal completeness:
  signal         present   fill
  device.os      1000/1000  100.0%
  device.ip       959/1000   95.9%
  user.id         959/1000   95.9%
  inventory.id   1000/1000  100.0%
  geo             940/1000   94.0%
  consent         465/1000   46.5%
  schain            0/1000    0.0%
  eids              0/1000    0.0%
  signal score: 66.5/100

Mix analysis (1000 request(s), 1000 impression(s)):
  inventory: 1000 site (100.0%), 0 app (0.0%)
  formats:   banner 100.0% (1000)
  top os:    android 63.2% (632), ios 36.8% (368)
  top geo:   USA 46.0% (460), GBR 30.5% (305), DEU 17.5% (175)
  devices:   —
  pmp: 12.8% of imps carry pmp, 12.8% carry deals, 0.0% private auction
  bid floors (n=1000): median 2.98, p90 5.32

Quality signals:
  duplicates: 83 exact (81 group(s)), 31 near-duplicate (10 group(s)), 83 reused request id(s)
  datacenter IPs: 49 (4.9%)
  user agents: 35 missing, 36 suspicious, 0 ua/os mismatch; top UA share 57.9%
  tmax: 0 unset, median 120 ms (4 distinct)
  red flags:
    [high] duplicate-request-ids: request ids must be unique per auction — reused ids suggest replayed or logged traffic
    [high] exact-duplicates: over 5% of the stream is byte-identical replays

Biddable QPS:
  biddable: 727/1000 requests pass every gate (72.7%)
    exact duplicate payload      83 failed (  8.3%)
    site.domain / app.bundle      0 failed (  0.0%)
    device.ip/ifa or user.id     41 failed (  4.1%)
    device.geo                   60 failed (  6.0%)
    user agent present and sane     71 failed (  7.1%)
    non-datacenter IP            49 failed (  4.9%)
  at 120000 QPS in: ~87240 biddable QPS

BidScope verdict: 727 of 1000 sampled requests (72.7%) are biddable — at a stated 120000 QPS that is ~87240 biddable QPS.
Signal completeness scores 66.5/100; weakest signals: schain 0.0%, eids 0.0%, consent 46.5%.
Inventory is 100% site / 0% app, led by USA (46%); 13% of impressions carry PMP.
Quality warnings: 83 exact-duplicate replays (8.3% of requests); red flag: duplicate-request-ids; red flag: exact-duplicates.
Biddability is lost to: exact duplicate payload (8.3%), user agent present and sane (7.1%), device.geo (6.0%), non-datacenter IP (4.9%), device.ip/ifa or user.id (4.1%).
```

The negotiation takeaway: the 120k-QPS deck is really ~87k biddable QPS,
and 8.3% of the stream is byte-identical replays — a concrete line item for
the IO, not a vibe.

The `-html` flag produces a self-contained report (inline CSS, no external
assets) you can attach to a vendor evaluation email. `-json` emits the full
machine-readable profile for pipelines.

## Local-first

Everything runs on your machine — **no telemetry, no uploads, ever**. Bid
payloads carry user IDs, geo, and commercial terms; that data never leaves
the box. Every fixture and sample in this repo is synthetic.

## Roadmap

- **v0.2**: ✅ web tester — live at
  [sujanchalla0510.github.io/bidscope](https://sujanchalla0510.github.io/bidscope/)
  (Go→WASM, local-first; reuses `-generate` as its "try sample data" source);
  ✅ GitHub Action (`uses: sujanchalla0510/bidscope@v0.1.0`) — SSP
  supply-quality gate with red-flag/score-floor failure, JSON+HTML artifacts;
  next: MCP server
- **Later**: drift mode (A/B diff), bid-response pairing (bidder mode),
  schain ↔ ads.txt/sellers.json cross-check, live tap, OpenRTB 3.0

## License

Apache-2.0. See [LICENSE](LICENSE).
