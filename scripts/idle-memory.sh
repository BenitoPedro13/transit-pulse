#!/usr/bin/env bash
# Median-of-3 idle memory per transit-pulse container, in MiB.
# Run it after `make up`, with the stack idle for at least 60 s. Needs bash, docker, awk and sort.
set -euo pipefail

SAMPLES=3
INTERVAL=10

# Convert a docker stats size ("123.4MiB", "1.2GiB", "800KiB", "512B") to MiB.
to_mib() {
  awk -v v="$1" 'BEGIN {
    n = v + 0
    if (v ~ /GiB$/) n *= 1024
    else if (v ~ /KiB$/) n /= 1024
    else if (v ~ /MiB$/) n *= 1
    else if (v ~ /B$/) n /= 1048576
    printf "%.1f", n
  }'
}

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

for i in $(seq 1 "$SAMPLES"); do
  docker stats --no-stream --format '{{.Name}} {{.MemUsage}}' |
    awk '$1 ~ /^transit-pulse-/ { print $1, $2 }' |
    while read -r name usage; do
      mib=$(to_mib "$usage")
      echo "sample $i  $name  $mib MiB"
      echo "$name $mib" >>"$tmp"
    done
  if [ "$i" -lt "$SAMPLES" ]; then
    sleep "$INTERVAL"
  fi
done

if [ ! -s "$tmp" ]; then
  echo "no transit-pulse-* containers running; run 'make up' first" >&2
  exit 1
fi

echo
echo "median of $SAMPLES (MiB):"
for name in $(awk '{ print $1 }' "$tmp" | sort -u); do
  median=$(awk -v n="$name" '$1 == n { print $2 }' "$tmp" | sort -n | awk '{ a[NR] = $1 } END { print a[int((NR + 1) / 2)] }')
  echo "$name  $median"
done
