# Architecture — Transit Pulse

> **Status (2026-10-12): skeleton, with the source inventory resolved.** This document
> records decisions made in the planning conversation. Provider facts in §2–§4, §6–§8 and
> §10 were checked against live sources on 2026-10-12 (`docs/research/README.md`, with
> samples in `docs/research/samples/`). Anything still unconfirmed is marked `[VERIFY]`.
> Sections get filled in as task documents resolve them.

## 1. Problem statement and scope

The project is a live public-transport map for Greater Rio de Janeiro. It shows where
vehicles are, how late each line is running, and how much of the network has any public data
at all. It's also a portfolio case study in distributed, data-heavy real-time systems:

- many heterogeneous providers;
- one canonical event;
- an immutable raw log;
- rebuildable projections;
- thousands of vehicles on a map.

**In scope:** the operators listed in §4, in phase order.

**Out of scope for v1:**

- Synthetic or simulated positions of any kind.
- Passenger counts or crowding (no known public source `[VERIFY]`).
- Trip planning or routing.
- Map-matching. OSRM/Valhalla is a stretch goal after phase 2.

## 2. Coverage tiers

Every operator, and every line within it, is in exactly one tier at any moment:

| Tier | Meaning | How it's drawn |
|---|---|---|
| `live` | A provider publishes vehicle positions we can poll | moving vehicles, with a "last seen" age |
| `scheduled` | Only a static timetable (GTFS) exists | route lines and timetable, labelled as scheduled; **never** drawn as moving vehicles |
| `none` | No public data found | listed in the coverage panel as having no public data |

Within `live`, these are separate states, never collapsed into zero:

- "no vehicles reported";
- "fetch failed";
- "stale beyond N minutes". Measured on 2026-10-12, a moving bus reports every ~30 s
  (p50) and reaches the ITS API with a lag of p99 232 s. A vehicle whose last
  `event_time` is more than **5 minutes** old is therefore stale. *Proposed*, to be
  confirmed in the phase 1 task. Snapshot feeds (the BRT `sonda` data) also carry vehicles
  parked for days, with timestamps up to years old, so this cut-off is required, not
  optional.

## 3. The canonical event (v1)

Every adapter emits this shape. Nothing downstream of the adapter knows or branches on which
provider an event came from. **v1 is final as of `docs/tasks/TASK-its-adapter.md`
(2026-10-12).** The contract is `packages/contracts/position.v1.schema.json` (JSON Schema, draft
2020-12, `additionalProperties: false`); the Go type is `packages/event` (`Position`), and a
test fails if the two disagree.

