#!/usr/bin/env python3
"""Ship the raw log to S3, seal old hours with a checksum manifest, prune the local copy.

docs/tasks/TASK-aws-infra.md §2.5. Standard library only (the AWS CLI does the S3 calls). Runs on
the box every 10 minutes (raw-sync.timer) and on the Mac for the GTFS log (`make gtfs-raw-upload`).

1. `aws s3 sync` of --root to s3://<bucket>/<prefix> (state/ files are not raw data and stay out).
2. Sealing: an hour file more than --seal-hours old can no longer grow. Its local SHA-256 is checked
   against the S3 object's SHA-256 checksum, and a line `<sha256>  <key>  <bytes>` goes into
   s3://<bucket>/manifests/<ns>YYYY/MM/DD.sha256 (rewritten whole; versioning keeps the old ones).
3. Pruning: a local file is deleted only when it is sealed, its manifest line is in S3, and it is
   more than --prune-days old. --no-prune turns this off.
4. Metrics (--metrics, box only): RawSyncSucceeded=1 on a clean run, RawNewestFileAgeSeconds always.

Multipart uploads (anything over 8 MiB, i.e. every real hour file) get a composite S3 checksum, not
a whole-file one, so the same composite is computed locally from the 8 MiB parts and compared.
"""
import argparse
import base64
import hashlib
import os
import re
import subprocess
import sys
import time
from datetime import datetime, timezone

PART = 8 * 1024 * 1024  # the AWS CLI's default multipart_chunksize
HOUR_FILE = re.compile(r"(?:^|/)v1/(\d{4})/(\d{2})/(\d{2})/(\d{2})\.raw\.gz$")
REGION = "us-east-1"


def aws(*args, capture=True, stdin=None, quiet=False):
    cmd = ["aws", "--region", REGION, *args]
    return subprocess.run(cmd, check=True, text=True, input=stdin,
                          stdout=subprocess.PIPE if capture else None,
                          stderr=subprocess.DEVNULL if quiet else None)


def sha256_and_composite(path):
    """Whole-file SHA-256 (hex) and the S3 composite checksum for 8 MiB parts (base64 + '-N')."""
    whole, parts = hashlib.sha256(), []
    with open(path, "rb") as f:
        while chunk := f.read(PART):
            whole.update(chunk)
            parts.append(hashlib.sha256(chunk).digest())
    composite = base64.b64encode(hashlib.sha256(b"".join(parts)).digest()).decode() + f"-{len(parts)}"
    return whole.hexdigest(), whole.digest(), composite


def head(bucket, key):
    import json
    out = aws("s3api", "head-object", "--bucket", bucket, "--key", key,
              "--checksum-mode", "ENABLED", "--output", "json").stdout
    return json.loads(out)


def verify_remote(bucket, key, path, size):
    hex_digest, raw_digest, composite = sha256_and_composite(path)
    meta = head(bucket, key)
    remote = meta.get("ChecksumSHA256", "")
    if meta.get("ContentLength") != size:
        return None
    if "-" in remote:
        ok = remote == composite
    else:
        ok = remote == base64.b64encode(raw_digest).decode()
    return hex_digest if ok else None


def read_manifest(bucket, key):
    try:
        text = aws("s3", "cp", f"s3://{bucket}/{key}", "-", "--only-show-errors", quiet=True).stdout
    except subprocess.CalledProcessError:
        return {}
    lines = {}
    for line in text.splitlines():
        sha, k, size = line.split("  ")
        lines[k] = (sha, int(size))
    return lines


def put_metric(name, value, unit):
    aws("cloudwatch", "put-metric-data", "--namespace", "TransitPulse",
        "--metric-name", name, "--value", str(value), "--unit", unit, capture=False)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", required=True)
    ap.add_argument("--bucket", required=True)
    ap.add_argument("--prefix", default="raw/")
    ap.add_argument("--manifest-ns", default="", help='manifest key namespace, e.g. "gtfs/"')
    ap.add_argument("--seal-hours", type=float, default=25)
    ap.add_argument("--prune-days", type=float, default=4)
    ap.add_argument("--no-prune", action="store_true")
    ap.add_argument("--metrics", action="store_true")
    a = ap.parse_args()
    now = time.time()
    ok = True

    files = []
    for dirpath, _, names in os.walk(a.root):
        for n in names:
            p = os.path.join(dirpath, n)
            rel = os.path.relpath(p, a.root).replace(os.sep, "/")
            if "/state/" in "/" + rel:
                continue
            files.append((p, rel))

    if a.metrics and files:
        newest = max(os.path.getmtime(p) for p, _ in files)
        put_metric("RawNewestFileAgeSeconds", int(now - newest), "Seconds")

    try:
        aws("s3", "sync", a.root, f"s3://{a.bucket}/{a.prefix}", "--exclude", "*/state/*",
            "--exclude", "state/*", "--checksum-algorithm", "SHA256", "--only-show-errors", capture=False)
    except subprocess.CalledProcessError as e:
        print(f"sync failed: {e}", file=sys.stderr)
        ok = False

    manifests, touched = {}, set()
    if ok:
        for p, rel in sorted(files):
            m = HOUR_FILE.search(rel)
            if not m:
                continue
            y, mo, d, h = m.groups()
            hour_end = datetime(int(y), int(mo), int(d), int(h), tzinfo=timezone.utc).timestamp() + 3600
            if now - hour_end < a.seal_hours * 3600:
                continue
            key = a.prefix + rel
            mkey = f"manifests/{a.manifest_ns}{y}/{mo}/{d}.sha256"
            if mkey not in manifests:
                manifests[mkey] = read_manifest(a.bucket, mkey)
            size = os.path.getsize(p)
            have = manifests[mkey].get(key)
            if have and have[1] == size:
                continue
            sha = verify_remote(a.bucket, key, p, size)
            if sha is None:
                print(f"checksum mismatch, not sealing: {key}", file=sys.stderr)
                ok = False
                continue
            manifests[mkey][key] = (sha, size)
            touched.add(mkey)

        for mkey in touched:
            body = "".join(f"{sha}  {k}  {size}\n" for k, (sha, size) in sorted(manifests[mkey].items()))
            try:
                aws("s3", "cp", "-", f"s3://{a.bucket}/{mkey}", "--only-show-errors", capture=False, stdin=body)
                print(f"sealed {len(manifests[mkey])} file(s) in {mkey}")
            except subprocess.CalledProcessError as e:
                print(f"manifest upload failed: {e}", file=sys.stderr)
                ok = False
                manifests[mkey] = {}

        if not a.no_prune:
            for p, rel in files:
                m = HOUR_FILE.search(rel)
                if not m:
                    continue
                y, mo, d, h = m.groups()
                mkey = f"manifests/{a.manifest_ns}{y}/{mo}/{d}.sha256"
                have = manifests.get(mkey, {}).get(a.prefix + rel)
                hour_end = datetime(int(y), int(mo), int(d), int(h), tzinfo=timezone.utc).timestamp() + 3600
                if have and have[1] == os.path.getsize(p) and now - hour_end >= a.prune_days * 86400:
                    os.remove(p)
                    print(f"pruned {rel}")

    if a.metrics and ok:
        put_metric("RawSyncSucceeded", 1, "Count")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
