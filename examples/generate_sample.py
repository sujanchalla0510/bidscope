#!/usr/bin/env python3
"""Generate a synthetic SSP bidstream sample for BidScope's worked example.

NOTE: `bidscope -generate` (Go, built in) supersedes this script for general
use — same idea, configurable mix (`-profile clean|mixed|dirty`), no Python
needed. This script is kept so the README's worked example stays
byte-reproducible.

Everything here is fake: TEST-NET IPs (198.51.100.0/24, 203.0.113.0/24),
example.com inventory, random identifiers. Deterministic (seed 7), so the
README's worked example reproduces byte-for-byte.

Usage:
    python3 generate_sample.py > ssp-sample.jsonl
    bidscope -in ssp-sample.jsonl -qps 120000 -html report.html
"""
import json
import random
import copy

random.seed(7)

ANDROID_UA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"
IOS_UA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
SITES = ["news.example", "sports.example", "shop.example", "weather.example"]

prev = []
for i in range(1000):
    if i % 12 == 0 and prev:
        r = copy.deepcopy(random.choice(prev))  # exact replay of an earlier payload
    else:
        is_android = random.random() < 0.6
        r = {
            "id": f"req-{i}",
            "imp": [{
                "id": "imp-1",
                "banner": {"format": [{"w": 300, "h": 250}]},
                "bidfloor": round(random.uniform(0.2, 6.0), 2),
                **({"pmp": {"deals": [{"id": "deal-1"}]}} if random.random() < 0.12 else {}),
            }],
            "site": {"domain": random.choice(SITES)},
            "device": {
                "os": "android" if is_android else "ios",
                "ip": f"198.51.100.{(i * 37) % 240 + 1}",
                "ifa": f"ifa-{i}",
                "ua": ANDROID_UA if is_android else IOS_UA,
                "geo": {"country": random.choices(["USA", "GBR", "DEU"], weights=[50, 30, 20])[0]},
            },
            "user": {"id": f"u-{i}"},
            "tmax": random.choice([100, 120, 120, 150, 200]),
            **({"regs": {"gdpr": 1, "gpp": "DBABMA~BAAAAA~1~AAAAA"}} if random.random() < 0.45 else {}),
        }
        if i % 17 == 0:
            del r["device"]["geo"]                       # missing geo
        if i % 19 == 0:
            r["device"]["ip"] = f"3.5.{(i // 19) % 250}.10"  # AWS range -> datacenter
        if i % 23 == 0:
            del r["device"]["ip"]; del r["device"]["ifa"]; del r["user"]["id"]  # no identity
        if i % 29 == 0:
            del r["device"]["ua"]                        # missing UA
        if i % 31 == 0:
            r["device"]["ua"] = "curl/8.0.1"             # bot-like UA
        prev.append(copy.deepcopy(r))
    print(json.dumps(r))
