"""Throwaway: derive the E7/E8/E9 numbers from the adapter's log lines and the raw log."""
import collections
import datetime as dt
import glob
import gzip
import json
import statistics as st
import sys

sys.path.insert(0, ".")
import rawread  # noqa: E402

S = "."
LAG_S = 60
iso = lambda s: dt.datetime.fromisoformat(s.replace("Z", "+00:00"))

# --- adapter log lines
pub = {}      # minute -> last "minute published" record (last wins)
pub_all = collections.defaultdict(list)
for line in open(f"{S}/e7.log"):
    try:
        r = json.loads(line)
    except ValueError:
        continue
    if r.get("msg") == "minute published":
        pub[r["minute"]] = r
        pub_all[r["minute"]].append(r)

events = []  # (timestamp, text)
for line in open(f"{S}/e7-events.log"):
    ts, _, txt = line.strip().partition(" ")
    events.append((iso(ts), txt))
t0 = events[1][0]  # adapter started
print("events:")
for ts, txt in events:
    print("  ", ts.strftime("%H:%M:%S"), txt)

# --- raw log attempts
attempts = []
for p in sorted(glob.glob(f"{S}/e7-raw/its/v1/*/*/*/*.raw.gz")):
    for h, _ in rawread.envelopes(p):
        end = iso(h["fetched_at"])
        attempts.append({**h, "end": end, "start": end - dt.timedelta(milliseconds=h["duration_ms"])})
attempts.sort(key=lambda a: a["start"])
print("\nraw attempts:", len(attempts), "status counts:", dict(collections.Counter(a["status"] for a in attempts)),
      "with error:", sum(1 for a in attempts if a["error"]))

# minutes
by_min = collections.defaultdict(list)
for a in attempts:
    by_min[a["minute_utc"]].append(a)
def chains(m):
    """number of complete page-0..last chains among 200 attempts (should be exactly 1)."""
    ok = [a for a in by_min[m] if a["status"] == 200 and not a["error"]]
    pages = collections.Counter(a["page"] for a in ok)
    return pages
bad_dupe = [m for m in by_min if any(c > 1 for c in chains(m).values())]
print("minutes with a page fetched OK more than once:", bad_dupe)

# --- E7: live minutes = those whose end (M+60s) is after adapter start
live = sorted(m for m in pub if iso(m) + dt.timedelta(seconds=60) >= t0)
first_live = live[0]; last_live = live[-1]
rng = []
t = iso(first_live)
while t <= iso(last_live):
    rng.append(t.strftime("%Y-%m-%dT%H:%M:%SZ")); t += dt.timedelta(minutes=1)
missing = [m for m in rng if m not in pub]
print(f"\nlive range {first_live} .. {last_live}: {len(rng)} minutes, published {len(rng)-len(missing)}, missing {missing}")
# complete in raw?
incomplete = [m for m in rng if not any(a["status"] == 200 and not a["error"] and a["next_cursor"] is None for a in by_min[m])]
print("live minutes without a final page in the raw log:", incomplete)

# request spacing
starts = [a["start"] for a in attempts]
gaps = [(b - a).total_seconds() for a, b in zip(starts, starts[1:])]
print(f"\nrequest start spacing: min {min(gaps):.3f}s median {st.median(gaps):.3f}s; "
      f"requests in any 0.99s window: ", end="")
mx = 1
j = 0
for i in range(len(starts)):
    while (starts[i] - starts[j]).total_seconds() >= 0.99:
        j += 1
    mx = max(mx, i - j + 1)
print(mx)
spans = sorted(starts)
print("first/last request:", spans[0].strftime("%H:%M:%S"), spans[-1].strftime("%H:%M:%S"),
      f"overall {len(starts)/((spans[-1]-spans[0]).total_seconds()):.3f} req/s")

# requests per minute
rq = [len(by_min[m]) for m in rng]
print("requests/min live minutes: median", st.median(rq), "max", max(rq), "dist", dict(collections.Counter(rq)))
rq_all = [len(v) for v in by_min.values()]
print("requests/min all minutes: median", st.median(rq_all))

# records / bytes per minute (live range)
rec = [pub[m]["records"] for m in rng if m in pub]
gz = [pub[m]["raw_bytes_gz"] for m in rng if m in pub]
print(f"records/min median {st.median(rec):.0f}  raw gz bytes/min median {st.median(gz)/1024:.1f} KiB ({st.median(gz):.0f} B)")
print(f"events/min median {st.median([pub[m]['events'] for m in rng if m in pub]):.0f}; "
      f"dups median {st.median([pub[m]['dups'] for m in rng if m in pub]):.0f}; "
      f"invalid median {st.median([pub[m]['invalid'] for m in rng if m in pub]):.0f}; "
      f"conflicting median {st.median([pub[m]['conflicting_duplicates'] for m in rng if m in pub]):.0f}")
