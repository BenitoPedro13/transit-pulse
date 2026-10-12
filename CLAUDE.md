# Workflow Guidelines — Transit Pulse

> Ported from the `apuracao-2026` / `renewable-pulse` / `flora` workflow (plan before you
> touch anything, lean on official CLIs and generators, treat documentation as part of the
> deliverable), retargeted to this project. Section 0 is project-specific; sections 1–4 are
> the portable rules.
>
> **The philosophy in one line:** plan before you write, lean on existing tooling while you
> work, and treat documentation as part of the deliverable when you finish.

---

## 0. Project context — Transit Pulse

A live public-transport map for **Greater Rio de Janeiro**. It's a portfolio case study in
**distributed, data-heavy real-time systems**:

- every operator gets its own provider adapter that emits one canonical event;
- the raw log of positions is immutable;
- projections are idempotent and can be rebuilt;
- thousands of moving vehicles are drawn on a map that always has a table alternative.

It sits alongside `renewable-pulse` (polled energy feeds → broker → TimescaleDB),
`apuracao-2026` (polled TSE feed → immutable S3 log → replay) and `flora`.

**Status (2026-10-12): the ITS adapter, the positions consumer, the rebuild, the read-only API
and the dashboard (full-bleed map, letreiro, Agora/Linhas/Ônibus panel, table at `/tabela`) are built.** The repo has a Go module, a Next.js app,
local Redpanda + TimescaleDB/PostGIS and a real-infrastructure test harness (`docs/tasks/TASK-scaffold.md`, done).
`docs/tasks/TASK-source-inventory.md` is done: every Rio source was verified live and real
samples are in `docs/research/samples/`. `docs/tasks/TASK-its-adapter.md` is done: the canonical
event v1 and `feed-status.v1` (JSON Schema in `packages/contracts`), the raw log
(`packages/raw`) and `apps/adapter-its`, which publishes `positions.v1` and `feed-status.v1`.
Its measurements are in that document's §7. `docs/tasks/TASK-positions-consumer.md` is done:
`packages/its` (the mapping, moved out of the adapter), `packages/store` (the `positions`
hypertable, `feed_status`, and the two idempotent, order-independent writes), `apps/consumer`
(topics → database) and `apps/rebuild` (raw log → database), with measurements in its §7. `docs/tasks/TASK-live-map.md` is
done: `apps/api` (a snapshot refreshed from `feed_status` every 10 s and served from memory, so a
request never queries), the Rio basemap (`make tiles`), vehicles on MapLibre + deck.gl, a TanStack
Table list, and a status strip that tells live, stale and failed apart; measurements in its §7. `docs/tasks/TASK-dashboard.md` (D1) is
done: the screen became a full-bleed dashboard with the letreiro identity, line and bus selection,
speed colours and the view in the URL; its results (contrast table, axe, fps) are in its §6. The time
machine (D2, `docs/tasks/TASK-time-machine.md`) is next. Of the four questions the inventory raised
(`docs/architecture.md` §10 #9–#12), #9 and #10 were answered on 2026-10-12 (supplier joins
the identity; ITS API only). #11 and #12 are still open. Read in this order:

1. `docs/research/README.md`: the verified source inventory, one row per operator, plus
   measured volumes. Its "Findings that contradict or change the plan" section comes first.
2. `docs/architecture.md`: decisions, the canonical event v1, phases, hosting and cost,
   the replay-first ending, and open questions.

### Decisions already made (user, 2026-10-12). Don't reopen them

- **Rio first.**
  - Start with the city's buses (SPPO) and BRT. Both have live GPS feeds: three public,
    keyless SMTR paths, all labelled beta (verified 2026-10-12, research §1).
  - Then add the whole metro area, including **state-run operators**: SuperVia (trains),
    MetrôRio, VLT Carioca, the Barcas ferries, intercity buses (DETRO-RJ) and Niterói's city
    buses.
  - São Paulo and standard GTFS-Realtime cities come later.