| Field | Meaning | Notes |
|---|---|---|
| `city` | Metro area | enum `rio` for now |
| `mode` | `bus` / `brt` / `rail` / `metro` / `tram` / `ferry` | enum, final |
| `operator` | Operator identifier | our own stable slug, not the provider's string. ITS: `rio-sppo`, `rio-sistema-rio`, `rio-brt` (the table is in `apps/adapter-its/mapping.go`) |
| `authority` | `city` / `state` | who regulates it |
| `source_kind` | `live` / `scheduled` | invariant 6; always `live` from the ITS adapter |
| `feed` | Opaque device-source tag, `<system>:<supplier>` | ITS: `its:conecta`, `its:zirix`, `its:maxtrack`, `its:sonda`. Part of the identity (§10 #9). **Nothing downstream may branch on its value** |
| `vehicle` | Vehicle identifier as the provider gives it | stable across polls and identical across all three SMTR paths (verified 2026-10-12). Formats differ by system: SPPO `A29001`, Sistema RIO `A2-003`, BRT `901008` |
| `line` | Line identifier, or null | ITS `servico` (= GTFS `route_short_name`, 85–100% join; misses are `ESPECIAL`, `LECD…`, `0`). `""` becomes null; anything else, including `"0"`, is kept as given |
| `route_id` | GTFS `route_id`, or null | ITS `route_id` (≈99% join where present). `""` becomes null. `trip_id` is not carried: it joins only ≈44% for buses because the public GTFS is stale (research §2) |
| `lat`, `lon` | WGS84 position | decimal degrees, finite and in range. `(0, 0)` is invalid |
| `event_time` | When the provider says the position was taken | RFC 3339 UTC. ITS API: `datetime`, real UTC. GTFS-RT: POSIX seconds. Legacy BRT: epoch ms, UTC. **Legacy SPPO: ISO with a `Z` that is false; it's America/Sao_Paulo (UTC−3)** |
| `received_time` | When our adapter received it | RFC 3339 UTC; the raw envelope's `fetched_at`, so a replay reproduces it |
| `speed_kmh` | Speed, or null | ITS `velocidade`. Null if absent or negative; never clamped |
| `heading_deg` | Heading, or null | ITS `direcao`. Null if absent or outside [0, 360]; never clamped |
| `raw_ref` | `{key, minute_utc, page}` | where the source bytes live: the raw-log object, the ITS minute and the page (invariant 2) |

- **Identity and format** are decided in §10 (#7, #9): `(operator, feed, vehicle, event_time)`.
  `Position.Key()` (`operator/feed/vehicle`) is the Kafka message key, so all of one device's
  fixes land in one partition in order; `Position.Identity()` adds `event_time`. JSON on a
  versioned topic (`positions.v1`, 6 partitions, 24 h retention: the broker is a buffer, the
  raw log is the record).
- **`feed-status.v1`** (`packages/contracts/feed-status.v1.schema.json`) has one message per
  `(feed, minute)` on a compacted topic, keyed `feed/minute_utc`, so a reader can tell "no
  vehicles reported" from "fetch failed" (invariant 6). `status` is `ok`, `failed` (our fetch
  failed after retries), `provider_not_ok` (`providers_status` for that supplier isn't `"ok"`)
  or `gap` (a minute older than the backfill window that was never captured). The latest
  message per key wins, so a minute that fails and is recovered later ends as `ok`.
- **Delivery is at-least-once, and the provider repeats fixes.** After a crash an adapter
  re-publishes from its checkpoint, so the topic can hold the same event twice. Separately, the
  ITS API re-serves the same fix in later minutes: in a 118-minute run 4.6% of published
  records (52,854 of 1,152,042) were repeats of an identity already published, none of them
  from a re-publish (research §1.1). The consumer is idempotent on `Identity()`, keeping the
  copy from the earliest raw minute (§10 #7), and that is required, not a safety net.
- **Identity collides across suppliers (2026-10-12).** In the ITS API, two AVL suppliers
  (`conecta` and `zirix`) report the same SPPO `id_veiculo` from two different devices. 3,114
  `(vehicle, datetime)` pairs exist in both, with different positions. Resolved by §10 #9:
  the supplier joins the identity as `feed`.
- **A fix is dropped as a duplicate only if it is exact** (same identity, same position, speed
  and heading). Two different positions under one identity are counted as
  `conflicting_duplicates` and the first is kept. See research §1.1 for the one device that does
  this.

## 4. Sources and phases

The full per-operator inventory is `docs/research/README.md`. Tiers were verified against
live sources on 2026-10-12.

| Phase | Scope | Verified tier | Deliverable |
|---|---|---|---|
| 1 | Rio city buses (SPPO and the new "Sistema RIO"), BRT | **live**: three public, keyless SMTR paths (ITS JSON API, GTFS-RT, legacy JSON), all beta | the pipeline and the "Rio em movimento" dashboard: the live map, line and bus exploration, and a time machine over recorded hours (`docs/research/ux-direction.md`, decided 2026-10-12; it replaces the unspecified "delays by line"). **99.5% of GTFS trips are frequency-based, so "delay" means headway adherence**, not lateness against a timetable |
| 2 | SuperVia (now TrensRJ), MetrôRio, VLT Carioca, Barcas (now Barcas Rio) | **none**: no official GTFS anywhere, only HTML timetables. ⚠ **This contradicts the phase plan** (§10 #11) | a per-operator coverage panel; the scheduled layer depends on #11 |
| 3 | Historical Rio data | historical: `rj-smtr` on BigQuery exists (`monitoramento.gps_onibus_*`, `br_rj_riodejaneiro_veiculos.gps_sppo`, …). Public access `[VERIFY: needs a GCP account]` | a backfill, plus replay. ≈ 2.1–2.4 GB scanned per day of every supplier; 1 TiB/month is free, then USD 6.25/TiB |
| 4 | Niterói buses (none: route geometry only), DETRO-RJ intercity buses (none: an HTML lookup form), the rest of the metro area; then São Paulo; then GTFS-RT cities | none / `[VERIFY]` | more adapters, same canonical event |

## 5. System shape

```
 providers ──▶ adapters (one per source) ──▶ raw log: S3, compressed, kept forever
                     │
                     ▼ canonical events
                 Redpanda ──▶ consumer(s) ──▶ Postgres + TimescaleDB + PostGIS
                                                  │  full detail: a few days
                                                  │  aggregates: forever
                                                  ▼
                                    API (precomputed views + live positions)
                                                  │
                                                  ▼
                                Caddy ──▶ web: MapLibre + deck.gl + table view

 later:  raw log ──▶ replay export (chosen days) ──▶ S3 + CloudFront (static, backend off)
```

- **Raw first.** An adapter writes the provider's response to the raw log before, or
  independently of, publishing events. Losing the broker or the database loses nothing that
  can't be rebuilt (invariant 2).
- **Readers are isolated.** Viewers hit precomputed views and cached positions, never the
  adapters and never a provider (invariants 4 and 5).
- **The consumer and the rebuild share one write path (built 2026-10-12).** `apps/consumer`
  reads `positions.v1` and `feed-status.v1` in the group `transit-pulse-consumer`, writes a
  batch to TimescaleDB in one transaction, and commits the offsets only after it. `apps/rebuild`
  maps the raw log of a minute range with the same `packages/its` code and writes it through the
  same `packages/store` functions. Both writes are idempotent and order-independent, so a table
  rebuilt from the raw log equals the one the consumer built live (see §10 #7 and
  `docs/tasks/TASK-positions-consumer.md`).

- **The API serves a snapshot from memory (built 2026-10-12).** `apps/api` never queries on a
  request. Every 10 s a refresher runs a tiny probe on `feed_status` (the newest row of each feed);
  only when the result differs from the one the snapshot was built from does it run the one snapshot
  query (the newest fix per `(operator, vehicle)` in the 300 s before the end of the newest minute
  with positions), encode it as JSON and gzip, and swap it in atomically. A viewer spike therefore
  costs no queries, and a stopped database leaves the last snapshot being served (`/healthz` says
  `refresh_ok: false`). `as_of_minute` is the newest minute with positions, so a newer `failed` or
  `gap` minute appears in `coverage` and never as zero vehicles (invariant 6). Measured in
  `docs/tasks/TASK-live-map.md` §7: 2,627 vehicles, 48.6 KB gzipped, p95 ≤ 1 ms. The one cost to watch
  is the snapshot query, a sequential scan of the newest chunk because `positions` has no time index.
- **The API also keeps the last hours in memory for the time machine (built 2026-10-12).** On start
  it streams the last `HISTORY_HOURS` (default 3) UTC hours of `positions` once and folds them in Go
  into one sample per vehicle per minute: the newest real fix in that minute, by the snapshot's own
  tie rule, never interpolated. Each hour is encoded once as a `history-hour.v1` file (JSON Schema in
  `packages/contracts`), gzipped, and served with an ETag at `GET /v1/history/{hour}`
  (`GET /v1/history` lists the hours). After that, each 10 s tick reads the window's `feed_status`
  rows (one indexed query, ~700 rows) and re-reads only the minutes whose row is new or changed, by
  their earliest raw minute, so a backfilled minute rebuilds its own hour and no other. Past hours
  older than the backfill window are `complete: true` and served `immutable`. A request still never
  queries. Measured in `docs/tasks/TASK-time-machine.md` §6.

## 6. Hosting and cost sketch

This copies `renewable-pulse/docs/tasks/TASK-aws-infra.md`. Prices below are that document's
2026-09-27 figures, **not re-checked for this project** `[VERIFY: AWS pricing pages and the
current Spot price in us-east-1 before provisioning]`.

- **Region `us-east-1`.** One `t4g.small` Spot instance (2 vCPU, 2 GiB, arm64), persistent,
  stops on interruption.
- **Docker Compose** runs the adapters, Redpanda, the consumer, Postgres (TimescaleDB +
  PostGIS), the API and Caddy.
- **Data** lives on a separate gp3 EBS volume that survives instance replacement.
- **Not used:** NAT Gateway, ALB, MSK, RDS, EKS.
- **An AWS Budgets alarm on day one,** kept outside the IaC so it survives a destroy.
- **Raw log on S3** with a lifecycle policy to Standard-IA after 30 days. Its retrieval fee
  is $0.01/GB, so re-reading a whole ITS day for a replay export costs about $0.005
  (§8; prices verified 2026-10-12).

| Item | renewable-pulse figure | Monthly |
|---|---|---|
| `t4g.small` Spot | ~USD 0.007/h | ~USD 5 |
| Elastic IP | USD 0.005/h | ~USD 3.65 |
| EBS gp3 root + data | USD 0.08/GB-mo | ~USD 2.5 |
| Snapshots, CloudWatch, ECR, S3 | | ~USD 1–2 |
| **Total** | | **~USD 12** |

**Memory is the risk.** renewable-pulse measured Redpanda at about 730 MiB idle and
TimescaleDB at about 240 MiB on the same 2 GiB box. This repo's scaffold measured, with
`scripts/idle-memory.sh` (`docker stats --no-stream` memory, the stack idle ≥ 60 s), both
containers with the limits they'd have on the box (`--memory 512M` and `mem_limit: 700m` for
Redpanda, `mem_limit: 450m` for TimescaleDB with PostGIS):

| Container | Idle, median of 3 | Limit |
|---|---|---|
| Redpanda v26.2.4 | **149.2 MiB** (210.3 MiB on the first run after creating the volumes) | 700 MiB |
| TimescaleDB 2.30.2 + PostGIS 3.6 (pg18) | **29.6 MiB** (35.8 MiB on the first run) | 450 MiB |

Measured on the Mac (Docker Desktop, arm64), 2026-10-12, median of 3. `docker stats` on Docker
Desktop isn't the same measure as renewable-pulse's figures on the box, and these are idle with
no data. So the numbers don't show that the box has headroom: PostGIS under load, a larger
write rate and deck.gl tiles may still not fit `[VERIFY: re-measure on the t4g.small, and again
under load once phase 1 runs; the fallback is t4g.medium]`. The consumer's sustained live run (30 min: lag, RSS,
TimescaleDB/Redpanda memory) was moved to the AWS infra task for this reason
(`docs/tasks/TASK-positions-consumer.md` E8).

**Raw volume (measured 2026-10-12, a mid-morning public holiday; re-measure in phase 1):**

- ITS API, every supplier: ≈ **529 MB/day** gzip.
- GTFS-RT: ≈ **128 MB/day** (bus + BRT).
- Legacy SPPO + BRT: ≈ 309 MB/day.
- About 10.9 k positions/min (≈ 180 events/s, ≈ 15.7 M rows/day before de-duplication) if
  the ITS API is ingested.
- S3 storage for the whole run is about USD 3. Per-poll objects would cost more in requests
  than in storage, so the raw log should batch hourly (research "Raw volume").

## 7. The ending: replay first

The live service is switched off after a while, and the case study must still work with the
backend off:

1. Raw positions are on S3, compressed, and kept forever: S3 is the working copy, and the
   durable copy outlives the account (§7.1, invariant 2).
2. A few chosen days are exported as static files on S3 + CloudFront. They're generated from
   the raw log, so the export is itself a projection.
3. The site says plainly that it's showing a replay of a named date, never "live".
4. The live backend and the replay share one view contract, so the web app doesn't care which
   one it's reading. **Resolved 2026-10-12:** the contract is `history-hour.v1`
   (`packages/contracts/history-hour.v1.schema.json`): one file per UTC hour, one sample per vehicle
   per minute, a per-minute coverage record. The API serves it from memory; the replay export will
   write the same files to S3, and the time machine in the web app reads either.

### 7.1 Account lifetime (Free plan)

The AWS account is on the Free plan (user, 2026-10-12). It **closes after 6 months (around
2027-03-27) or when its credits run out, whichever comes first.** Its content is deleted 90
days after that. "Kept forever" therefore means that the raw log survives the account, not
that S3 does:

- **The durable copy of the raw log is the user's machine.** S3 is the working copy.
- **Before the account closes:** `aws s3 sync` the raw prefix to the Mac, verify the byte
  counts and SHA-256 checksums against a manifest, and keep a second local copy (an external
  drive). Then recreate the stack in a new Free-plan account and re-upload whatever the
  replay needs. `[VERIFY: AWS's current Free-plan terms on opening a new account]`
- **The credits are shared by three projects,** so they may run out before month 6. Rough
  monthly figures: this project ~USD 12, renewable-pulse ~USD 12, Apuração ~USD 40 in
  total. The account's Budgets alarm has to watch the total, not one project's share.
  `[VERIFY: remaining credit in the Billing console]`
- **The replay must survive a move between accounts.** A CloudFront `*.cloudfront.net` URL
  changes with the account, so a portfolio link pointing at it breaks. Replay files are read
  from one configurable base URL, and the public link goes through something that outlives
  the account: the Next.js site's own domain, or the Vercel deploy. That's decided in the
  replay task.
- **Everything else is rebuildable** (invariant 2): database rows and aggregates are
  recomputed from the raw log in the new account. They're never migrated by hand.
- **Size of the move (estimated 2026-10-12):** after a ~4-month (122-day) run the raw log is
  about **65 GB** gzip-compressed with the ITS API alone, or about **80 GB** with GTFS-RT
  kept as well. Downloading it fits AWS's 100 GB/month free egress if done in one month.
  Re-uploading for the replay needs only the chosen days, about 0.5 GB each.

## 8. Retention (decided 2026-10-12)

Provisional numbers, to be re-checked once the source inventory measures the real raw volume.

| Data | Where | Kept | Notes |
|---|---|---|---|
| Raw provider responses | S3, compressed; durable copy on the user's machine (§7.1) | **forever** | S3 Standard for 30 days, then Standard-IA. No Glacier: replay exports read it back. **Confirmed 2026-10-12** (AWS Price List, us-east-1): Standard $0.023/GB-mo, IA $0.0125/GB-mo plus $0.01/GB retrieval, a 30-day minimum and a **128 KB minimum object size**. So objects must be batched (one per source per hour) for IA to pay off. The total is ≈ 65–80 GB after 4 months |
| Full-detail positions | TimescaleDB hypertable, daily chunks | **3 days** | dropped by a retention policy. Replays are built from S3, never from this table. With the ITS API that's ≈ 47 M rows (≈ 15.7 M/day before de-duplication, **13.4 M/day after it**). **Measured 2026-10-12: 289 B/row** (table plus primary-key index, uncompressed, 1,099,188 rows from the 118-minute run). Retention drops whole chunks, so up to 4 days are on disk: 4 × 13.4 M rows ≈ **14.4 GiB**, above 70% of a 20 GiB volume. **Decided (user, 2026-10-12): keep the schema and size the data volume at 30 GiB** (the AWS infra task). A slimmer schema (smallint codes, no `raw_key`) measured 203 B/row ≈ 10.1 GiB, if it's ever needed. The index was bulk-loaded in key order, so live arrival order may add some `[VERIFY: re-measure on the box after a few live days]` |
| Per line, 5-minute buckets | TimescaleDB continuous aggregate | **forever** | active vehicles, median and p90 delay, observed headway (the exact list is fixed in the phase 1 task) |
| Per line, hourly and daily rollups | continuous aggregates | **forever** | |
| Disk guard | CloudWatch | — | alarm at 70% of the data volume (renewable-pulse `abb09de`) |

**What retention means for replay = live.** `positions` equals a rebuild from the raw log only for
rows inside the 3-day window: the retention job drops whole chunks, including ones a rebuild has just
written. Rebuilding minutes older than 3 days into the live database is therefore pointless for
`positions`; it matters for the aggregates, which are kept forever. The tests pause the job after its
first run (`testharness.SettleRetention`, `docs/tasks/TASK-replay-flake.md`) so the comparison doesn't
depend on when that run lands. The odd old fix (`803043`, `event_time` 2026-09-16 in a 2026-10-12 poll)
is a stale device report; whether to ingest such fixes is left to the delays-by-line task.

## 9. Replay days (decided 2026-10-12)

The raw log records everything while the service is live, so these are the days the live run
has to cover. They don't need to be captured specially.

1. **An ordinary weekday:** a Tuesday to Thursday with no holiday and no major event.
2. **A Sunday:** the weekend network, a different shape of day.
3. **An event day:** Réveillon in Copacabana (31 Dec 2026 → 1 Jan 2027) if phase 1 is live by
   then. Otherwise, the first heavy-rain or disruption day recorded.

The live run is switched off only after all three are in the raw log and their exports are
verified.

## 10. Decisions and open questions

| # | Question | Answer | Status |
|---|---|---|---|
| 1 | AWS account | the shared Free-plan account `860897618882` (also renewable-pulse and Apuração). Before it closes, the raw log is downloaded to the Mac and the project moves to a new Free-plan account (§7.1) | **decided** (user, 2026-10-12) |
| 2 | Backend language | Go for the adapters, consumer and API | **decided** (user, 2026-10-12) |
| 3 | Frontend framework | Next.js (the global `~/.claude/CLAUDE.md` rules apply) | **decided** (user) |
| 4 | Stream processing | a plain Redpanda consumer group with per-vehicle state in the consumer, plus TimescaleDB continuous aggregates for the windows. No Flink or Kafka Streams (JVM, doesn't fit in 2 GiB) | **decided** (user, 2026-10-12) |
| 5 | Retention windows | §8 | **decided** |
| 6 | Replay days | §9 | **decided** |
| 7 | Event identity and schema format | identity = `(operator, feed, vehicle, event_time)` (§10 #9), enforced as the primary key. **A repeat keeps the copy from the earliest raw minute** (`ON CONFLICT … DO UPDATE … WHERE excluded.raw_minute < positions.raw_minute`), not `DO NOTHING`: the provider re-serves a fix in a later minute for 4.6% of records, and the adapter publishes the newest minute first after a restart, so `DO NOTHING` would keep whichever copy arrived first and a live table would differ from a rebuilt one (11 of the 52,854 repeats in the 118-minute run even differ in position). Format = JSON on a versioned topic (`…v1`) with a JSON Schema file as the contract | **decided** (user, 2026-10-12). Vehicle IDs **are stable** across polls and across SMTR's paths, and a parked vehicle repeats its `event_time` in snapshot feeds (verified 2026-10-12). **But see #9:** two suppliers collide on the same key |
| 8 | Tile source | Protomaps: a Rio-region `.pmtiles` extract on S3 + CloudFront | **decided** (user, 2026-10-12). Fonts and sprites have to be hosted too. Verified 2026-10-12 (`docs/tasks/TASK-live-map.md` §7): `make tiles` extracts the Rio bbox from the newest Protomaps daily build (≈ 45 MB, `pmtiles` CLI 1.31.2) and Next serves it with `Range` (206). Glyphs and sprites come from Protomaps' hosted `basemaps-assets` for now; self-hosting them is left to the AWS task |
| 9 | Identity across suppliers | `conecta` and `zirix` both report the same SPPO bus (2,072 vehicles) from two devices with different positions. **Answer: the supplier joins the identity** as an opaque `feed` tag (`its:conecta`, …): identity = `(operator, feed, vehicle, event_time)`. Both observations are kept; picking one fix per vehicle is a projection's job. Nothing downstream branches on `feed`'s value | **decided** (user, 2026-10-12); implemented by `docs/tasks/TASK-its-adapter.md` |
| 10 | Which live path for phase 1 | (a) ITS API: every supplier and the whole fleet, per-minute immutable windows, 24 h backfill, ≈ 529 MB/day; (b) GTFS-RT: standard, ≈ 128 MB/day, but only trip-matched vehicles (≈ 46% of buses); (c) legacy JSON: undocumented, a false `Z` on SPPO times, conecta only. Recommendation: (a) as the source of truth, with (b) optionally recorded as a cross-check. All three are beta | **decided** (user, 2026-10-12): **(a) the ITS API only.** GTFS-RT isn't ingested in phase 1; it may be added later as a cross-check. Implemented by `docs/tasks/TASK-its-adapter.md` |
| 11 | Phase 2 without official GTFS | no official GTFS exists for rail, metro, VLT or ferries (research §4–§7). Options: show them as `none` in the coverage panel only, or hand-build GTFS from the operators' published HTML timetables, labelled as derived by us | **open**: the user decides (raised 2026-10-12) |
| 12 | Static GTFS freshness | the public SMTR feed hasn't changed since 2026-06-10, has `feed_end_date` 2026-12-31, and live `trip_id`s join only ≈44%. Shapes and `route_id`s still join ≈98–99%. Options: ask SMTR (`infra.smtr@prefeitura.rio`) for the current feed, or rely on `route_id`/`shape_id` only | **closed: not needed** (user, 2026-10-12: a learning project; the stale feed is fine for drawing routes) |
| 13 | Product direction | a learning project: a beautiful dashboard people play with. The map is the hero, with a letreiro (LED sign) identity, line and bus exploration, and a time machine over recorded hours. No rider tool, no ETAs, no headway accountability (`docs/research/ux-direction.md`; tasks `TASK-dashboard.md`, `TASK-time-machine.md`) | **decided** (user, 2026-10-12); the dashboard (D1) is built, the time machine (D2) is next |
