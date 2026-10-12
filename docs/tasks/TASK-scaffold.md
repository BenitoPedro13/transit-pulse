# TASK: Scaffold the repo — Go module, Next.js app, local infrastructure, test harness

**Status (2026-10-12): planned, awaiting review.** Written by the planner; implemented by a
different model. Every command, path and number below is meant to be followed literally. If
something here turns out to be wrong against the live docs, **stop and ask** (§2.9). Don't
improvise around it.

## 1. Current scenario

- The repo holds docs only: `CLAUDE.md`, `README.md`, `docs/architecture.md`,
  `docs/research/` (verified inventory + 9 real samples), and two task documents. There's no
  code, no package manager file, no compose file and no test.
- The decisions it builds on are settled (`docs/architecture.md` §10 #1–#8): Go for adapters,
  consumer and API; Next.js for the web; Redpanda + a plain consumer group; Postgres +
  TimescaleDB + PostGIS on one `t4g.small` (2 GiB, arm64); JSON events with a JSON Schema
  contract; identity `(operator, vehicle, event_time)` (but see #9).
- **Blocked on this task:** every later task (canonical-event schema, ITS adapter + raw log,
  consumer, API, map) needs somewhere to land, a way to start Redpanda and TimescaleDB, and a
  test harness that uses real infrastructure (CLAUDE.md "Tests").
- **Open and not this task's to decide:** architecture §10 **#9** (identity across
  suppliers), **#10** (which live path), **#11** (phase 2 without GTFS), **#12** (stale static
  GTFS). Nothing in this scaffold depends on any of them; §5 names which later task each one
  blocks.
- **Local machine (checked 2026-10-12):** Apple M2, 8 GiB RAM, Docker 29.7.2 (arm64) with
  Compose v5.3.1, `make` present, `just` **not** installed, Node v24.19.0 via nvm, pnpm
  present, Go **1.26.6** (Homebrew, `GOTOOLCHAIN=auto`). Current stable is 1.27.2; the
  project stays on 1.26 (decided 2026-10-12, §2.2).

## 2. Planned changes

### 2.1 Layout: one repo, one Go module

```
transit-pulse/
├── go.mod / go.sum              # module github.com/BenitoPedro13/transit-pulse (one module)
├── .golangci.yml                # golangci-lint v2 config, default linters
├── Makefile                     # the only task runner
├── compose.yaml                 # local infra: redpanda + timescaledb
├── .env.example                 # every variable the scaffold reads
├── infra/
│   └── postgres/initdb/
│       └── 001-extensions.sql   # CREATE EXTENSION timescaledb, postgis (shared by compose + tests)
├── internal/
│   ├── testharness/             # testcontainers-go: starts Redpanda and TimescaleDB for tests
│   │   ├── harness.go
│   │   └── harness_test.go      # the smoke test (§2.5)
│   └── samples/                 # loads docs/research/samples/<name> for parser tests
│       ├── samples.go
│       └── samples_test.go
├── schemas/
│   └── .gitkeep                 # JSON Schema contracts; empty until the canonical-event task
├── scripts/
│   └── idle-memory.sh           # median-of-3 idle memory per container (§2.8)
├── web/                         # create-next-app output + TanStack Query provider
│   ├── app/
│   │   ├── layout.tsx           # generated; edited only to wrap children in <Providers>
│   │   ├── page.tsx             # generated, untouched
│   │   ├── providers.tsx        # 'use client'; QueryClientProvider (official pattern)
│   │   └── get-query-client.ts  # makeQueryClient / getQueryClient (official pattern)
│   ├── hooks/
│   │   └── .gitkeep             # every custom hook lives here (global rules)
│   └── … (everything else exactly as create-next-app generates it)
└── docs/ …                      # unchanged except the doc updates in §2.10
```

**Not created now:** `cmd/`. Each binary arrives with its task as `cmd/<name>/main.go`
(`cmd/adapter-its`, `cmd/consumer`, `cmd/api`, …). An empty `main()` would be placeholder
code that pretends to work.

**One Go module, chosen over several modules:**

- Adapters, consumer and API all import the same canonical-event package (`internal/event`,
  from the schema task). In one module that's a plain import. With several modules it needs a
  `go.work` file or `replace` directives, plus keeping several `go.sum` files in step.
- Separate binaries (and separate Docker images later) come from `cmd/*`, not from separate
  modules. Independent versioning would buy nothing: nothing here is published as a library.
- **Rejected: one module per app (`adapters/`, `consumer/`, `api/`) + `go.work`.** That is
  how renewable-pulse's TypeScript workspaces look, but in Go it adds version skew between
  modules and a workspace file for no gain at this size.
- **Rejected: the Go module in a `backend/` subfolder.** It keeps Go out of `web/`, but the
  `ignore` directive (below) does the same with less nesting.