print("null_heading median", st.median([pub[m]["null_heading"] for m in rng if m in pub]),
      "null_speed median", st.median([pub[m]["null_speed"] for m in rng if m in pub]))

# lag to publish: split by "disturbed" (published within an E8/E9 pause window or catching up from it)
def when(label_part):
    for ts, txt in events:
        if label_part in txt:
            return ts
e8_stop = when("E8: stopping"); e8_restart = when("E8: restarting")
e9_stop = when("E9: stopping"); e9_up = when("E9: redpanda healthy")
def disturbed(m):
    end = iso(m) + dt.timedelta(seconds=60)
    r = pub[m]
    pub_at = end + dt.timedelta(milliseconds=r["lag_to_publish_ms"])
    # minutes whose publish or fetch window overlaps the outages, plus catch-up
    wins = []
    if e8_stop and e8_restart: wins.append((e8_stop, e8_restart + dt.timedelta(minutes=8)))
    if e9_stop and e9_up: wins.append((e9_stop, e9_up + dt.timedelta(minutes=5)))
    for a, b in wins:
        if a and b and (a - dt.timedelta(minutes=2) <= pub_at <= b or a <= end <= b):
            return True
    return False
steady = [m for m in rng if m in pub and not disturbed(m)]
lags = sorted(pub[m]["lag_to_publish_ms"] for m in steady)
def p(q, xs): return xs[min(len(xs) - 1, int(round(q * (len(xs) - 1))))]
print(f"\nlag_to_publish_ms (steady-state, {len(lags)} minutes): median {st.median(lags)/1000:.1f}s p95 {p(0.95, lags)/1000:.1f}s max {lags[-1]/1000:.1f}s "
      f"(limit {LAG_S + 30}s)")
dist = [m for m in rng if m in pub and disturbed(m)]
print("disturbed minutes:", len(dist), "lags s:", sorted(round(pub[m]["lag_to_publish_ms"]/1000) for m in dist))

# E8
if e8_stop and e8_restart:
    gap_minutes = [m for m in rng if e8_stop - dt.timedelta(seconds=60) <= iso(m) + dt.timedelta(seconds=60) + dt.timedelta(seconds=LAG_S) <= e8_restart + dt.timedelta(seconds=120)]
    print("\nE8 pause", e8_stop.strftime("%H:%M:%S"), "->", e8_restart.strftime("%H:%M:%S"))
    due_during = [m for m in rng if e8_stop <= iso(m) + dt.timedelta(seconds=60 + LAG_S) <= e8_restart]
    print("  minutes that became due during the pause:", len(due_during), "all published:", all(m in pub for m in due_during))
    after = [a for a in attempts if a["start"] >= e8_restart]
    first_after = after[:60]
    ss = [a["start"] for a in after]
    gp = [(y - x).total_seconds() for x, y in zip(ss, ss[1:])]
    print("  after restart: min request spacing %.3fs over %d requests" % (min(gp), len(ss)))
    refetched = [m for m in due_during if any(a["start"] < e8_stop for a in by_min[m])]
    print("  minutes in the pause that already had attempts before it:", refetched)

# E9
if e9_stop and e9_up:
    during = [m for m in by_min if any(e9_stop <= a["end"] <= e9_up for a in by_min[m])]
    print("\nE9 outage", e9_stop.strftime("%H:%M:%S"), "->", e9_up.strftime("%H:%M:%S"), f"({(e9_up-e9_stop).total_seconds():.0f}s incl. restart)")
    cap = [m for m in during if any(a["status"] == 200 and not a["error"] and a["next_cursor"] is None and e9_stop <= a["end"] <= e9_up for a in by_min[m])]
    print("  minutes completed in the raw log during the outage:", len(cap), sorted(m[11:16] for m in cap))
    print("  all of them published afterwards:", all(m in pub for m in cap),
          "max lag s:", max(pub[m]["lag_to_publish_ms"] for m in cap if m in pub) / 1000)
    outage_minutes = [m for m in rng if e9_stop <= iso(m) + dt.timedelta(seconds=60 + LAG_S) <= e9_up]
    print("  minutes that became due during the outage:", len(outage_minutes), "fetched during it:", sum(1 for m in outage_minutes if m in cap))

# RSS
if not open(f"{S}/e7-rss.log").read(): sys.exit(0)
rss = [(iso(l.split()[0]), int(l.split()[1])) for l in open(f"{S}/e7-rss.log")]
print("\nRSS MiB (every minute): min %.1f median %.1f max %.1f" % tuple(x / 1024 for x in (min(r for _, r in rss), st.median([r for _, r in rss]), max(r for _, r in rss))))
for mins in (10, 20, 40, 60):
    pick = min(rss, key=lambda r: abs((r[0] - t0).total_seconds() - mins * 60))
    print(f"  at ~{mins} min: {pick[1]/1024:.1f} MiB")
