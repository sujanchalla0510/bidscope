/* BidScope web tester — drives the Go/WASM profiler, renders results.
   All computation happens locally; the sample never leaves the browser. */
(function () {
  "use strict";

  var $ = function (id) { return document.getElementById(id); };
  var statusEl = $("status"), jsonlEl = $("jsonl"), qpsEl = $("qps");
  var btnSample = $("btn-sample"), btnProfile = $("btn-profile"), fileEl = $("file");
  var resultsPanel = $("results-panel");
  var wasmReady = false;

  function setStatus(msg) { statusEl.textContent = msg; }

  // Boot the WASM module. We avoid instantiateStreaming so the page works
  // even where the host doesn't serve application/wasm.
  async function boot() {
    try {
      var go = new Go();
      var buf = await (await fetch("bidscope.wasm")).arrayBuffer();
      var mod = await WebAssembly.instantiate(buf, go.importObject);
      go.run(mod.instance);
      wasmReady = true;
      btnSample.disabled = false;
      btnProfile.disabled = false;
      setStatus("profiler ready — paste bid requests, load a file, or try sample data.");
    } catch (e) {
      setStatus("could not start the profiler: " + e);
    }
  }

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function pct(x) { return (x * 100).toFixed(1) + "%"; }
  function bar(x) {
    var w = Math.max(2, Math.round(x * 120));
    return '<span class="bar" style="width:' + w + 'px"></span>';
  }

  function callGenerate(n, seed, profile) {
    var r = JSON.parse(bidscopeGenerate(n, seed, profile));
    if (!r.ok) throw new Error(r.error);
    return r.sample;
  }
  function callProfile(jsonl, qps) {
    var r = JSON.parse(bidscopeProfile(jsonl, qps));
    if (!r.ok) throw new Error(r.error);
    return r.result;
  }

  async function trySample() {
    if (!wasmReady) return;
    setStatus("generating a synthetic SSP sample…");
    // Let the UI paint before the (fast, but synchronous) WASM call.
    await new Promise(function (r) { setTimeout(r, 30); });
    try {
      var t0 = performance.now();
      var sample = callGenerate(500, 7, "mixed");
      jsonlEl.value = sample;
      var lines = sample.trim().split("\n").length;
      setStatus("generated " + lines + " synthetic requests in " +
        Math.round(performance.now() - t0) + " ms — profiling…");
      doProfile();
    } catch (e) {
      setStatus("sample generation failed: " + e.message);
    }
  }

  function doProfile() {
    if (!wasmReady) return;
    var jsonl = jsonlEl.value;
    if (!jsonl.trim()) {
      setStatus("paste some JSONL bid requests first — or hit “Try sample data”.");
      return;
    }
    var qps = parseFloat(qpsEl.value);
    if (isNaN(qps) || qps < 0) qps = 0;
    setStatus("profiling…");
    setTimeout(function () {
      try {
        var t0 = performance.now();
        var res = callProfile(jsonl, qps);
        var ms = Math.round(performance.now() - t0);
        render(res);
        resultsPanel.classList.remove("hidden");
        setStatus("profiled " + res.parsed + " request(s) in " + ms +
          " ms — all computation happened in your browser." +
          (res.malformed ? " (" + res.malformed + " malformed line(s) skipped)" : ""));
        resultsPanel.scrollIntoView({ behavior: "smooth", block: "start" });
      } catch (e) {
        setStatus("profiling failed: " + e.message);
      }
    }, 30);
  }

  function scoreClass(x) { return x >= 70 ? "good" : x >= 40 ? "mid" : "bad"; }

  function render(res) {
    var sig = res.signals, bid = res.biddable, mx = res.mix, q = res.quality;

    // Score cards.
    $("cards").innerHTML =
      card("Requests parsed", String(res.parsed), "") +
      card("Signal score", sig.score.toFixed(1) + " / 100", scoreClass(sig.score)) +
      card("Biddable share", pct(bid.biddable_share), scoreClass(bid.biddable_share * 100)) +
      card("Red flags", String(q.red_flags ? q.red_flags.length : 0),
        (q.red_flags && q.red_flags.length) ? "bad" : "good");

    // Signal completeness table.
    $("tbl-signals").querySelector("tbody").innerHTML = sig.signals.map(function (s) {
      return "<tr><td>" + esc(s.label) + "</td><td class='num'>" + s.present + " / " + s.total +
        "</td><td class='num'>" + bar(s.fill_rate) + pct(s.fill_rate) + "</td></tr>";
    }).join("");

    // Biddable gates table.
    var gatesHtml = bid.gates.map(function (g) {
      return "<tr><td>" + esc(g.label) + "</td><td class='num'>" + g.failed +
        "</td><td class='num'>" + bar(g.fail_rate) + pct(g.fail_rate) + "</td></tr>";
    }).join("");
    gatesHtml += "<tr><td><strong>Biddable</strong> (passes every gate)</td><td class='num'><strong>" +
      bid.biddable + " / " + bid.requests + "</strong></td><td class='num'><strong>" +
      pct(bid.biddable_share) + "</strong></td></tr>";
    if (bid.input_qps > 0) {
      gatesHtml += "<tr><td colspan='3'>At " + Math.round(bid.input_qps) +
        " QPS in: ~" + Math.round(bid.biddable_qps) + " biddable QPS</td></tr>";
    }
    $("tbl-gates").querySelector("tbody").innerHTML = gatesHtml;

    // Mix.
    $("mix").innerHTML =
      stat("Inventory", mx.site + " site (" + pct(mx.site_share) + "), " +
        mx.app + " app (" + pct(mx.app_share) + ")") +
      stat("Formats", items(mx.formats)) +
      stat("Top OS", items(mx.top_os)) +
      stat("Top geo", items(mx.top_countries)) +
      stat("PMP / deals", pct(mx.pmp_coverage) + " of imps carry pmp, " +
        pct(mx.deal_coverage) + " carry deals, " + pct(mx.private_share) + " private auction") +
      stat("Bid floors", mx.bidfloors > 0
        ? "n=" + mx.bidfloors + ", median " + mx.floor_median.toFixed(2) + ", p90 " + mx.floor_p90.toFixed(2)
        : "none set");

    // Quality.
    $("quality").innerHTML =
      stat("Duplicates", q.exact_duplicates + " exact (" + q.exact_duplicate_groups + " groups), " +
        q.near_duplicates + " near (" + q.near_duplicate_groups + " groups), " +
        q.duplicate_ids + " reused id(s)") +
      stat("Datacenter IPs", q.datacenter_ips + " (" + pct(q.datacenter_ip_share) + ")") +
      stat("User agents", q.missing_ua + " missing, " + q.suspicious_ua + " suspicious, " +
        q.ua_os_mismatch + " ua/os mismatch; top UA " + pct(q.top_ua_share)) +
      stat("tmax", q.tmax_distinct > 0
        ? q.tmax_unset + " unset, median " + Math.round(q.tmax_median) + " ms (" + q.tmax_distinct + " distinct)"
        : q.tmax_unset + " unset (no timeouts set)");

    // Red flags.
    var flags = q.red_flags || [];
    $("flags").innerHTML = flags.length === 0
      ? '<p class="none">none — nothing tripped a rule.</p>'
      : flags.map(function (f) {
          return '<div class="flag ' + esc(f.severity) + '"><span class="sev">' +
            esc(f.severity) + '</span> <span class="code">' + esc(f.code) +
            "</span><br>" + esc(f.detail) + "</div>";
        }).join("");
  }

  function card(k, v, cls) {
    return '<div class="card"><div class="k">' + esc(k) + '</div><div class="v ' + cls + '">' +
      esc(v) + "</div></div>";
  }
  function stat(k, v) {
    return '<div class="stat"><div class="k">' + esc(k) + "</div><div>" + esc(v) + "</div></div>";
  }
  function items(list) {
    if (!list || !list.length) return "—";
    return list.map(function (it) { return it.label + " " + pct(it.share); }).join(", ");
  }

  // File loading stays local: optionally gunzip in-browser, then hand the
  // text to the WASM profiler. Nothing is uploaded.
  fileEl.addEventListener("change", async function () {
    var f = fileEl.files[0];
    if (!f) return;
    try {
      var text;
      if (/\.gz$/i.test(f.name) && typeof DecompressionStream !== "undefined") {
        var ds = new DecompressionStream("gzip");
        text = await new Response(f.stream().pipeThrough(ds)).text();
      } else {
        text = await f.text();
      }
      jsonlEl.value = text;
      setStatus("loaded " + f.name + " (" + text.length.toLocaleString() +
        " chars) — hit Profile.");
    } catch (e) {
      setStatus("could not read " + f.name + ": " + e.message);
    }
    fileEl.value = "";
  });

  btnSample.addEventListener("click", trySample);
  btnProfile.addEventListener("click", doProfile);
  btnSample.disabled = true;
  btnProfile.disabled = true;
  boot();
})();
