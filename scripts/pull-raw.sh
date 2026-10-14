#!/bin/bash
# The exit runbook (docs/architecture.md §7.1, docs/tasks/TASK-aws-infra.md §2.5.6): copy the raw log
# and its manifests from S3 to a local directory and check every manifest line with shasum.
#   scripts/pull-raw.sh [dest]        default dest: ./var/s3-raw
# Prints the counts and exits non-zero on any mismatch or missing file.
set -euo pipefail
cd "$(dirname "$0")/.."
DEST="${1:-var/s3-raw}"
BUCKET="transit-pulse-raw-$(aws sts get-caller-identity --query Account --output text)"
mkdir -p "$DEST"
aws s3 sync "s3://${BUCKET}/raw/" "${DEST}/raw/" --only-show-errors
aws s3 sync "s3://${BUCKET}/manifests/" "${DEST}/manifests/" --only-show-errors

ok=0; bad=0; missing=0
while IFS= read -r mf; do
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    sha="${line%%  *}"; rest="${line#*  }"; key="${rest%%  *}"; size="${rest##*  }"
    f="${DEST}/${key}"
    if [ ! -f "$f" ]; then echo "MISSING ${key}" >&2; missing=$((missing + 1)); continue; fi
    got="$(shasum -a 256 "$f" | cut -d' ' -f1)"; gotsize="$(wc -c < "$f" | tr -d ' ')"
    if [ "$got" = "$sha" ] && [ "$gotsize" = "$size" ]; then ok=$((ok + 1)); else echo "MISMATCH ${key}" >&2; bad=$((bad + 1)); fi
  done < "$mf"
done < <(find "${DEST}/manifests" -name '*.sha256' | sort)

total_files="$(find "${DEST}/raw" -type f | wc -l | tr -d ' ')"
echo "manifest lines ok=${ok} mismatched=${bad} missing=${missing}; raw files pulled=${total_files}"
[ "$bad" -eq 0 ] && [ "$missing" -eq 0 ]
