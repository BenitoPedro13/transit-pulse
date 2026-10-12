"""Throwaway: measure how long the ITS API takes to finish a minute's data (TASK-its-adapter §6 step 2).

For 30 consecutive minutes M, fetch all pages at M+90s, M+3m and M+10m, and record the record
count and the SHA-256 of the sorted rows. Sequential, with >= 1.2 s between requests.
"""
import hashlib
import json
import sys
import time
import urllib.request
from datetime import datetime, timedelta, timezone

BASE = "https://its.mobilidade.rio/v1/geolocalizacao/veiculos"
UA = "transit-pulse/dev (lag measurement, throwaway script)"
OUT = sys.argv[1]
N = 30
# Offsets are from the START of minute M; the minute closes at M+60s, so lag-after-close = offset - 60s.
OFFSETS = [("90s", 90), ("2m", 120), ("3m", 180), ("10m", 600)]

last_req = 0.0


def get(url):
    global last_req
    wait = 1.2 - (time.monotonic() - last_req)
    if wait > 0:
        time.sleep(wait)
    last_req = time.monotonic()
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=45) as r:
        return r.read()


def fetch_minute(m):
    rows, pages, cursor, statuses = [], 0, None, None
    t0 = time.monotonic()
    while True:
        url = f"{BASE}?minuto_utc={m}&limit=5000" + (f"&cursor={cursor}" if cursor else "")
        d = json.loads(get(url))
        pages += 1
        rows += d["data"]
        statuses = d["providers_status"]
        cursor = d["next_cursor"]
        if not cursor:
            break
    digest = hashlib.sha256("\n".join(sorted(json.dumps(r, sort_keys=True) for r in rows)).encode()).hexdigest()
    by = {}
    for r in rows:
        by[r["fornecedor"]] = by.get(r["fornecedor"], 0) + 1
    return {"records": len(rows), "pages": pages, "sha": digest, "by_supplier": by,
            "providers_status": statuses, "fetch_s": round(time.monotonic() - t0, 2)}


now = datetime.now(timezone.utc)
m0 = now.replace(second=0, microsecond=0)
jobs = []
for i in range(N):
    m = m0 + timedelta(minutes=i)
    for label, off in OFFSETS:
        jobs.append((m + timedelta(seconds=off), m, label))
jobs.sort(key=lambda j: j[0])

with open(OUT, "a") as f:
    for due, m, label in jobs:
        delay = (due - datetime.now(timezone.utc)).total_seconds()
        if delay > 0:
            time.sleep(delay)
        started = datetime.now(timezone.utc)
        try:
            res = fetch_minute(m.strftime("%Y-%m-%dT%H:%M:%SZ"))
        except Exception as e:  # recorded, not hidden
            res = {"error": repr(e)}
        rec = {"minute": m.strftime("%Y-%m-%dT%H:%M:%SZ"), "offset": label,
               "due": due.isoformat(), "started": started.isoformat(), **res}
        f.write(json.dumps(rec) + "\n")
        f.flush()
