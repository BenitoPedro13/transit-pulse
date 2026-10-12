# Transit Pulse

A live public-transport map for **Greater Rio de Janeiro**. It starts with the city's buses
and BRT, then adds the state-run trains, metro, tram, ferries and intercity buses. Every
operator is shown honestly as **live**, **scheduled only** or **no public data**.

It's also a portfolio case study in distributed, data-heavy real-time systems:

- one adapter per provider, all emitting one canonical event;
- an immutable raw log on S3;
- rebuildable projections;
- a static replay that keeps working after the live backend is switched off.

**Status: scaffolded, no adapter yet.** The repo has a Go module, a Next.js app (`apps/web`), local
Redpanda + TimescaleDB/PostGIS and a test harness that runs against real infrastructure. The
source inventory is verified (2026-10-12): Rio's city buses and BRT are live; rail, metro,
tram, ferries, intercity and Niterói buses have no public machine-readable data. The event
schema and the first adapter (ITS API) are planned in
[`docs/tasks/TASK-its-adapter.md`](docs/tasks/TASK-its-adapter.md).

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
pnpm --dir apps/web install   # first time only, if apps/web/node_modules is missing
```

| Target | What it does |
|---|---|
| `make up` | `docker compose up -d --wait --wait-timeout 120` |
| `make down` | `docker compose down`. Keeps the volumes: no target deletes them |
| `make test` | `go test -count=1 ./...` (real Redpanda and TimescaleDB via testcontainers) |
| `make lint` | gofmt check, `go vet`, `golangci-lint run`, `pnpm --dir apps/web lint` |
| `make fmt` | `gofmt -w` on the Go packages |
| `make web-dev` / `make web-build` | the Next.js dev server / production build |

Local ports (all bound to `127.0.0.1`): Kafka `29092`, Redpanda Admin API `29644`, Postgres
`5435`. They differ from renewable-pulse's so both stacks can run at once. `scripts/idle-memory.sh`
prints the median idle memory of the two containers.

## Docs

- [`CLAUDE.md`](CLAUDE.md): workflow, decisions, invariants, tests
- [`docs/architecture.md`](docs/architecture.md): design skeleton and open questions
- [`docs/research/README.md`](docs/research/README.md): the verified source inventory and samples
- [`docs/tasks/`](docs/tasks/): task documents

Layout: deployables in `apps/` (`apps/web`, and each Go binary as `apps/<name>`), shared code
in `packages/` (`testharness`, `samples`, `contracts`). One Go module at the root.