- **Provider adapters → one canonical event.**
  - Every source has its own adapter that emits the same event shape.
  - Nothing after the adapter knows which provider an event came from.
  - The draft shape is in `docs/architecture.md` §3; it isn't final.
- **Honest coverage tiers.**
  - **Live**, **scheduled only** and **no public data** are three distinct states, in the
    data contracts and in the UI.
  - Scheduled data is never drawn as if it were live.
- **Phases.**
  1. Rio buses and BRT live: the pipeline, the map, and delays by line.
  2. Static GTFS for rail and ferries: a scheduled layer and a per-operator coverage panel.
     ⚠ No official GTFS exists for rail, metro, VLT or ferries (verified 2026-10-12). How
     phase 2 proceeds is open: `docs/architecture.md` §10 #11.
  3. Historical backfill from Rio's public data lake (`rj-smtr` on BigQuery: it exists;
     public access `[VERIFY: needs a GCP account]`), plus replay.
  4. Niterói and the rest of the metro area, then São Paulo, then GTFS-RT cities.
- **Hosting on AWS, as cheap as possible.**
  - It copies renewable-pulse's pattern (`renewable-pulse/docs/tasks/TASK-aws-infra.md`): one
    `t4g.small` Spot instance running Docker Compose.
  - Region `us-east-1`. A Budgets alarm on day one.
  - No NAT Gateway, no ALB, no managed Kafka, no RDS.
- **A short live run, then replay.**
  - The live service is switched off after a while, so the case study must still work with
    the backend off.
  - Raw positions go to S3 as compressed files and are kept forever. The AWS account is on
    the Free plan, so before it closes the raw log is downloaded to the user's machine and the
    stack moves to a new account (`docs/architecture.md` §7.1).
  - A few chosen days are exported as a static replay on S3 + CloudFront, and the site says
    plainly that it's a replay.
- **Storage discipline.**
  - Full-detail positions stay in the database for only a few days; aggregates are kept
    forever.
  - Why: renewable-pulse filled its disk during a backfill (its commit `abb09de`).
- **The map.**
  - MapLibre GL with OpenStreetMap tiles (Protomaps `.pmtiles` on S3, or OpenFreeMap).
  - deck.gl for thousands of moving vehicles.
  - Route lines from GTFS `shapes.txt`.
  - Map-matching (OSRM/Valhalla) is a stretch goal.

### Stack

The choices were decided on 2026-10-12 (`docs/architecture.md` §10). The scaffold
(2026-10-12) installed the rows marked **scaffolded**; every other row is **planned**. Version
numbers are a snapshot, not a pin: check each tool's current docs before installing or
upgrading (§2.0).

