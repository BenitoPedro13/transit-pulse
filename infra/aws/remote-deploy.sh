#!/bin/bash
# Runs ON the box, as root, through SSM Run Command (scripts/deploy.sh starts it).
#   remote-deploy.sh <git-sha>
# Pulls the release files from the ops bucket, writes .env from SSM (mode 0600), starts the
# stack and waits for the public health check. Exits non-zero if it never answers.
set -euo pipefail

SHA="${1:?usage: remote-deploy.sh <git-sha>}"
REGION=us-east-1
ACCOUNT="$(aws sts get-caller-identity --query Account --output text)"
OPS="transit-pulse-ops-${ACCOUNT}"
ECR="${ACCOUNT}.dkr.ecr.${REGION}.amazonaws.com"
HOST=transit-pulse.duckdns.org
BASE=/opt/transit-pulse
REL="${BASE}/releases/${SHA}"

mkdir -p "${REL}" /data/{redpanda,pg,raw,caddy}
aws s3 sync "s3://${OPS}/deploy/${SHA}/" "${REL}/" --region "${REGION}" --only-show-errors
ln -sfn "${REL}" "${BASE}/current"

# Data directories keep the owner of the process inside each image.
chown -R 101:101 /data/redpanda     # redpanda
chown -R 1000:1000 /data/pg         # postgres (timescaledb-ha)
chown -R 65532:65532 /data/raw      # distroless nonroot: adapter-its

param() { aws ssm get-parameter --region "${REGION}" --with-decryption --name "/transit-pulse/prod/$1" --query Parameter.Value --output text; }
PG_PASSWORD="$(param POSTGRES_PASSWORD)"
umask 077
cat > "${BASE}/.env" <<ENV
IMAGE=${ECR}/transit-pulse/go:${SHA}
POSTGRES_PASSWORD=${PG_PASSWORD}
DATABASE_URL=postgres://transit_pulse:${PG_PASSWORD}@timescaledb:5432/transit_pulse?sslmode=disable
ITS_CONTACT=$(param ITS_CONTACT)
API_HOST=${HOST}
HISTORY_HOURS=3
CADDYFILE_SHA256=$(sha256sum "${REL}/infra/aws/Caddyfile" | cut -d' ' -f1)
ENV
umask 022

aws ecr get-login-password --region "${REGION}" | docker login --username AWS --password-stdin "${ECR}" >/dev/null
cd "${REL}"
DC=(docker compose --project-directory "${REL}" --env-file "${BASE}/.env" -f "${REL}/infra/aws/compose.prod.yml")
"${DC[@]}" pull --quiet
"${DC[@]}" up -d --remove-orphans

for i in $(seq 1 24); do
  code="$(curl -s -o /tmp/healthz.json -w '%{http_code}' "https://${HOST}/healthz" || true)"
  if [ "${code}" = "200" ]; then
    echo "healthz 200 after $((i * 5)) s:"; cat /tmp/healthz.json; echo
    "${DC[@]}" ps
    exit 0
  fi
  echo "healthz attempt ${i}: HTTP ${code}"
  sleep 5
done
# A fresh box has an empty database: the API answers 503 "no snapshot yet" until the adapter's
# first minute has gone through Redpanda and the consumer. Caddy and the API are up in that case
# (a dead API would be 502), so it is reported, not failed. Check /healthz again a few minutes later.
if [ "${code}" = "503" ]; then
  echo "stack is up but the API has no snapshot yet (HTTP 503); check https://${HOST}/healthz in a few minutes"
  "${DC[@]}" ps
  exit 0
fi
echo "healthz never returned 200 (last HTTP ${code})" >&2
"${DC[@]}" ps >&2
"${DC[@]}" logs --tail 20 >&2
exit 1
