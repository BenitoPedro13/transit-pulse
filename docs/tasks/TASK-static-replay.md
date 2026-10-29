# TASK-static-replay
Written 2026-10-29 following user approval of the portfolio cost-control plan.

## 1. Current scenario
Live frontend on Vercel; real Go history-hour.v1 API exists, but no backend-independent export.
The public history API returns 503 before changes. Read-only SSM diagnosis found the 30 GiB
data filesystem full: PostgreSQL 22 GiB, raw log 7.1 GiB, Redpanda 778 MiB. Containers fail
on ENOSPC. Recent production raw logs are not being shipped (S3 manifests cover only the
old local 2026-10-12 capture and GTFS). The raw-sync implementation exists locally and
verifies checksums before sealing/pruning; its periodic production installation is incomplete.

## 2. Planned changes
1. Recover without enlarging EC2/EBS: pause failed ingestion, install the existing raw-sync
script on the root filesystem (which has space), upload without pruning, verify all sealed
objects and manifests, retain the durable archive in S3 (user explicitly declined a bulk Mac copy), then apply the existing four-day local pruning
policy only to verified sealed copies. Enable the existing timer for durable ongoing shipping.
Restart PostgreSQL/broker/consumer/collector in dependency order. No database file deletion.
2. Add a portable export CLI that reads the real history API, writes history-hour.v1 files,
snapshot metadata, SHA-256 manifest and provenance, and refuses empty/inconsistent captures.
Rebuild from raw first when exporting old dates; never invent missing coverage or positions.
3. Add a dedicated /replay page with a stable data-source context, reusing the dashboard,
map, timeline and query contracts. Static files are served under /replay-data/<capture>/.
No /api request from the replay route; original live page remains. Query keys separate sources.
Replay starts paused at a recorded minute, remains replay at its last frame, and offers replay
restart instead of a misleading live button. Label the recorded date and gaps.
4. Supply a backend-independent table route too. Add discoverable replay navigation.
5. Export a real verified capture, build with static assets, test with all API requests blocked,
and deploy the frontend to the existing Vercel project. Keep the collector running until the
previously agreed weekday/Sunday/event captures exist and their exports are verified;
Réveillon remains a future capture, not a claimed completed recording.

## 3. Why
Preserve real recordings and restore a useful portfolio without increasing instance size.
The static demo must survive backend outages and eventual retirement.

## 4. Affected files
| File | Change |
|---|---|
| scripts/export-replay.mjs | Offline packaging of recorded API history with provenance |
| apps/web/hooks/data-source.tsx | Stable live/replay context and typed URLs |
| apps/web/hooks/use-latest-vehicles.ts, use-fleet-stats.ts, use-lines.ts, use-history.ts | Shared query factories scoped to source |
| apps/web/components/playback-provider.tsx, timeline.tsx | Honest replay initialization and controls |
| apps/web/store/playback.ts | Replay-only end state |
| apps/web/hooks/use-playback.ts | Replay-only seek/restart behavior |
| apps/web/app/replay/page.tsx, replay/tabela/page.tsx | Static demo surfaces |
| apps/web/public/replay-data/ | Generated, ignored recordings shipped in deployment artifact |
| README.md, CLAUDE.md, docs/architecture.md | Operation, public links and current state |
| infra/aws/raw-sync.service, timer, script | Existing ship/seal/prune procedure enabled on production |

## 5. Verification
Existing raw SHA-256 verification passes; no unverified copy is pruned. Database restarts,
disk has headroom, public API answers real snapshots/history. Export verifies equal fix column
lengths, valid indices/time/coordinates, existing gap semantics and checksums. Pure unit tests
cover source separation and replay ending. Existing Go tests and relevant web tests/typecheck/
lint/build pass. Browser checks at desktop/mobile block all /api requests, render real buses,
scrub/play/seek to end, and verify replay/table labels and gaps. Public existing live page is
preserved. Record exact numbers and unresolved source gaps in Results below.

## Storage clarification (user, 2026-10-29)
Do not download the full production raw archive to the Mac. Keep full recordings in S3,
verify them from the server, and retain only the selected compact public replay locally.
The attempted bulk download never started. S3 must be migrated/exported elsewhere before
the Free plan account expires; do not assume this account provides perpetual free storage.

## Results
- Full production archive copied/sealed in S3 without a bulk Mac download. Stable raw-sync
  service/timer enabled, with a second manifest/remote checksum check before local pruning.
- Server disk recovered from 100% full to 1.4 GiB used / about 29 GiB free. Existing automatic
  database retention resumed; PostgreSQL/Redpanda healthy and ingestion restarted.
- A recovery verification command mistakenly used SELECT for Timescale's run_job procedure;
  that command made no change. Existing automatic retention had already freed the old chunks;
  a subsequent read verified the policy still scheduled and resumed ingestion successfully.
- Live history verified at newest_minute 2026-10-29T13:34:00Z after recovery.
- Preserved local recording rebuilt (118 complete minutes, 1,099,257 idempotent writes);
  compact export has 225,286 fixes and occupies about 8 MB. Temp DB/API removed.
- 106 web tests, TypeScript, lint and Vercel production build passed.
- Desktop/mobile Chrome replay tests with all /api blocked: 0 API requests, 0 page errors,
  playback/end-seek/restart/table working; table rendered 20 data rows and no horizontal overflow.
- Compatible Next.js/eslint-config-next patch to 16.4.3 applied for verified advisories.
- cfn/compute instance size, EBS size and live-route behavior were not enlarged/replaced.
- Remaining dependency audit findings outside Next.js are documented, not silently declared fixed.