| Layer | Choice | Status |
|---|---|---|
| Ingestion | One adapter per provider → canonical event. Go 1.26 (`go 1.26.0`, `toolchain go1.26.9`; revisit when 1.28 ships, ≈ Feb 2027), one module `github.com/BenitoPedro13/transit-pulse`, binaries in `apps/<name>` | **Go** (decided); Go module and lint **scaffolded** (golangci-lint v2.14.0); the ITS adapter (`apps/adapter-its`, ITS API only) is **built**; other adapters planned |
| Broker | Redpanda v26.2.4 (Kafka API) on the box, as in renewable-pulse; franz-go v1.22.1 client (+ kadm v1.19.0 for topic creation) | **scaffolded** (local compose, `make up`); used by the ITS adapter and the consumer |
| Stream processing | Plain consumer group + TimescaleDB continuous aggregates | consumer group (`apps/consumer`) **built**; continuous aggregates planned (decided) |
| Store | Postgres 18 + TimescaleDB 2.30.2 + PostGIS 3.6 (`timescale/timescaledb-ha:pg18.6-ts2.30.2`, arm64), same box; pgx v5.11.0; schema migrations with goose v3.29.0 (embedded SQL in `packages/store/migrations`, Postgres session lock) | **built**: `positions` (hypertable, 1-day chunks, 3-day retention, no compression) and `feed_status` |
| Raw log | S3, compressed files (working copy); durable copy on the user's machine before the account closes | planned |
| API | Serves precomputed views and live positions. `apps/api` (**Go**): `GET /v1/vehicles/latest` and `/healthz` from an in-memory, pre-gzipped snapshot; pgx v5, read-only | **built** (`GET /v1/vehicles/latest`); other views planned |
| Web | **Next.js** 16.4.0 (App Router, Cache Components) + TanStack Query v5 (`@tanstack/react-query`), pnpm 11, Node 24; MapLibre GL 6.14.0 (ESM-only, worker URL set under Turbopack) + react-map-gl 8.1.3 + deck.gl 9.4.0 (`@deck.gl/maplibre`, overlaid mode); pmtiles 4.5.0 + `@protomaps/basemaps` 5.7.2 on a Protomaps Rio `.pmtiles` extract (`make tiles`, local; S3 + CloudFront with the AWS task); TanStack Table **v9** (`@tanstack/react-table` 9.2.8); **shadcn/ui** (CLI 4.21, Radix base, `button`, `tabs`, `toggle`, `toggle-group`, themed onto the CSS tokens in `app/globals.css`) + Tailwind v4 + `lucide-react`; **Archivo** (variable, `wdth` axis) for the UI and **Doto** (letreiro text only, ≥ 20 px) through `next/font/google`; Vitest 5.0 for the pure rules (node environment) | **built** (`apps/web/`): dashboard (map, letreiro, panel), `/tabela`, status strip |
| Test harness | testcontainers-go v0.44.0 starts real Redpanda and TimescaleDB (`packages/testharness`); real captures load through `packages/samples` | **scaffolded** |
| Replay | Static export of chosen days on S3 + CloudFront | planned |
| Hosting | AWS `us-east-1`, Free-plan account `860897618882` (shared with renewable-pulse and Apuração; closes around 2027-03-27 or when the credits run out): one `t4g.small` Spot, Docker Compose, Caddy, Budgets alarm | planned (decided) |
| Frontend rules | the user's global `~/.claude/CLAUDE.md`, unchanged | applies |

### How to write in this repo

- **Never invent a provider field, URL, endpoint or behaviour.** That covers SMTR,
  data.rio, SuperVia, MetrôRio, VLT, Barcas, DETRO-RJ, Niterói and every GTFS/GTFS-RT feed.
  - Write `[VERIFY: what to check and where]` inline instead.
  - Resolve it against a real response captured into `docs/research/samples/` before any
    code depends on it.
  - Every availability claim from the planning conversation came from memory and is
    unverified.
- **Be specific to the point of discomfort:** exact URLs, exact poll intervals, exact
  numbers.
  - No acceptance criterion may rest on "works" or "fast enough".
  - State the vehicles per poll, the events per second, the p95 latency and the bytes per day.
- **Sources over memory.** Anything about a provider, a library or a hosting platform is
  checked against the live source, and the source is linked.

### Invariants — never break these without changing the spec first

1. **Real data only.**
   - Every vehicle position traces back to a real provider response.
   - Nothing is synthetic, simulated or interpolated and then shown as observed.
   - A gap (an unrecorded period, a failed fetch) is shown as missing, never smoothed over.
   - Replaying real recorded positions is fine. Fabricating one is not.
2. **Raw positions are immutable.**
   - Every provider response we fetch is stored as received, compressed, in S3, and kept
     forever, including across the move between AWS accounts (`docs/architecture.md` §7.1).
   - Everything else is a projection that can be rebuilt from that raw log: database rows,
     aggregates, delays by line, the replay export. A projection is never the source of
     truth.
3. **Ingestion is idempotent.**
   - Fetching or processing the same provider response twice changes nothing.
   - Events have a deterministic identity (`operator + feed + vehicle + event_time`; a repeat keeps the copy from the earliest raw minute).
4. **Be a polite client to every provider.**
   - Poll no faster than the source updates.
   - Use conditional requests where the provider supports them. Verified 2026-10-12:
     SMTR's static GTFS and GTFS-RT honour `ETag` and `Last-Modified`; the ITS API sends
     no validators. Details per source are in `docs/research/README.md`.
   - Keep a hard request budget per source and back off on 4xx/5xx.
   - Never proxy our readers straight through to a provider.