**Keeping Go out of `web/`:** `go.mod` gets `ignore ./web` so `go test ./...`, `go vet ./...`
and golangci-lint never walk `web/node_modules`. Syntax from
<https://go.dev/ref/mod> (`ignore ./node_modules` is its own example). **Verified locally
2026-10-12 with Go 1.26.6:** a scratch module with `ignore ./web` and a deliberately broken
`web/node_modules/x/x.go` passed `go list ./...` and `go vet ./...`, listing only the
non-ignored package. golangci-lint honouring it is still
`[VERIFY: A12 + "make lint" passing with web/node_modules present]`.

**Module path:** `github.com/BenitoPedro13/transit-pulse`, matching renewable-pulse's
`github.com/BenitoPedro13/renewable-pulse/...`. No remote is created; the path is only a name
until the user adds one.

**JS:** `web/` is a single standalone pnpm project (its own `package.json` and
`pnpm-lock.yaml`). No root `package.json`, no pnpm workspace, no Turborepo: there is exactly
one JS package. Rejected: a renewable-pulse-style monorepo root — it exists there to share
TypeScript contracts between TS apps, which this repo doesn't have (contracts are JSON Schema
files in `schemas/`).

### 2.2 Toolchains (check every version at implementation time, §2.0)

Versions below were observed on 2026-10-12 and are **a snapshot, not a pin**. The implementer
re-checks each against the linked source and uses the current stable.

| Tool | Observed 2026-10-12 | Source to re-check | Notes |
|---|---|---|---|
| Go | **1.26.x, latest patch** (1.26.9 on 2026-10-08). Not 1.27 | <https://go.dev/doc/devel/release> | see "Go version" below |
| golangci-lint | v2.14.0 (2026-09-24), config format v2 | <https://golangci-lint.run/docs/welcome/install/local/> | install the binary (`brew install golangci-lint`, the page recommends binary installs; `go install` is listed as unsupported) |
| Node.js | 24 LTS "Krypton", latest v24.21.0 | <https://nodejs.org/dist/index.json> | local v24.19.0 is the same LTS major, so it's fine |
| pnpm | pnpm 12 is the current line | <https://pnpm.io/installation> | use what `create-next-app --use-pnpm` pins; if local pnpm < 12, `pnpm self-update` |
| Next.js | 16.4.0 (docs page `lastUpdated` 2026-09-29) | <https://nextjs.org/docs/app/api-reference/cli/create-next-app> | via `create-next-app`, never hand-written |
| TanStack Query | v5 (`@tanstack/react-query`) | <https://tanstack.com/query/latest/docs/framework/react/guides/advanced-ssr> | |
| testcontainers-go | v0.44.0 (2026-08-07) | <https://github.com/testcontainers/testcontainers-go/releases> | modules `redpanda` and `postgres` |
| franz-go | `[VERIFY: latest tag at https://github.com/twmb/franz-go/tags — the releases API returned none]` | | |
| pgx | v5.11.0 (2026-09-07) | <https://github.com/jackc/pgx/releases> | |

**Go version (user, 2026-10-12: upgrade only if it brings big improvements for this
project).** 1.27 doesn't (release notes at <https://go.dev/doc/go1.27>, read 2026-10-12):

- the relevant items are `encoding/json` unmarshal "significantly faster" (now on the v2
  implementation), about 1% faster small allocations, and HTTP/1 bodies drained on close;
- at ≈ 180 events/s (research "Database load") JSON decoding isn't a bottleneck, and
  nothing in it targets low-memory containers or arm64 (only experimental NEON SIMD);
- a global `brew upgrade go` would also move renewable-pulse (`go 1.26.6`).

So: **`go 1.26` in `go.mod` plus `toolchain go1.26.9`** (or the latest 1.26.x patch at
implementation time). 1.26.7–1.26.9 carry `net/http` and `crypto/tls` fixes, which matter
for HTTP-polling adapters. With `GOTOOLCHAIN=auto` (the local setting), the `go` command
downloads that patch for this module only, and the Homebrew install stays as it is.
`[VERIFY: toolchain-directive behaviour at https://go.dev/doc/toolchain]`.

**When to revisit:** Go supports a major until two newer majors exist. 1.26 loses security
fixes when 1.28 ships, expected around February 2027, before the account closes (around
2027-03-27). Bump to 1.27 then, as its own small task.

**Go dependencies (each with its reason; nothing else is added):**

| Module | Why |
|---|---|
| `github.com/testcontainers/testcontainers-go` + `/modules/redpanda` + `/modules/postgres` | integration tests against real Redpanda and TimescaleDB, never mocks (CLAUDE.md "Tests") |
| `github.com/twmb/franz-go/pkg/kgo` | Kafka client for the smoke test's produce/consume, and later the adapters and consumer. Pure Go (no cgo, so arm64 builds stay simple) and already proven against Redpanda in renewable-pulse's `apps/ingest` |
| `github.com/jackc/pgx/v5` | Postgres driver for the smoke test's query, and later the consumer and API |

