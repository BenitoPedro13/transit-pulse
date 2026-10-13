"""Measure three offline GTFS exports on macOS. Run after building var/gtfs-export."""
import gzip
import json
import pathlib
import re
import statistics
import subprocess
import tempfile
import time
import urllib.request

root = pathlib.Path(__file__).resolve().parents[4]
runs = []
for _ in range(3):
    with tempfile.TemporaryDirectory(prefix="transit-gtfs-measure-") as output:
        start = time.perf_counter()
        result = subprocess.run(
            ["/usr/bin/time", "-l", str(root / "var/gtfs-export"), "export", "-out", output],
            cwd=root, text=True, capture_output=True, check=True,
        )
        elapsed = time.perf_counter() - start
        rss = int(re.search(r"(\d+)\s+maximum resident set size", result.stderr)[1])
        runs.append({"seconds": round(elapsed, 3), "peak_rss_bytes": rss})
        index = json.loads((pathlib.Path(output) / "index.json").read_text())
        files = [(e["line"], pathlib.Path(output) / e["file"].removeprefix("/gtfs/")) for e in index["lines"]]
        sizes = [(line, len(p.read_bytes()), len(gzip.compress(p.read_bytes(), mtime=0))) for line, p in files]
        payloads = [json.loads(p.read_text()) for _, p in files]

stats = {
    "feed": index["feed"], "runs": runs,
    "median_seconds": statistics.median(r["seconds"] for r in runs),
    "median_peak_rss_bytes": statistics.median(r["peak_rss_bytes"] for r in runs),
    "lines": len(files), "patterns": sum(len(p["patterns"]) for p in payloads),
    "unique_stops": len({s["id"] for p in payloads for s in p["stops"]}),
    "json_bytes": sum(s[1] for s in sizes), "gzip_bytes": sum(s[2] for s in sizes),
    "largest_gzip": dict(zip(["line", "json_bytes", "gzip_bytes"], max(sizes, key=lambda s: s[2]))),
}
with urllib.request.urlopen("http://127.0.0.1:8080/v1/vehicles/latest") as response:
    observed = {v["line"] for v in json.load(response)["vehicles"] if v["line"] is not None}
known = {e["line"] for e in index["lines"]}
stats["unmatched_snapshot_lines"] = sorted(observed - known)
stats["matched_snapshot_lines"] = len(observed & known)
path = pathlib.Path(__file__).with_name("export-results.json")
path.write_text(json.dumps(stats, indent=2, ensure_ascii=False) + "\n")
print(json.dumps(stats, indent=2, ensure_ascii=False))
assert stats["median_seconds"] < 10
assert stats["median_peak_rss_bytes"] < 512 * 1024 * 1024
assert stats["gzip_bytes"] < 15 * 1024 * 1024
assert stats["largest_gzip"]["gzip_bytes"] < 500 * 1024
