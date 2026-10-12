# Architecture — Transit Pulse

> **Status (2026-10-12): skeleton.** This document records decisions made in the planning
> conversation. Nothing in it has been checked against a real source yet. Every provider fact
> is `[VERIFY]` until `docs/research/` confirms it with a captured sample. Sections get filled
> in as task documents resolve them.

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
- "stale beyond N minutes" `[VERIFY: pick N per source from its update interval]`.

## 3. The canonical event (draft)

Every adapter emits this shape. Nothing downstream of the adapter knows or branches on which
provider an event came from. **This is a draft, and it gets finalised here once the source
inventory has real samples.**

| Field | Meaning | Notes |
|---|---|---|
| `city` | Metro area, e.g. `rio` | |
| `mode` | `bus` / `brt` / `rail` / `metro` / `tram` / `ferry` | enum to be finalised |
| `operator` | Operator identifier | our own stable slug, not the provider's string |
| `authority` | `city` / `state` | who regulates it |
| `source_kind` | `live` / `scheduled` | invariant 6 |
| `vehicle` | Vehicle identifier as the provider gives it | `[VERIFY per source: stable across polls?]` |
| `line` | Line/route identifier | `[VERIFY per source: joins to GTFS route_id?]` |
| `lat`, `lon` | WGS84 position | `[VERIFY per source: format and precision]` |
| `event_time` | When the provider says the position was taken | `[VERIFY per source: time zone / epoch]` |
| `received_time` | When our adapter fetched it | ours, UTC |

Still open: the event identity used for idempotency (invariant 3), speed and heading if
sources carry them, and the schema format (JSON Schema / Protobuf / Avro).

## 4. Sources and phases

The full per-operator inventory is `docs/research/README.md`. Expected tiers below are from
memory and **unverified**.

| Phase | Scope | Expected tier | Deliverable |
|---|---|---|---|
| 1 | Rio city buses (SPPO), BRT | live `[VERIFY]` | the pipeline, the map, delays by line |
| 2 | SuperVia, MetrôRio, VLT Carioca, Barcas | scheduled `[VERIFY]` | a scheduled layer, a per-operator coverage panel |
| 3 | Historical Rio data | — | a backfill from data.rio on BigQuery `[VERIFY: does it exist, which tables, what cost]`, plus replay |
| 4 | Niterói buses, DETRO-RJ intercity buses, the rest of the metro area; then São Paulo; then GTFS-RT cities | `[VERIFY]` | more adapters, same canonical event |

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
- **Raw log on S3** with a lifecycle policy to a cheaper storage class
  `[VERIFY: which class, and its retrieval cost for replay exports]`.

| Item | renewable-pulse figure | Monthly |
|---|---|---|
| `t4g.small` Spot | ~USD 0.007/h | ~USD 5 |
| Elastic IP | USD 0.005/h | ~USD 3.65 |
| EBS gp3 root + data | USD 0.08/GB-mo | ~USD 2.5 |
| Snapshots, CloudWatch, ECR, S3 | | ~USD 1–2 |
| **Total** | | **~USD 12** |

**Memory is the risk.** renewable-pulse measured Redpanda at about 730 MiB idle and
TimescaleDB at about 240 MiB on the same 2 GiB box. PostGIS, a larger write rate and deck.gl
tiles may not fit `[VERIFY: measure locally once phase 1 runs; the fallback is t4g.medium]`.

**Raw volume is unknown** `[VERIFY: bytes per poll × polls per day per source, from the
captured samples]`. Retention windows (§8) depend on it.

## 7. The ending: replay first

The live service is switched off after a while, and the case study must still work with the
backend off:

1. Raw positions are on S3, compressed, kept forever (invariant 2).
2. A few chosen days are exported as static files on S3 + CloudFront. They're generated from
   the raw log, so the export is itself a projection.
3. The site says plainly that it's showing a replay of a named date, never "live".
4. The live backend and the replay share one view contract, so the web app doesn't care which
   one it's reading `[VERIFY: design in the replay task]`.

## 8. Open questions

| # | Question | Notes |
|---|---|---|
| 1 | Which AWS account, and how much credit does it have? | user mentioned over USD 130; reuse renewable-pulse's account `860897618882` or a new one? Free-plan limits apply if it's a Free-plan account |
| 2 | Backend language | adapters, consumer, API. Apuração is TypeScript, renewable-pulse ingest is Go |
| 3 | Frontend framework | the global `~/.claude/CLAUDE.md` rules apply if Next.js/React |
| 4 | Stream processing | a plain consumer group vs. a stream processor, for delays by line and windowed aggregates |
| 5 | Retention windows | full-detail positions in the database (days), aggregate granularity, and S3 storage class transitions |
| 6 | Which days to record for the replay | e.g. a normal weekday, a weekend, a disruption or event day; must be chosen before the live run ends |
| 7 | Canonical event identity and schema format | §3 |
| 8 | Tile source | Protomaps `.pmtiles` on S3 vs. OpenFreeMap |
