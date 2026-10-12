# Transit Pulse

A live public-transport map for **Greater Rio de Janeiro**. It starts with the city's buses
and BRT, then adds the state-run trains, metro, tram, ferries and intercity buses. Every
operator is shown honestly as **live**, **scheduled only** or **no public data**.

It's also a portfolio case study in distributed, data-heavy real-time systems:

- one adapter per provider, all emitting one canonical event;
- an immutable raw log on S3;
- rebuildable projections;
- a static replay that keeps working after the live backend is switched off.

**Status: the ITS adapter, the consumer, the rebuild, the API and the dashboard are built.** The repo has a Go module, a Next.js app
(`apps/web`), local Redpanda + TimescaleDB/PostGIS and a test harness that runs against real
infrastructure. The source inventory is verified (2026-10-12): Rio's city buses and BRT are
live; rail, metro, tram, ferries, intercity and Niterói buses have no public machine-readable
data. `apps/adapter-its` polls the ITS API one UTC minute at a time, stores every response
byte for byte in a local raw log, and publishes `positions.v1` and `feed-status.v1` to Redpanda
(contracts in `packages/contracts`; plan and measurements in
[`docs/tasks/TASK-its-adapter.md`](docs/tasks/TASK-its-adapter.md)). `apps/consumer` writes
both topics to TimescaleDB (`positions`, a hypertable kept for 3 days, and `feed_status`, kept
forever), and `apps/rebuild` rebuilds the same tables from the raw log (plan and measurements in
[`docs/tasks/TASK-positions-consumer.md`](docs/tasks/TASK-positions-consumer.md)). `apps/api` serves
the latest vehicle positions from an in-memory snapshot (it queries the database once per new
minute, never per request), and `apps/web` draws them on a full-bleed Rio map with a letreiro (the amber LED sign of a Rio
bus) that says plainly whether the data is live, stale or failed, a panel to explore lines and
buses (pick a line, click a bus, follow it, colour by speed; the view lives in the URL), and a
table at `/tabela` as the non-map alternative (plans and measurements in
[`docs/tasks/TASK-live-map.md`](docs/tasks/TASK-live-map.md) and
[`docs/tasks/TASK-dashboard.md`](docs/tasks/TASK-dashboard.md)). A time machine (a timeline along the bottom: scrub the last hours, play at
1×/10×/60× with fading trails, and a way back to live) is built too; its on-screen checks are still to run
([`docs/tasks/TASK-time-machine.md`](docs/tasks/TASK-time-machine.md)). The raw log isn't uploaded to S3 yet.

## Setup

Prerequisites (versions checked 2026-10-12):

- **Docker** with Compose (Docker Desktop on the Mac). The tests start containers, so Docker
  must be running.
- **Go 1.26.** `go.mod` pins `toolchain go1.26.9`, so with `GOTOOLCHAIN=auto` (the default) the
  `go` command downloads that patch for this module only. Don't upgrade your global Go for it.
- **golangci-lint v2.14.0.** Install the prebuilt binary with the official script, not
  `brew install`: the Homebrew formula depends on `go` and would upgrade your global Go.

  ```bash
  curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$HOME/.local/bin" v2.14.0
  ```

  `~/.local/bin` has to be on your `PATH`.
- **Node 24** (`apps/web/.nvmrc`) and **pnpm 11** (pinned in `apps/web/package.json`).
- `make` (the only task runner).

```bash
make up          # copies .env.example to .env if needed, starts Redpanda + TimescaleDB, waits until healthy
make test        # Go tests, including the integration smoke test (needs Docker)
make lint        # gofmt, go vet, golangci-lint, ESLint
make web-dev     # Next.js dev server on http://localhost:3000
make adapter-its # polls the ITS API and publishes to Redpanda (needs `make up`; Ctrl-C stops it)
make consumer    # topics -> TimescaleDB (needs `make up`; Ctrl-C stops it)
make rebuild FROM=2026-10-12T14:27:00Z TO=2026-10-12T16:24:00Z   # raw log -> TimescaleDB
make api         # read-only API on 127.0.0.1:8080 (needs `make up` and a filled database)
make tiles       # Rio basemap extract -> apps/web/public/tiles/rio.pmtiles (needs `brew install pmtiles`)
pnpm --dir apps/web install   # first time only, if apps/web/node_modules is missing
```

