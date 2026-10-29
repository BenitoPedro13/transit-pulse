# Static replay and recording archive

## Public views
- Live map: https://transit-pulse-tau.vercel.app/
- Backend-independent recorded map: https://transit-pulse-tau.vercel.app/replay
- Recorded table: https://transit-pulse-tau.vercel.app/replay/tabela

The replay route reads only packaged files under /replay-data/<capture>/ and never /api.
It starts paused, keeps real gaps, stays in replay when reaching the last frame, and offers
Recomeçar instead of pretending to go live. The table preserves a shared minute and line.
The original live route remains available. Next.js and its matching ESLint config were patched
from 16.4.0 to 16.4.3 after verified advisories blocked the first build attempt.

## Current capture
2026-10-12, 14:27–16:24 UTC (118 observed minutes), rebuilt from the preserved real raw log.
The rebuild applied 1,099,257 writes, deduplicated by the existing idempotent store rules.
The packaged replay contains 225,286 sampled fixes. Its directory is approximately 8 MB.
The hour files preserve their partial-hour boundaries; missing minutes outside the observed
window are not presented as a zero fleet. This is not a complete day or a Réveillon capture.

Generated files are deliberately ignored by Git. Vercel Git builds run the app's vercel-build
script, which downloads the compact release pinned in apps/web/assets-release.json and verifies
the bundle and every file before installing the complete replay and GTFS directories in public/.
The deployment includes the whole capture folder, its capture.json and SHA256SUMS.
Never overwrite a published capture under the same name; use a new capture id for new bytes.

## Export
Use a real read-only API backed by the desired recording. For an older recording, rebuild the
retained raw log into an isolated local or remote database first, with its historical retention
job disabled. Never point that rebuild at the production database.

    node scripts/export-replay.mjs --source http://127.0.0.1:8096/v1       --out apps/web/public/replay-data/2026-10-12       --raw-manifest var/runs/20261012-its-adapter-raw.sha256

The tool checks the history contract, fix columns/indices, coverage, non-empty observations,
snapshot/index consistency, a stable window during export and SHA-256 of every written file.
The provenance records the source manifest hash; no provider is queried during packaging.
NEXT_PUBLIC_REPLAY_CAPTURE selects the packaged folder (default 2026-10-12).

## Full archive stays in S3
User decision, 2026-10-29: no bulk raw-log download to the Mac. Keep the complete production
archive in transit-pulse-raw-860897618882, with sealed SHA-256 manifests. Only compact selected
replay bundles and small verification evidence need local disk space.

Production runs raw-sync.timer every ten minutes. The stable installed script is
/opt/transit-pulse/raw-sync.py; deployments install it before enabling the timer.
Closed hours older than 25 hours are checksum-verified and sealed. Local server copies older
than four days are pruned only after their manifest exists and their local bytes match both
the manifest and the current remote checksum. The instance cannot delete S3 objects.

S3 in this Free-plan account is not perpetual free storage: choose an approved durable remote
destination or paid-plan continuation before the account/credits expire and verify the copy.
Do not create another Free-plan account as an assumed entitlement. A full local pull remains
an explicit optional exit command, not a default backup step.

## Recovery on 2026-10-29
Before this work the 30 GiB data disk was full (about 22 GiB PostgreSQL, 7.1 GiB raw and 778 MiB
Redpanda). The public API returned 503. The already-written but incompletely deployed shipping
process had left recent raw files on the server.
- Paused failed ingestion, copied all recordings to S3 without pruning, and verified/sealed
  the eligible hour files and manifests.
- Reclaimed only verified old local copies and enabled the shipping timer. The existing
  scheduled database retention job cleared expired projection chunks once disk space returned.
- PostgreSQL and Redpanda became healthy; ingestion restarted. Data disk was then 1.4 GiB used,
  about 29 GiB free, with no volume enlargement.
- Live history later answered 200 with newest_minute 2026-10-29T13:34:00Z.
- AWS reclaimed both Spot instances during recovery; this was capacity-not-available, not a
  deployment stop. Their persistent requests resumed when capacity returned.
- No complete raw archive was downloaded to the Mac. The temporary local replay API/database
  and its anonymous Docker volume were removed after the compact export was verified.

The previous ordinary-weekday/Sunday/event capture goals still apply. Keep collection running
until the intended event capture exists and its export is verified, or explicitly revise that
goal. The current compact replay is useful now but does not claim those future recordings exist.

## Verification
106 web tests passed, with typecheck/lint and the production Vercel build passing.
Chrome desktop/mobile checks blocked every /api request: replay map, playback, end-seek,
restart and table worked with zero API requests, zero page errors and no mobile overflow.
Python shipping script syntax and remote-deploy shell syntax passed. Server S3 offload and
periodic service were verified through SSM. Browser reports/screenshots are small local
operations evidence outside Git. A pre-existing broader dependency audit has other advisories;
the targeted Next.js patch is not a claim that every dependency is now vulnerability-free.

## Reproducible Vercel Git builds
The existing project's Root Directory is apps/web (not the Go repository root), framework Next.js,
Node 24.x, with default install/build commands. package.json pins pnpm and provides vercel-build.
Preview and production project variables both contain:
- API_ORIGIN=https://transit-pulse.duckdns.org
- NEXT_PUBLIC_TILES_BASE_URL=https://transit-pulse-tiles-860897618882.s3.us-east-1.amazonaws.com

The existing public tiles bucket also holds the content-addressed web-releases/<sha256>.json.gz
object. It contains 502 already-public replay/GTFS projection files: 21,845,208 expanded bytes,
6,264,515 downloaded bytes. It contains no raw logs or map tile archive. The map reads rio.pmtiles
from the existing public S3 endpoint; no 43 MB tile copy is needed in the Git build.
A build fails on unavailable/corrupt assets rather than deploying an empty replay.
This release still depends on the existing AWS account; preserve/migrate it with the archive before
the Free plan expires. Existing deployed replay files do not request this bundle at runtime.

To publish a new compact release after exporting real data and GTFS:
1. In apps/web, run node scripts/public-assets.mjs --pack /absolute/path/to/new-release.
2. Upload the generated <sha256>.json.gz to the existing tiles bucket under web-releases/,
   with content type application/gzip, immutable cache control, and If-None-Match: *.
   Never upload raw recordings to this public bucket.
3. Copy the generated assets-release.json into apps/web/assets-release.json and review its inventory.
4. Run pnpm assets:prepare, pnpm test:assets and the ordinary web checks, then commit the pin.
   A push creates a fresh Git preview. Root Directory is a Vercel project setting, not inferred
   from a local CLI working directory. Run repo-based CLI deployments from the repository root.