5. **Readers never touch the ingestion path.**
   - Viewer traffic is served from precomputed or cached views.
   - A traffic spike must not slow ingestion.
   - An ingestion stall must not take the site down; it shows "last updated at …".
6. **Missing is not zero.**
   - **Live**, **scheduled only** and **no public data** are three distinct coverage states.
   - So are "no vehicles reported" and "fetch failed".
   - All of these are distinct in contracts and in the UI. Scheduled data is never drawn as
     if it were live.
7. **WCAG 2.2 AA.**
   - The map is never the only way to read a result; a table or list alternative is
     required.
   - Colour is never the only carrier of meaning (mode, tier, delay).
   - Motion respects `prefers-reduced-motion`.

### Tests

- **Integration tests against real infrastructure** (testcontainers or equivalent), never a
  mocked database or broker.
- **Parsers are tested against the real captured samples** in `docs/research/samples/`,
  never hand-built fixtures.
- **Idempotency is a test:** ingest the same provider response twice and assert nothing
  changed.
- **Replay is a test:** rebuild a projection from the raw S3 log and assert it equals the
  live projection.
- **Performance claims need medians of repeated runs**, never a single measurement.

---

## 1. Plan before executing — write a task document first

**Rule:** Before editing or creating **any** code file, write a task document at
`docs/tasks/TASK-<slug>.md`. No exceptions for "small" changes.

Required sections, in order:

1. **Current scenario**: how it works today, and what's missing or blocked.
2. **Planned changes**: file by file, plus alternatives considered and rejected.
3. **Why**: what it unblocks and what it costs.
4. **Affected files**: a table of `File | Change type | Notes`.

Write the document, summarize it in 2–3 lines, and wait for alignment on anything
significant before writing code. Keep it in sync if the plan changes. The task document is
the contract.

## 2. Use CLIs, generators, and SDKs — don't write everything by hand

### 2.0 Assume your framework knowledge is outdated — check first, every time

1. Go to the tool's own current docs first: the chosen framework, MapLibre, deck.gl,
   Redpanda, TimescaleDB, PostGIS, Terraform/CDK, and GTFS / GTFS-Realtime.
2. Use the official CLI or generator to scaffold, never a hand-written copy of what it
   produces.
3. Take the current stable major as authoritative over anything written here, and update
   this file's stack table to match (§3).
4. One package manager per language, decided at scaffold time, never mixed.

Hand-writing is for what no generator covers: the adapters, the canonical event, the
projections, the replay export and the map layers. Match the style of the surrounding code.

## 3. Update documentation after executing

A task isn't done until every doc it affects is updated:

- **`CLAUDE.md`**: if the stack, architecture or an invariant changes.
- **`docs/architecture.md`**: if a `[VERIFY]` resolves, an open question closes, or scope
  changes.
- **`docs/research/`**: if a new fact about a provider is observed. Update the inventory row
  and add the captured sample.
- **`.env.example`** (once code exists): every environment variable the code reads.
- **`README.md`**: the status line, setup and scripts.

When unsure, grep for the thing you changed across these files.

## 4. Project conventions

A single repo laid out as `apps/` (deployables) + `packages/` (shared code), with one Go
module at the root and one standalone pnpm project (`apps/web/`). `apps/` and `packages/`
are directories, not modules or workspaces (`docs/tasks/TASK-repo-layout.md`). Today it holds:

```
transit-pulse/
├── go.mod / go.sum              # module github.com/BenitoPedro13/transit-pulse (one module)
├── .golangci.yml                # golangci-lint v2 config, default linters
├── Makefile                     # the only task runner
├── compose.yaml                 # local infra: redpanda + timescaledb
├── .env.example                 # every variable the scaffold, the adapter, the consumer and the API read
├── var/                         # git-ignored: local raw log (var/raw) and adapter state
├── apps/
│   ├── adapter-its/             # ITS API adapter: raw log first, then positions.v1 + feed-status.v1
│   ├── consumer/                # positions.v1 + feed-status.v1 -> TimescaleDB, one transaction per batch
│   ├── rebuild/                 # raw log -> TimescaleDB for a minute range (the same writes as the consumer)
│   ├── api/                     # read-only API: feed_status probe every 10 s -> in-memory snapshot (JSON + gzip)
│   └── web/                     # Next.js app: the dashboard at /, the table at /tabela
│       ├── app/                 # layout.tsx (fonts), page.tsx + tabela/page.tsx (Suspense around the client views), globals.css (the tokens), providers.tsx, get-query-client.ts
│       ├── components/          # dashboard (state + layout), letreiro, live-map (+ canvas, basemap, vehicle-style), panel-agora/linhas/onibus, color-legend, status-strip, tabela-view, vehicle-table; ui/ = shadcn output
│       ├── hooks/               # every custom hook lives here, with the pure rules and their Vitest tests (testdata/ = a real, trimmed snapshot)
│       ├── lib/                 # shadcn's utils.ts
│       └── public/tiles/        # git-ignored: rio.pmtiles from `make tiles`
├── packages/
│   ├── testharness/             # testcontainers-go: real Redpanda and TimescaleDB for tests
│   ├── samples/                 # loads docs/research/samples/<name> for parser tests
│   ├── contracts/               # position.v1 and feed-status.v1 JSON Schema + a test validator
│   ├── event/                   # Position and FeedStatus Go types; Key() and Identity()
│   ├── its/                     # raw ITS envelope -> events (Events, MinuteEvents) and feed statuses
│   ├── store/                   # goose migrations, WritePositions / WriteFeedStatus, read helpers for tests
│   └── raw/                     # append-only raw log: hourly gzip files, byte-exact bodies
├── infra/postgres/initdb/       # 001-extensions.sql, shared by compose and the tests
├── scripts/idle-memory.sh       # median-of-3 idle memory per container
└── docs/
    research/                    # source inventory + samples/ (real captured provider responses)
    tasks/                       # task documents (§1)
    architecture.md
```

Each Go binary arrives with its task as `apps/<name>/main.go` (`package main`, e.g.
`apps/adapter-its`); shared Go code goes in `packages/<name>/`. A root pnpm workspace is
added only when a second JS package exists. Make targets: `up`, `down`, `test`
(Go, then the web's Vitest), `lint`, `fmt`, `web-dev`, `web-build`, `adapter-its`, `consumer`, `rebuild`, `api`, `tiles`. No target runs `down -v`: deleting volumes is the
user's call. There is no CI yet (no remote; `docs/tasks/TASK-scaffold.md` §2.7).

- **Samples:** keep small, representative captures in git (a single response, a trimmed
  GTFS zip). Large files stay out (see `.gitignore`); record their source URL and capture
  time instead.

### 4.1 Commit conventions

- **Commit automatically once a task document's work is complete and verified** (build,
  lint and tests passing, where they exist). Don't wait to be asked each time.
- **Never run destructive git commands without asking.** Force-push, `reset --hard`,
  branch deletion and history rewrites need explicit confirmation.
- **No `Co-Authored-By` trailer.** User preference for every repo.

---

## TL;DR

| Phase | Rule | Output |
|---|---|---|
| **Research** | Verify every source live, never from memory | `docs/research/` + real samples |
| **Before** | Write a task document first | `docs/tasks/TASK-<slug>.md`: current scenario, planned changes, why, affected files |
| **During** | Official CLIs and generators; `[VERIFY: …]` for anything unconfirmed | canonical, reproducible output |
| **After** | Update `CLAUDE.md` / `docs/architecture.md` / `docs/research/` / `README.md`, then commit | docs in sync, a commit with no `Co-Authored-By` |

**Never broken:** real data only, raw positions immutable and replayable, idempotent
ingestion, polite to every provider, readers isolated from ingestion, missing ≠ zero (live /
scheduled only / no public data), WCAG 2.2 AA.