**Run it locally (the screen):**

1. `make up`, then fill the database: `make adapter-its` and `make consumer` for live data, or
   `make rebuild FROM=… TO=…` from a raw log (the screen then says "Não está ao vivo" and the minute
   it shows).
2. `make tiles` once (about 45 MB, git-ignored; the Protomaps daily build is kept for a week).
3. `make api` in one terminal and `make web-dev` in another, then open http://localhost:3000. The
   web app reaches the API through a rewrite (`/api/*` → `127.0.0.1:8080`), so there is no CORS.

| Target | What it does |
|---|---|
| `make up` | `docker compose up -d --wait --wait-timeout 120` |
| `make down` | `docker compose down`. Keeps the volumes: no target deletes them |
| `make test` | `go test -count=1 ./...` (real Redpanda and TimescaleDB via testcontainers), then `pnpm --dir apps/web test` (Vitest, pure functions) |
| `make lint` | gofmt check, `go vet`, `golangci-lint run`, `pnpm --dir apps/web lint` |
| `make fmt` | `gofmt -w` on the Go packages |
| `make dev` | local infra, the API and the Next.js dev server in one terminal (Ctrl-C stops both) |
| `make web-dev` / `make web-build` | the Next.js dev server / production build |
| `make adapter-its` | runs `apps/adapter-its` with `.env` loaded. Writes the raw log under `RAW_DIR` (`./var/raw`, git-ignored: **never delete it**, it's the source of truth), publishes to Redpanda, and logs one JSON line per published minute. Set `ITS_CONTACT` in your own `.env` |
| `make consumer` | runs `apps/consumer` with `.env` loaded: consumer group `transit-pulse-consumer` on `positions.v1` and `feed-status.v1`, one transaction per batch, offsets committed after it. Needs `DATABASE_URL`. Logs one JSON line per batch |
| `make api` | runs `apps/api` with `.env` loaded. Read-only; listens on `API_ADDR` (default `127.0.0.1:8080`). `GET /v1/vehicles/latest` and `GET /healthz`, both from memory. Starts and answers `503` if the database is down, then recovers by itself |
| `make tiles` | `pmtiles extract` of the newest Protomaps daily build for the Rio bbox into `apps/web/public/tiles/rio.pmtiles` (git-ignored). `make tiles TILES_BUILD=<key>` picks a build |
| `make rebuild FROM=… TO=…` | runs `apps/rebuild`: rebuilds `positions` and `feed_status` from the raw log for those minutes (both inclusive), oldest first. Writes `DATABASE_URL`, or `DB=postgres://…` for another database. Running it again changes nothing |

**If the consumer falls more than 24 h behind**, the topic has already dropped the oldest
records (`positions.v1` keeps 24 h). The minutes are still in the raw log: run `make rebuild`
for the missing range, then start `make consumer` again. Rebuilding into the live database is
safe.

Local ports (all bound to `127.0.0.1`): Kafka `29092`, Redpanda Admin API `29644`, Postgres
`5435`. They differ from renewable-pulse's so both stacks can run at once. `scripts/idle-memory.sh`
prints the median idle memory of the two containers.

## Docs

- [`CLAUDE.md`](CLAUDE.md): workflow, decisions, invariants, tests
- [`docs/architecture.md`](docs/architecture.md): design skeleton and open questions
- [`docs/research/README.md`](docs/research/README.md): the verified source inventory and samples
- [`docs/tasks/`](docs/tasks/): task documents

Layout: deployables in `apps/` (`apps/web`, and each Go binary as `apps/<name>`), shared code
in `packages/` (`testharness`, `samples`, `contracts`, `event`, `raw`, `its`, `store`). One Go
module at the root. Go binaries today: `adapter-its`, `consumer`, `rebuild`, `api`.