No assertion library (standard `testing` only), no config library, no logger yet.

**Go lint/format:** `gofmt` (format check), `go vet`, and **golangci-lint v2** with its
default linter set and no custom enables. Reason: one binary gives `staticcheck`, `errcheck`
and friends for free, with a 3-line config. `.golangci.yml`:

```yaml
version: "2"
run:
  timeout: 3m
```

`[VERIFY: that a minimal v2 config with only "version" and "run.timeout" is valid, at
https://golangci-lint.run/docs/configuration/file/]`

**Next.js, exact command** (run from the repo root):

```bash
pnpm create next-app@latest web \
  --ts --app --eslint --tailwind --no-src-dir \
  --no-react-compiler --cache-components \
  --import-alias "@/*" --use-pnpm --disable-git \
  --agents-md --no-agent-feedback
```

| Flag | Choice | Why |
|---|---|---|
| `--ts` | TypeScript | default; the API contract types come later from `schemas/` |
| `--app` | App Router | Server Components by default (global rules) |
| `--eslint` | ESLint, not Biome | includes `@next/eslint-plugin-next`; same as renewable-pulse |
| `--tailwind` | yes | the recommended default, and shadcn/ui + Radix (the global rules' preferred accessible components) are styled with Tailwind; the map and table tasks will need it |
| `--no-src-dir` | no `src/` | the CLI's recommended default; `web/` is already a boundary, so `web/src/` adds a level for nothing |
| `--no-react-compiler` | off | the CLI default; turning it on is one config flag, decided in the map task if profiling of deck.gl updates warrants it (global rules: no preemptive optimisation) |
| `--cache-components` | on | the CLI default for App Router. `[VERIFY: that "pnpm build" succeeds with Cache Components on and a client QueryClientProvider in the root layout — if it doesn't, stop and ask; don't switch it off silently]` |
| `--disable-git` | yes | the repo already is a git repo |
| `--agents-md` | yes | the CLI default; keeps an implementer on current Next.js APIs |
| `--no-agent-feedback` | off | no telemetry-style feedback from this project |

If the CLI still prompts for anything, the answer is the "Choice" column above; anything not
covered → stop and ask.

**After generation, the only web changes are:**

1. `pnpm --dir web add @tanstack/react-query` — server state goes through TanStack Query
   (global rules). No devtools package (not needed until there's a query).
2. `web/app/get-query-client.ts` and `web/app/providers.tsx`: copy the **official
   dual-client pattern** from the TanStack advanced-SSR guide linked above, as published at
   implementation time:
   - `makeQueryClient()` with `defaultOptions.queries.staleTime: 60 * 1000` (the guide's
     value, so the client doesn't refetch right after SSR);
   - `getQueryClient()`: a new client on every server call; one module-level
     `browserQueryClient` reused in the browser. The guide (fetched 2026-10-12) uses
     `environmentManager.isServer()` from `@tanstack/react-query` for the check — use
     whatever the guide shows at implementation time;
   - `Providers` is `'use client'` and wraps `children` in `QueryClientProvider`. **Not**
     `useState(() => new QueryClient())` (global rules; the guide says why).
3. `web/app/layout.tsx`: wrap `{children}` in `<Providers>`. Nothing else in the generated
   layout or page changes.
4. `web/hooks/.gitkeep`.
5. `web/.nvmrc` containing `24` (nvm is how Node is managed locally).

**Not installed:** MapLibre GL, deck.gl, shadcn/ui, a test runner for the web. Each comes with
the task that first needs it.

### 2.3 Local infrastructure: `compose.yaml`

Two services. **No Redpanda Console:** `docker compose exec redpanda rpk …` covers topic and
consumer-group inspection at this stage, and Console would cost memory the 2 GiB budget
doesn't have. It can come back later behind a Compose profile if a task justifies it.

**Images (checked on Docker Hub, 2026-10-12):**

| Service | Image | arm64? | Notes |
|---|---|---|---|
| redpanda | `docker.redpanda.com/redpandadata/redpanda:v26.2.4` | **yes** (amd64 + arm64, pushed 2026-10-06) | the tag Redpanda's quickstart uses (<https://docs.redpanda.com/current/get-started/quick-start/>) |
| timescaledb | `timescale/timescaledb-ha:pg18.6-ts2.30.2` | **yes** (amd64 + arm64, pushed 2026-10-07; ~618 MiB compressed for arm64) | see the PostGIS note below |

**The PostGIS question** (from the task brief):

- **`timescale/timescaledb-ha`** — its README (<https://github.com/timescale/timescaledb-docker-ha>)
  says "the Docker image contains many extensions, including TimescaleDB and PostGIS", and
  Docker Hub lists arm64 for every `pg18*` tag checked. `-all` tags only add older Postgres
  majors; `-oss` tags drop Timescale-License code (which includes compression and
  continuous-aggregate features we rely on, `[VERIFY: what -oss drops, in the same README]`),
  so **neither suffix is used**.
- **`timescale/timescaledb`** (the non-HA image) — its README doesn't say whether PostGIS is
  included. Not chosen.
- **Still to verify by running it** (step 5 of §2.9 checks it):
  `[VERIFY: that timescale/timescaledb-ha:pg18.6-ts2.30.2 for linux/arm64 has postgis in
  pg_available_extensions]`. If it doesn't: **stop and ask.** Don't build a custom image.
- **Postgres 18 over 17:** 18 is the newest major the image publishes and there's no
  existing data to migrate. `[VERIFY: TimescaleDB's supported-Postgres matrix lists 18, at
  https://docs.timescale.com/self-hosted/latest/upgrades/upgrade-pg/ or the current
  equivalent page]`
- **Pin the full version tag**, not `pg18`: the tag must be identical in `compose.yaml` and
  in `internal/testharness/harness.go` (and a comment in each points to the other). If a
  newer full tag exists at implementation time, use it in both places.

**`compose.yaml` requirements:**

- `name: transit-pulse`; one bridge network `transit-pulse`; named volumes
  `transit-pulse-redpanda` and `transit-pulse-timescaledb`.
- **redpanda:**
  - `redpanda start` with `--mode dev-container`, `--smp 1`, `--memory 512M`,
    `--default-log-level=info`. `[VERIFY: --memory semantics under dev-container mode, at
    https://docs.redpanda.com/current/reference/rpk/rpk-redpanda/rpk-redpanda-start/]`
  - Two Kafka listeners: `internal://0.0.0.0:9092` (advertised `redpanda:9092`) and
    `external://0.0.0.0:29092` (advertised `localhost:29092`). RPC on `redpanda:33145`.
  - **No Schema Registry or HTTP Proxy ports exposed**: the contract is a JSON Schema file
    (§10 #7), not a registry.
  - Ports: `127.0.0.1:29092:29092` (Kafka), `127.0.0.1:29644:9644` (Admin API). **29xxx, not
    renewable-pulse's 19xxx**, so both stacks can run side by side on the Mac.
  - `mem_limit: 700m` (renewable-pulse's production limit for the same flags,
    `infra/aws/compose.prod.yml`).
  - Health check (proven in renewable-pulse): `rpk cluster health | grep -E
    'Healthy:.+true'`, `interval: 3s`, `timeout: 5s`, `retries: 20`.
- **timescaledb:**
  - `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` from `.env`, written as
    `${POSTGRES_PASSWORD:?set it in .env}` so a missing value fails loudly.
  - Mounts `./infra/postgres/initdb` read-only at the image's init-script directory.
    `[VERIFY: init-script directory and PGDATA path for timescaledb-ha — the README/
    Dockerfile at https://github.com/timescale/timescaledb-docker-ha; the volume must be
    mounted at the actual data directory or data won't survive "make down && make up"]`
  - Port: `127.0.0.1:5435:5432` (renewable-pulse uses 5434).
  - `mem_limit: 450m` (renewable-pulse's production limit).
  - Health check: `pg_isready -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"`, `interval: 2s`,
    `timeout: 3s`, `retries: 30`.
  - `[VERIFY: mem_limit is honoured by "docker compose" (Compose spec), at
    https://docs.docker.com/reference/compose-file/services/#mem_limit]`
- No `restart:` policy locally (a crashed container should be noticed, not hidden).

**`infra/postgres/initdb/001-extensions.sql`:**

```sql
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS postgis;
```

That's the only SQL in this task. No tables: those come with the consumer task.

**Memory budget for the 2 GiB box** (architecture §6; renewable-pulse treats ~1.8 GiB as
usable):

| Container | Limit | Status |
|---|---|---|
| redpanda | 700 MiB | this task |
| timescaledb | 450 MiB | this task |
| adapters + consumer + API + Caddy | ≤ ~650 MiB together | sized by their own tasks |

### 2.4 Developer entry points

**`Makefile`, not `justfile`:** `make` is already on the Mac and on every Linux CI runner;
`just` isn't installed, and adding a tool to run six commands isn't worth it.

| Target | Command(s) | Notes |
|---|---|---|
| `.env` (file target) | `cp .env.example .env` | only if `.env` doesn't exist; `up` depends on it |
| `up` | `docker compose up -d --wait` | `--wait` blocks until health checks pass. `[VERIFY: --wait and --wait-timeout flags, at https://docs.docker.com/reference/cli/docker/compose/up/]`; use `--wait-timeout 120` |
| `down` | `docker compose down` | keeps volumes. **No target runs `down -v`**: deleting volumes is the user's call |
| `test` | `go test -count=1 ./...` | `-count=1` so the integration test isn't served from cache; needs Docker running |
| `lint` | `test -z "$$(gofmt -l $$(go list -f '{{.Dir}}' ./...))"`, `go vet ./...`, `golangci-lint run ./...`, `pnpm --dir web lint` | fails on any gofmt diff |
| `fmt` | `gofmt -w $$(go list -f '{{.Dir}}' ./...)` | Go only; `go list` keeps it out of `web/` |
| `web-dev` | `pnpm --dir web dev` | |
| `web-build` | `pnpm --dir web build` | |

All targets are `.PHONY` except `.env`.

**`.env.example`** — every variable the scaffold reads (only `compose.yaml` reads any):

```
# Local development only. Copied to .env by `make up` if .env doesn't exist.
POSTGRES_USER=transit_pulse
POSTGRES_PASSWORD=transit_pulse
POSTGRES_DB=transit_pulse
```

The Go tests read no variables (testcontainers picks ports itself). The web app reads none
yet.

### 2.5 Test harness

**`internal/testharness/harness.go`** (exported helpers used by every later integration test):

- `const RedpandaImage = "docker.redpanda.com/redpandadata/redpanda:v26.2.4"` and
  `const TimescaleImage = "timescale/timescaledb-ha:pg18.6-ts2.30.2"` — the same tags as
  `compose.yaml`.
- `func StartRedpanda(t testing.TB) (seedBroker string)`: `redpanda.Run(ctx,
  RedpandaImage, redpanda.WithAutoCreateTopics())`, `ctr.KafkaSeedBroker(ctx)`, and
  `t.Cleanup` that terminates the container (`testcontainers.TerminateContainer` or the
  current documented equivalent).
- `func StartTimescale(t testing.TB) (connString string)`: `postgres.Run(ctx,
  TimescaleImage, …)` with:
  - database/user/password `transit_pulse` (never the database name `postgres`; the
    module's snapshot feature drops the connected database);
  - the init script `infra/postgres/initdb/001-extensions.sql` via the module's init-script
    option `[VERIFY: option name, e.g. postgres.WithInitScripts, at
    https://golang.testcontainers.org/modules/postgres/]`, so tests and compose share one
    file;
  - wait strategies as the module page recommends: the log line `"database system is ready
    to accept connections"` with occurrence 2, **plus** `wait.ForListeningPort("5432/tcp")`
    (flaky on macOS without it, per that page). `[VERIFY: the -ha image logs that line twice
    on first start; if not, stop and ask rather than guessing a different wait]`;
  - `ctr.ConnectionString(ctx, "sslmode=disable")`;
  - `t.Cleanup` that terminates it.
- Both helpers call `t.Fatalf` on failure. **They never `t.Skip` when Docker is missing:** a
  skipped integration test reads as green.

**`internal/testharness/harness_test.go` — the smoke test, `TestSmoke`:**

1. Start both containers with `t.Parallel()` subtests or sequentially (implementer's choice;
   record the timing either way).
2. **Redpanda:** a `kgo.Client` with `kgo.SeedBrokers(seed)`,
   `kgo.AllowAutoTopicCreation()`, `kgo.ConsumeTopics("scaffold.smoke.v1")`,
   `kgo.ConsumeResetOffset(kgo.NewOffset().AtStart())`. `ProduceSync` one record with key
   `smoke` and value `hello transit-pulse`, then `PollFetches` until that record arrives.
   Assert key and value are equal byte-for-byte. The whole round trip runs under a
   `context.WithTimeout` of **10 s**. `[VERIFY: these kgo option names at
   https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo]`
3. **TimescaleDB:** `pgx.Connect`, then exactly
   `SELECT postgis_version(), extversion FROM pg_extension WHERE extname = 'timescaledb'`.
   Assert one row, `postgis_version()` non-empty, and `extversion` equals the TimescaleDB
   version in the image tag (`2.30.2` today — derive it from `TimescaleImage` with a
   `strings` split, not a second hard-coded literal).

**`internal/samples/samples.go` — load real captures by name:**

- `func Path(name string) (string, error)`: finds the repo root by walking up from the
  package directory (`runtime.Caller(0)`) until it finds `go.mod`, then returns
  `<root>/docs/research/samples/<name>`. Rejects any `name` containing a path separator or
  `..` (samples are flat, named `<operator>-<YYYYMMDD>-<what>.<ext>`). Returns an error if the
  file doesn't exist.
- `func Read(name string) ([]byte, error)`.
- `func MustRead(t testing.TB, name string) []byte`: `t.Fatalf` on error, never `t.Skip`.
- No decoding, no parsing: parsers belong to the adapter tasks.

**`internal/samples/samples_test.go`:**

- `MustRead(t, "smtr-its-20261012-veiculos-conecta.json")` returns **exactly 26,048 bytes**
  (size on disk, 2026-10-12).
- `MustRead(t, "sppo-20261012-gtfsrt-vehicle-positions.pb")` returns **exactly 126,988
  bytes**.
- `Read("does-not-exist.json")` returns an error.
- `Read("../CLAUDE.md")` and `Read("a/b.json")` return an error.

### 2.6 Next.js baseline

Covered in §2.2: `create-next-app` output, `@tanstack/react-query`, the dual-client provider
(`get-query-client.ts` + `providers.tsx`), the layout wrap, `hooks/.gitkeep`, `.nvmrc`.
Nothing else: no map, no MapLibre/deck.gl, no components, no hooks yet (there's no query to
wrap).

### 2.7 CI: none in this task

- **No CI now.** The repo has no remote, so a workflow file would never run, and an unrun CI
  config is exactly the "placeholder that pretends to work" this task excludes.
- The Makefile targets are what CI will call. When the user adds a GitHub remote, a short
  follow-up adds `.github/workflows/ci.yml` with three jobs: `make lint`, `make test`
  (ubuntu runners have Docker, which testcontainers needs `[VERIFY at that time]`),
  `make web-build`.

### 2.8 Acceptance criteria

Run on the Mac (Apple M2, 8 GiB, Docker Desktop 29.7.2) with images already pulled — a cold
pull is network time, not startup time. "Median of 3" means three separate runs; record all
three values in the outcome section (§6), not just the median.

| # | Command | Expected |
|---|---|---|
| A1 | `make down && time make up` ×3 | exit 0; `docker compose ps` shows `redpanda` and `timescaledb` both `healthy`; **median ≤ 60 s** |
| A2 | `scripts/idle-memory.sh` after A1, with the stack idle ≥ 60 s | prints per-container `docker stats --no-stream` memory, sampled 3 times 10 s apart, and the median. **Redpanda ≤ 700 MiB, TimescaleDB ≤ 450 MiB** (their limits), and `docker inspect -f '{{.State.OOMKilled}}'` is `false` for both. Over a limit → stop and ask; don't raise it |
| A3 | `time make test` ×3 | exit 0; `TestSmoke` and the `samples` tests pass, none skipped (`go test -v` shows no `SKIP`); **median ≤ 120 s** with images cached; produce→consume ≤ 10 s (enforced by the test's timeout) |
| A4 | `make lint` | exit 0; `gofmt -l` prints nothing; golangci-lint reports **0 issues**; ESLint **0 errors, 0 warnings** |
| A5 | `make web-build` | exit 0 |
| A6 | `make web-dev` then `curl -s -o /dev/null -w '%{http_code}' http://localhost:3000` | `200` |
| A7 | `docker compose exec timescaledb psql -U transit_pulse -d transit_pulse -c "SELECT postgis_version(), extversion FROM pg_extension WHERE extname = 'timescaledb'"` | one row; PostGIS version non-empty; extversion `2.30.2` (or the pinned tag's version) |
| A8 | `docker compose exec redpanda rpk topic create scaffold.check && docker compose exec redpanda rpk topic delete scaffold.check` | both exit 0 |
| A9 | Persistence: create `CREATE TABLE persist_check(x int)` via psql, `make down && make up`, `\dt persist_check`, then `DROP TABLE persist_check` | the table survives the restart (proves the volume is mounted at the real PGDATA) |
| A10 | `lsof -nP -iTCP:29092 -iTCP:29644 -iTCP:5435 -sTCP:LISTEN` | every listener bound to `127.0.0.1`, none to `*` or `0.0.0.0` |
| A11 | `grep -h 'redpanda:v\|timescaledb-ha:pg' compose.yaml internal/testharness/harness.go` | the same two tags in both files |
| A12 | `go list -m all \| wc -l` and `go list ./...` | `go list ./...` shows only `internal/testharness` and `internal/samples` (nothing under `web/`) |
| A13 | `git status --porcelain` after the commit | clean; no `node_modules`, `.next`, `.env` tracked (`git ls-files \| grep -E 'node_modules\|\.next/\|^\.env$'` prints nothing) |

**`scripts/idle-memory.sh`:** bash, `set -euo pipefail`; for `i in 1 2 3`: `docker stats
--no-stream --format '{{.Name}} {{.MemUsage}}'` filtered to the two `transit-pulse-*`
containers, `sleep 10` between samples; prints the three samples and the median per container
in MiB. No dependencies beyond bash, docker, awk and sort.

**Record the A2 medians in `docs/architecture.md` §6**, replacing "renewable-pulse measured
Redpanda at about 730 MiB idle and TimescaleDB at about 240 MiB" with this repo's figures,
labelled "measured on the Mac (Docker Desktop, arm64), 2026-MM-DD, median of 3; re-measure
on the t4g.small". Keep the `[VERIFY]` for the box itself.

### 2.9 Handoff to the implementer (follow in order)

Each step ends with its verification. If a verification fails, fix only what the step
covers; if the fix needs a decision this document doesn't make, stop and ask.

1. **Re-check versions** against every link in §2.2 and the two images in §2.3 (Docker Hub
   tag page: arm64 present?). Write down what you'll use.
   *Verify:* a short list in your notes; if anything differs from this document, say so in
   the final summary.
2. **Go toolchain.** Stay on 1.26 (§2.2). **Don't** run `brew upgrade go`. The patch comes
   from the `toolchain` line written in step 4.
   *Verify (after step 4):* `go version` run inside the repo prints `go1.26.9` (or the
   latest 1.26.x patch); run in renewable-pulse it still prints `go1.26.6`.
3. **golangci-lint.** `brew install golangci-lint` (or upgrade).
   *Verify:* `golangci-lint version` shows v2.x.
4. **Go module.** `go mod init github.com/BenitoPedro13/transit-pulse`, then
   `go mod edit -go=1.26 -toolchain=go1.26.9` (or the latest 1.26.x patch), then add
   `ignore ./web` to `go.mod`; write `.golangci.yml`.
   *Verify:* `go list -m` prints the module path; `go version` matches step 2.
5. **Infra.** Write `compose.yaml`, `infra/postgres/initdb/001-extensions.sql`,
   `.env.example`, `Makefile` (resolve the §2.3 and §2.4 `[VERIFY]`s first).
   *Verify:* A1, A7, A8, A9, A10. A7 is the PostGIS-on-arm64 check: if PostGIS is missing,
   stop and ask.
6. **Idle memory.** Write `scripts/idle-memory.sh` (`chmod +x`).
   *Verify:* A2. Over a limit → stop and ask.
7. **Samples helper.** `internal/samples/samples.go` + tests.
   *Verify:* `go test ./internal/samples/ -v` passes, no SKIP.
8. **Test harness.** `go get` the dependencies in §2.2 (exactly those), write
   `internal/testharness/harness.go` + `harness_test.go`, `go mod tidy`.
   *Verify:* `go test -count=1 -v ./internal/testharness/` passes; then A3, A11, A12.
9. **Web.** Run the `create-next-app` command in §2.2 verbatim from the repo root; then
   `pnpm --dir web add @tanstack/react-query`; add `get-query-client.ts`, `providers.tsx`,
   the layout wrap, `hooks/.gitkeep`, `.nvmrc`.
   *Verify:* A5, A6, and `pnpm --dir web lint` clean.
10. **`schemas/.gitkeep`.**
11. **Full run.** *Verify:* A1–A13 all pass; record the three runs of A1, A2, A3 in §6 of
    this document.
12. **Docs** (§2.10). *Verify:* `grep -rn 'no code yet\|Nothing is installed' README.md
    CLAUDE.md` finds nothing stale.
13. **Commit** once everything above is green: one commit, a plain message (e.g. "Scaffold Go
    module, Next.js app, local infra and test harness"), **no `Co-Authored-By` trailer**
    (CLAUDE.md §4.1). No push (there's no remote).

**Stop and ask, don't improvise, when:**

- **any version conflict**: the 1.26 toolchain download failing or golangci-lint refusing
  1.26 (step 2; never jump to 1.27 on your own), a pinned tag that no longer exists, a
  library whose current API differs from what §2.5 names, a peer-dependency warning from pnpm;
- **an image without linux/arm64**, or the `-ha` image without PostGIS;
- **anything touching architecture §10 #9–#12**: event identity, which SMTR path to read,
  phase 2 rail/ferry data, the static GTFS. This task writes no event schema, no topic other
  than the throwaway `scaffold.smoke.v1`/`scaffold.check`, no table, and fetches nothing from
  any provider;
- a memory figure over its limit (A2), or a startup median over 60 s (A1);
- `create-next-app` asking a question not covered in §2.2, or generating something that
  needs editing beyond §2.2's five changes;
- a `[VERIFY]` that the linked docs can't resolve.

### 2.10 Documentation updates (part of the task, §3 of CLAUDE.md)

| File | Change |
|---|---|
| `CLAUDE.md` §0 | status line: scaffolded, no adapter yet. Stack table: "Nothing is installed" → what is; add the actual versions used; Status column "scaffolded" for Broker, Store, Web, and the test harness |
| `CLAUDE.md` §4 | replace the docs-only tree with the layout in §2.1 |
| `docs/architecture.md` §6 | the measured idle memory (A2), replacing renewable-pulse's numbers |
| `README.md` | status line; a **Setup** section (prerequisites: Docker, Go, golangci-lint, Node 24 + pnpm) and the Makefile targets |
| `.env.example` | new (§2.4) |
| this file | status → done, plus an outcome section (§6) with the measured numbers and any deviation |

### 2.11 Alternatives considered and rejected

- **Several Go modules + `go.work`** — §2.1.
- **A pnpm/Turborepo monorepo root** — §2.1; one JS package doesn't need a workspace.
- **`justfile`** — §2.4; `make` is already installed.
- **Redpanda Console in compose** — §2.3; memory, and `rpk` is enough.
- **`timescale/timescaledb` (non-HA) + installing PostGIS ourselves** — a custom image to
  build and keep patched, for an extension the `-ha` image already ships.
- **`postgis/postgis` + installing TimescaleDB** — same objection, the other way round.
- **Mocking Kafka/Postgres in tests** — forbidden (CLAUDE.md "Tests").
- **A `Skip` when Docker is unavailable** — a silent green; integration tests fail instead.
- **Biome instead of ESLint** — loses `@next/eslint-plugin-next`'s rules; renewable-pulse
  uses ESLint.
- **`src/` directory, React Compiler on** — both off by the CLI's own defaults; §2.2.
- **Adding empty `cmd/*` mains or a stub API** — placeholder code; excluded by the brief.
- **CI now** — §2.7.

## 3. Why

- **Unblocks every later task:** the canonical-event schema lands in `schemas/` and
  `internal/event`; adapters, consumer and API land in `cmd/*`; their integration tests reuse
  `internal/testharness`; their parser tests read real captures through `internal/samples`;
  the map lands in `web/` with the query provider already in place.
- **Turns the memory risk in architecture §6 into this repo's own numbers** instead of
  renewable-pulse's, before any service is added on top.
- **Proves the riskiest infrastructure assumption early:** that one arm64 image gives
  TimescaleDB + PostGIS without a custom build.
- **Cost:** no money and no AWS. About 1.3 GB of images locally (Redpanda + `-ha`), a few
  hundred MB of `node_modules`, and Go module downloads. Time: roughly an afternoon.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `docs/tasks/TASK-scaffold.md` | new | this document |
| `go.mod`, `go.sum` | new | one module; `go 1.26`, `toolchain go1.26.9`; `ignore ./web` |
| `.golangci.yml` | new | v2, default linters |
| `Makefile` | new | `up`, `down`, `test`, `lint`, `fmt`, `web-dev`, `web-build`, `.env` |
| `compose.yaml` | new | redpanda + timescaledb-ha, localhost-only ports, limits, health checks |
| `.env.example` | new | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` |
| `infra/postgres/initdb/001-extensions.sql` | new | `timescaledb`, `postgis` |
| `internal/testharness/harness.go` | new | `StartRedpanda`, `StartTimescale`, image constants |
| `internal/testharness/harness_test.go` | new | `TestSmoke` |
| `internal/samples/samples.go` | new | `Path`, `Read`, `MustRead` |
| `internal/samples/samples_test.go` | new | sizes of two real samples; error cases |
| `schemas/.gitkeep` | new | empty until the schema task |
| `scripts/idle-memory.sh` | new | median-of-3 idle memory |
| `web/**` | new | `create-next-app` output |
| `web/app/get-query-client.ts`, `web/app/providers.tsx` | new | TanStack dual-client pattern |
| `web/app/layout.tsx` | edit (generated) | wrap children in `<Providers>` |
| `web/hooks/.gitkeep`, `web/.nvmrc` | new | |
| `CLAUDE.md` | edit | §0 status + stack table, §4 layout |
| `docs/architecture.md` | edit | §6 idle memory |
| `README.md` | edit | status, setup, targets |

## 5. Suggested follow-up tasks, in order

| # | Task | Blocked on |
|---|---|---|
| 1 | **Canonical-event JSON Schema** (`schemas/vehicle-position.v1.json`, `internal/event`, topic name, validation in Go) | **architecture §10 #9** — whether the supplier is part of the identity changes the schema's key fields and the primary key |
| 2 | **ITS adapter + raw log** (`cmd/adapter-…`, hourly-batched S3 objects per research "Raw volume", polite polling, idempotency test against the samples) | **§10 #10** — which live path the first adapter reads (ITS / GTFS-RT / legacy); also #9 via the schema |
| 3 | **Consumer** (consumer group → TimescaleDB hypertable with 3-day retention, continuous aggregates, replay-equals-live test) | tasks 1–2 |
| 4 | **API** (precomputed views + live positions, "last updated at …", coverage tiers) | task 3 |
| 5 | **Map** (MapLibre + deck.gl + table alternative, Protomaps tiles) | task 4. Route lines from `shapes.txt`: shapes still join ≈98% (research §2), so **#12** doesn't block it, but its answer decides which GTFS the map loads |

**Before the adapter task, the user answers §10 #9 and #10.** Those two decide the event schema
and what the first adapter reads. **#11** (phase 2) and **#12** (stale GTFS) can wait: #11
only matters for phase 2, and #12 only refines which static feed the map and the headway
metric use.

## 6. Outcome

*(Filled in by the implementer: versions actually used, the three runs of A1/A2/A3, every
resolved `[VERIFY]` with its source link, and any deviation from this plan.)*
