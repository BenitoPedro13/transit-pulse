# TASK: Canonical event v1 + the ITS API adapter

**Status (2026-10-12): planned, not started.** Written for a Sonnet session to execute end to
end. It depends on the decisions in §2.1, which the user made on 2026-10-12. Read
`CLAUDE.md` first, then `docs/research/README.md` §1, then this document.

## 0. Rules for whoever executes this

- **Follow the order in §6.** Steps 1–2 resolve `[VERIFY]`s that later steps depend on.
- **Never invent a provider field, parameter or behaviour.** Everything about the ITS API
  here is from `docs/research/README.md` §1.1 and the samples. If the live API disagrees
  with this document, the live API wins: record what you saw in §7, update
  `docs/research/README.md`, and **stop and ask the user** if it changes the design (not for
  a cosmetic difference).
- **Check every library version against its own current docs before adding it** (CLAUDE.md
  §2.0). The versions named here are what was known on 2026-10-12, not pins.
- **Tests run against real infrastructure and real samples** (CLAUDE.md "Tests"). An
  `httptest` server that serves *captured* response bytes is fine. A hand-written provider
  JSON body is not.
- **Don't widen the scope.** No consumer, no database tables, no API, no map, no S3 upload
  and no GTFS-RT. Those are separate tasks (§2.7).
- When done: fill §7, update the docs listed in §4, run `make lint` and `make test`, and
  commit **without a `Co-Authored-By` trailer**. Don't push.

## 1. Current scenario

- The repo is scaffolded and uses the `apps/` + `packages/` layout
  (`docs/tasks/TASK-repo-layout.md`). It has one Go module, a real-infrastructure test
  harness (`packages/testharness`: Redpanda + TimescaleDB) and a sample loader
  (`packages/samples`). There's no binary, no event type and no schema yet
  (`packages/contracts/` is empty).
- `docs/research/README.md` §1.1 verified the ITS API on 2026-10-12:
  - `GET https://its.mobilidade.rio/v1/geolocalizacao/veiculos`, keyless.
  - It returns `minuto_utc`-windowed positions for every supplier: a median of 10,883
    records/min over 3 pages at `limit=5000`.
  - A completed minute is immutable for at least 24 h.
  - It sends no validators and no gzip. The gateway allows 5 requests/s.
- Architecture §10 #9 and #10 blocked this task until the user answered them (§2.1).

## 2. Planned changes

### 2.1 Decisions this task implements (user, 2026-10-12)

- **#10, the live path:** **the ITS API only.** It's the source of truth for phase 1.
  GTFS-RT isn't ingested now; it may be added later as a cross-check.
- **#9, identity across suppliers:** **the supplier joins the identity.** An event's
  identity is `(operator, feed, vehicle, event_time)`.
  - `feed` is an opaque device-source tag. For the ITS API it's `its:<fornecedor>`
    (`its:conecta`, `its:zirix`, `its:maxtrack`, `its:sonda`).
  - Nothing downstream may branch on the value of `feed`. It's only for identity, and for a
    later projection that picks one fix per vehicle (e.g. the freshest).
  - Both `conecta` and `zirix` observations of the same bus are kept.

### 2.2 The canonical event v1: `packages/contracts/position.v1.schema.json`

A JSON Schema (draft 2020-12) file. It's the contract. The Go type in §2.3 mirrors it by
hand, and a test enforces that they match (§5 E2).

| Field | Type | Rule |
|---|---|---|
| `city` | string, enum `rio` | |
| `mode` | enum `bus` `brt` `rail` `metro` `tram` `ferry` | finalised here (architecture §3) |
| `operator` | string, pattern `^[a-z0-9-]+$` | our slug, from the table in §2.4.4 |
| `authority` | enum `city` `state` | |
| `source_kind` | enum `live` `scheduled` | always `live` in this task |
| `feed` | string, pattern `^[a-z0-9-]+:[a-z0-9-]+$` | §2.1 |
| `vehicle` | string, minLength 1 | ITS `id_veiculo`, as given |
| `line` | string or null | ITS `servico`; `""` becomes null. Never `"0"`-guessed: kept as given if non-empty |
| `route_id` | string or null | ITS `route_id`; `""` becomes null |
| `lat`, `lon` | number | WGS84, finite, in [-90, 90] / [-180, 180] |
| `event_time` | string, RFC 3339 UTC (`Z`) | ITS `datetime` (verified real UTC, research §1.5) |
| `received_time` | string, RFC 3339 UTC | the raw envelope's `fetched_at` (§2.5), so replays reproduce it |
| `speed_kmh` | number or null | ITS `velocidade`; null if absent or negative |
| `heading_deg` | number or null | ITS `direcao`; null if absent or outside [0, 360] |
| `raw_ref` | object `{key, minute_utc, page}` | where the source bytes live (invariant 1) |

`additionalProperties: false`. Speed and heading are in: research §1.5 shows every SMTR path
carries them, and the map needs heading for arrows. Values outside the ranges become null
and are counted (§2.4.6). They're never clamped or corrected.

### 2.3 `packages/event/`: Go type and identity

- `event.go`:
  - a `Position` struct with JSON tags matching §2.2 exactly;
  - `func (p Position) Key() string`, returning `operator + "/" + feed + "/" + vehicle`.
    That's the Kafka message key, so all of one device's fixes land in one partition, in
    order.
  - `func (p Position) Identity() string`, returning `Key() + "/" + event_time`.
- `event_test.go` validates marshalled `Position`s against the schema file. Use
  `github.com/santhosh-tekuri/jsonschema/v6`
  `[resolved, §7: v6.0.3 supports draft 2020-12]`. It's a test dependency only.
  If it's unmaintained or doesn't support draft 2020-12, pick another library and record
  why in §7.

### 2.4 `apps/adapter-its/`: the adapter binary

`package main`. It's split into small files so each piece has its own test. Suggested files:
`main.go`, `config.go`, `client.go`, `schedule.go`, `mapping.go`, `publish.go`.

#### 2.4.1 Which minute is fetched, and when

- **The unit of work is one UTC minute** `M` (`minuto_utc=M`, e.g. `2026-10-12T13:38:00Z`).
- `M` is fetched once `now ≥ M + 1 min + ITS_MINUTE_LAG`. The documentation allows "up to
  60 s from reception to availability", so the lag can't be shorter than that.
  `ITS_MINUTE_LAG` comes from measurement (§6 step 2). It's never guessed. If step 2 can't
  run, it defaults to `3m`.
- **Steady state:** one minute per minute, each fetched as all of its pages (§2.4.2).
- **Startup and gaps:** every minute from the oldest missing minute to the newest due one
  is a to-do item.
  - "Missing" means the raw log has no complete set of pages for that minute (§2.5).
  - Look back at most `ITS_BACKFILL_WINDOW` (default `23h`): history is verified for 24 h,
    and the extra hour is a margin.
  - Work runs **oldest first**. Live minutes take priority, so a backfill never delays the
    current minute by more than one request.
  - A minute older than the window is never fetched. It's reported once as a permanent gap
    (§2.4.6).

#### 2.4.2 HTTP client, and being a polite client (invariant 4)

- Requests are `?minuto_utc=M&limit=5000`, then `&cursor=<next_cursor>` until
  `next_cursor` is null.
  - In the sample, the cursor is opaque base64 (`eyJvIjo0MCwicSI6bnVsbH0=`). Pass it back
    unchanged and never decode it.
  - **No other filters.** One minute is ≈ 3 requests and covers every supplier.
- **A global rate limiter of 1 request/s with a burst of 1** (`golang.org/x/time/rate` or a
  ticker). That's 20% of the documented 5 requests/s, so backfill can't hammer the
  gateway. A 23 h backfill (≈ 1,380 min × 3 pages ≈ 4,140 requests) takes about 70 min.
  That's accepted.
- **Timeout:** 45 s per request. The gateway documents a 15–30 s timeout.
- **Header:** `User-Agent: transit-pulse/<version> (+<ITS_CONTACT>)`. `ITS_CONTACT` comes
  from `.env`, and `.env.example` ships it empty with a comment. **Don't commit an email
  address.** If it's empty, the header has no `(+…)` part.
- **Retries:**
  - On a transport error, a 5xx, a 429 or a truncated body (research §1.3 saw a truncated
    response, `IncompleteRead`): exponential backoff from 2 s, doubling to a 60 s cap, ±20%
    jitter, at most 5 attempts per page. A 429 honours `Retry-After` if it's sent.
  - **A 4xx other than 429 is not retried.** It means the request is wrong: log it with the
    body, and the minute stays missing.
  - After the last attempt, the minute is **failed** (§2.4.6). It stays in the to-do set and
    is tried again on the next cycle, until it's older than the backfill window.
- **Every attempt's response goes to the raw log before anything else** (§2.5), including
  failures, which are stored with their status and error. "Fetch failed" is then a recorded
  fact, not an absence (invariant 6).

#### 2.4.3 Pages → events: a pure function over the raw envelope

- `func Events(env raw.Envelope) (events []event.Position, stats Stats, err error)` takes a
  **stored raw envelope**, never the live `*http.Response`.
- The adapter writes the envelope (§2.5), reads its bytes back through the same `raw`
  reader, and only then calls `Events`.
- Live ingestion and a later replay therefore run the same function on the same bytes. That
  makes "replay = live" (CLAUDE.md "Tests") true by construction.

#### 2.4.4 Mapping, verified against the four samples

| `fornecedor` | `sistema` | `modo` | → `operator` | `mode` | `authority` |
|---|---|---|---|---|---|
| `conecta` | `sppo` | `onibus` | `rio-sppo` | `bus` | `city` |
| `zirix` | `sppo` | `onibus` | `rio-sppo` | `bus` | `city` |
| `maxtrack` | `rio` | `onibus` | `rio-sistema-rio` | `bus` | `city` |
| `sonda` | null | `brt` | `rio-brt` | `brt` | `city` |

- The table is a Go `map` keyed on the exact triple.
- **Any other triple isn't emitted.** It's counted as `unmapped` and logged once per
  distinct triple per hour. The raw log still has it. A new supplier is added by editing the
  table after you've looked at its data, never by a fallback.
- `city` is always `rio`, `source_kind` is always `live`, and `feed` is
  `"its:" + fornecedor`.

#### 2.4.5 Within-minute de-duplication and invalid records

- **Exact duplicates are dropped before publishing,** keeping the first. An exact duplicate
  has the same identity and the same lat, lon, speed and heading. `sonda` re-serves ≈ 34%
  of its rows within a minute (research §1.1).
- **Same identity but a different position** (it shouldn't happen within one feed):
  - keep the first and count `conflicting_duplicates`;
  - log the first 5 per minute with both values.
  - Don't pick a "better" one.
- **Invalid records** aren't emitted and are counted as `invalid`. Invalid means: missing
  `id_veiculo` or `datetime`; an unparseable `datetime`; a non-finite or out-of-range
  lat/lon; or lat == 0 && lon == 0.
- **Stale or bogus timestamps are not filtered here.** `sonda` serves fixes up to years old
  (research §1.1), and they're still real observations. The 5-minute staleness rule
  (architecture §2) belongs to the projection, not the adapter.

#### 2.4.6 Feed status: `feed-status.v1`

One message per `(feed, minute)` lets downstream tell "no vehicles reported" from "fetch
failed" (invariant 6).

- Key: `feed + "/" + minute_utc`. Value:
  `{feed, operator, minute_utc, status, records, events, invalid, unmapped,
  conflicting_duplicates, provider_status, raw_keys}`.
- `status` is one of:
  - `ok`;
  - `failed`: our fetch failed after retries;
  - `provider_not_ok`: `providers_status[fornecedor]` isn't `"ok"`;
  - `gap`: a permanent miss, older than the backfill window.
- **When the fetch fails, the supplier list isn't known.** Publish one `failed` message per
  feed in the mapping table (§2.4.4).
- A minute that fails and later succeeds publishes `ok` with the same key. Latest wins.
- Give this message a JSON Schema too: `packages/contracts/feed-status.v1.schema.json`.

#### 2.4.7 Publishing (`publish.go`)

- **franz-go** `kgo` (already in `go.mod`). The idempotent producer is on by default, with
  `acks=all` and zstd compression.
  `[resolved, §7: the default compression is snappy, not zstd, so zstd is set explicitly]`.
- **Topics:** `positions.v1` and `feed-status.v1`.
  - Created at startup with `kadm` if missing: 6 partitions, replication factor 1.
  - `positions.v1` gets `retention.ms` = 24 h. `feed-status.v1` gets `cleanup.policy=compact`.
  - **The broker is a buffer, not the record:** the raw log is (invariant 2).
- **Publish checkpoint:** `RAW_DIR/its/state/published-through`. It's a single RFC 3339
  minute, written atomically (temp file + rename) after **every** message of a minute and
  of all earlier minutes is acked.
  - On startup, every minute from the checkpoint up to the raw-log head is re-published
    from the raw log.
  - Duplicates on the topic are possible after a crash. They're harmless, because the
    consumer is idempotent on `Identity()` (architecture §10 #7). That's the
    at-least-once contract, and it goes in the package doc comment.
- **If the broker is down:** raw-log writes continue, and publishing catches up from the
  checkpoint when the broker returns. A broker outage must never stop raw capture.

#### 2.4.8 Configuration (`config.go`) and logging

| Env var | Default | Notes |
|---|---|---|
| `KAFKA_BROKERS` | `127.0.0.1:29092` | compose's port (README) |
| `RAW_DIR` | `./var/raw` | `var/` added to `.gitignore` |
| `ITS_BASE_URL` | `https://its.mobilidade.rio` | |
| `ITS_MINUTE_LAG` | from §6 step 2 (fallback `3m`) | Go duration |
| `ITS_BACKFILL_WINDOW` | `23h` | |
| `ITS_RATE_PER_SEC` | `1` | must stay ≤ 1 unless the user changes it |
| `ITS_CONTACT` | empty | §2.4.2 |

- **Logging:** `log/slog`, JSON to stdout. One summary line per minute:
  `minute, pages, records, events, invalid, unmapped, dups, conflicting_duplicates,
  fetch_ms, raw_bytes_gz, lag_to_publish_ms`. §5's measurements come from these lines.
- No metrics endpoint yet.
- **Shutdown:** on SIGINT/SIGTERM, stop scheduling, finish the in-flight page, flush the
  producer, write the checkpoint and exit 0 within 10 s.

### 2.5 `packages/raw/`: the raw log, local first

The raw log is append-only on the local filesystem, with S3-shaped keys. Uploading to S3
belongs to the AWS infra task: that's where a real bucket exists to test against.

- **Key:** `its/v1/YYYY/MM/DD/HH.raw.gz`, where `HH` is the hour of the **minute fetched**
  (UTC), not the hour it was fetched in. One object per source per hour. That's the
  batching research "Raw volume" calls for: under $0.10 of PUTs for the whole run, and
  every object is above S3-IA's 128 KB minimum.
- **Format:** concatenated gzip members, one member per HTTP attempt. Each member holds:
  1. one JSON header line:
     `{v:1, source:"its", url, minute_utc, page, attempt, fetched_at, status, error,
     duration_ms, content_type, body_len, next_cursor}`;
  2. `\n`;
  3. exactly `body_len` bytes of the body **as received**.
  - The body isn't put inside a JSON string: Go's `encoding/json` would rewrite invalid
    UTF-8 and compact whitespace, and the bytes must stay exact (invariant 2).
- **Writes:** open with `O_APPEND|O_CREATE`, write the member, `fsync`, then close.
  `next_cursor` is copied into the header so pages can be chained without re-parsing.
- **Reader:**
  - `raw.ReadHour(path) ([]Envelope, error)` tolerates a **truncated last member** (a crash
    mid-write). It returns the complete members, and reports the truncation without
    failing.
  - A minute is **complete** when its envelopes form a chain from page 0 to a 200 response
    with `next_cursor == null`, every page a 200.
- **Startup scan:** read the hour files inside the backfill window and build the set of
  complete minutes. That set is the "already have it" check. **It's what makes re-fetching
  idempotent: a complete minute is never requested again** (invariant 3).
- **Files are never rewritten or deleted.** Appending to an earlier hour's file during a
  backfill is expected.

### 2.6 Makefile, env and samples

- `Makefile`: add `adapter-its` (`go run ./apps/adapter-its`, depends on `.env`). Add it
  to `.PHONY`.
- `.env.example`: every variable in §2.4.8, with comments.
- `.gitignore`: `var/`.
- **New real samples** (§6 step 1). Capture into `docs/research/samples/` and record them in
  the README "Samples" table:
  - `smtr-its-YYYYMMDD-paged-p{0,1,2}.json`: one small, really paged minute, from
    `?minuto_utc=<M>&id_veiculo=<a maxtrack vehicle>&limit=5`. That vehicle reports ≈ every
    5 s, so ≈ 12 records over ≈ 3 pages, and the last page has `next_cursor: null`.
    `[resolved, §7: it paginates, 5 + 5 + 4 over 3 pages]`. This is the paging and "minute complete" fixture. No
    hand-built final page.
  - `smtr-its-YYYYMMDD-error-<status>.json`: one real 4xx body, e.g. from an invalid
    `minuto_utc` like `minuto_utc=not-a-time`. Record the status you actually get.

### 2.7 Alternatives considered and rejected

- **Ingesting GTFS-RT as well:** the user chose ITS only (§2.1). It would be a second adapter.
- **Choosing one supplier per vehicle in the adapter:** rejected by the user (§2.1). It
  would drop real observations.
- **Building events from the in-memory response:** it would let live and replay drift
  apart. §2.4.3 builds them from the stored bytes instead.
- **One raw object per page:** ≈ 4,300 objects/day, PUT cost and IA minimum-size waste
  (research "Raw volume"). Hourly concatenated gzip is used instead.
- **Storing the body base64-encoded, or as a JSON string inside a JSONL file:** base64 is
  +33% before gzip, and a JSON string isn't byte-exact. Header line + raw bytes is used
  instead.
- **A local S3 emulator (MinIO/LocalStack) in compose now:** their licensing and
  distribution changed recently `[VERIFY before adopting either]`, and nothing here needs S3
  semantics yet. Deferred to the AWS infra task, which can test against real S3.
- **Filtering stale `sonda` fixes in the adapter:** that's a projection rule (§2.4.5). The
  adapter reports what the provider said.
- **Excluded from this task:** a consumer, database tables, `ON CONFLICT` and any
  TimescaleDB work. That's the next task (`TASK-positions-consumer`). Only the topic
  contract is fixed here.

## 3. Why

- It's the first real data path. Every later piece (the consumer, the aggregates, the API,
  the map, the replay) needs `positions.v1` and the raw log to exist.
- It turns the invariants into code early:
  - raw first and byte-exact (2);
  - idempotent by construction, through the complete-minute set and `Identity()` (3);
  - polite (4);
  - failed ≠ empty, through `feed-status.v1` (6).
- **Cost:** about 529 MB/day of local disk while running (research estimate, re-measured in
  E6), and ≈ 4,320 requests/day to SMTR (0.06% of the documented gateway limit over a day).
  Two new dependencies: `golang.org/x/time` (optional) and the JSON Schema validator
  (test-only).

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `packages/contracts/position.v1.schema.json` | new | §2.2 |
| `packages/contracts/feed-status.v1.schema.json` | new | §2.4.6 |
| `packages/event/event.go`, `event_test.go` | new | §2.3 |
| `packages/raw/raw.go`, `raw_test.go` | new | §2.5 |
| `apps/adapter-its/*.go` + tests | new | §2.4 |
| `go.mod`, `go.sum` | edit | `go get` only, never hand-edited |
| `Makefile` | edit | `adapter-its` target |
| `.env.example` | edit | §2.4.8 |
| `.gitignore` | edit | `var/` |
| `docs/research/samples/smtr-its-*-paged-p*.json`, `*-error-*.json` | new | §2.6 |
| `docs/research/README.md` | edit | Samples table, plus any new observed fact (lag measurement, error body) |
| `docs/architecture.md` | edit | §3 table final for v1, the `[VERIFY]`s this resolves, §5 if the shape changed |
| `CLAUDE.md` | edit | §0 status and stack table (ingestion "scaffolded" → ITS adapter built), §4 tree |
| `README.md` | edit | status line, `make adapter-its` |
| `docs/tasks/TASK-its-adapter.md` | edit | §7 results |

## 5. Acceptance criteria

Each one is recorded in §7 with the numbers. Timings are medians of 3 runs.

- **E1 Parsing the real samples.** `Events` over the four
  `smtr-its-20261012-veiculos-*.json` samples (40 records each, 160 in total):
  - the per-supplier counts of events, invalid, unmapped and dups are asserted **exactly**,
    with the numbers derived by reading the samples and written into the test;
  - `A48011` (conecta) has `event_time` `2026-10-12T13:37:43Z`, `operator` `rio-sppo` and
    `feed` `its:conecta`;
  - every `sonda` event has `mode` `brt` and `route_id` null (the sample has `""`).
- **E2 Contract.** Every event from E1, and every `feed-status` message produced in the
  tests, validates against its schema file. A test fails if the `Position` struct has a
  JSON field the schema doesn't, or the other way round.
- **E3 Raw is byte-exact.** For each sample: write it as an envelope, read it back, and the
  body is byte-identical (`bytes.Equal`), with `body_len` matching. A file whose last member
  is truncated returns the earlier envelopes and reports the truncation.
- **E4 Paging + completeness.** An `httptest` server serves the captured `paged-p*`
  samples in order:
  - the adapter makes exactly N requests, where N is the number of captured pages;
  - it marks the minute complete;
  - restarted against the same `RAW_DIR`, it makes **0** requests for that minute.
- **E5 Idempotency, with a real Redpanda** (`packages/testharness`):
  - processing the same raw envelope twice yields byte-identical event sets;
  - after a full adapter cycle, a restart with the checkpoint deleted (forcing a re-publish)
    leaves the topic's **distinct `Identity()` count** unchanged.
- **E6 Failure paths.**
  - A served 5xx followed by success leads to 2 envelopes for that page in the raw log, the
    minute complete, and `feed-status` `ok`.
  - A served 4xx (the captured error sample) leads to no retry, the minute missing, and
    `feed-status` `failed` for each mapped feed.
  - A connection reset mid-body is treated like a 5xx.
- **E7 Live run against the real API, 60 consecutive minutes** (`make up`, then
  `make adapter-its`). From the per-minute log lines:
  - 60/60 minutes complete, or each miss visible as `failed` and recovered later;
  - never more than 1 request/s, checked from the log timestamps;
  - median requests per minute ≤ 4;
  - the median records/min, raw gzip bytes/min and topic bytes/min are reported. Raw
    gzip/min is compared with research's 367 KB, and a gap of over ±50% is explained;
  - p95 of `lag_to_publish_ms` (publish ack − end of minute) ≤ `ITS_MINUTE_LAG` + 30 s;
  - the adapter's RSS, median of 3 samples taken 10 min apart, is ≤ 100 MiB.
- **E8 Restart and backfill.** Stop the adapter for 5 minutes during E7, then restart:
  - every missed minute is fetched;
  - no minute has more than one complete page chain in the raw log;
  - the checkpoint advances past the gap;
  - the backfill rate is still ≤ 1 request/s.
- **E9 Broker down.** Stop Redpanda for 3 minutes during the run:
  - raw capture continues with no missed minute;
  - after Redpanda returns, the checkpoint catches up and every minute is published.
- **E10** `make lint` gives 0 issues, `make test` exits 0 with 0 skipped, and the test
  runtime is the median of 3 runs.
- **E11** The docs in §4 are updated. `grep -rn "\[VERIFY" docs/tasks/TASK-its-adapter.md`
  lists only items that are explicitly deferred in §7, with the reason.

## 6. Execution order

1. **Capture the samples** (§2.6). Confirm that the four existing samples still match the
   live API's shape: same top-level keys, same record fields. Any difference goes to §7 and
   the research README.
2. **Measure `ITS_MINUTE_LAG`**, for 30 consecutive minutes, with a throwaway script in the
   session scratchpad, not the repo:
   - fetch each minute `M` at `M+90s`, `M+3m` and `M+10m`, all pages;
   - compare the record count and the SHA-256 of the sorted rows with the `+10m` fetch;
   - choose the smallest offset at which 30/30 minutes are identical, plus 30 s of margin;
   - record the table in research §1.1.

   At ≈ 9 requests/min this stays under 0.2 requests/s.
3. `packages/contracts` schemas → `packages/event` (E2).
4. `packages/raw` (E3).
5. `apps/adapter-its`: mapping and `Events` (E1) → client and schedule (E4, E6) →
   publish and checkpoint (E5).
6. Live runs E7, E8 and E9 (about 75 min in total).
7. Docs (§4), §7, lint, tests, commit.

## 7. Results

**Status (2026-10-12): done.** All eleven criteria are met, with the deviations listed at the
end. The live run was a single 60-minute run, not a median of 3 (see "Deviations").

The scripts, logs and raw data behind these numbers are in
`docs/research/runs/20261012-its-adapter/` (the 41 MB raw log is in git-ignored
`var/runs/`, see that README).

### Step 2: `ITS_MINUTE_LAG` (measured)

30 consecutive minutes (14:47–15:16 UTC), 4 fetch times each (`M+90s`, `M+2m`, `M+3m`, `M+10m`
as the reference; times from the start of `M`, and a minute closes at `M+60s`), 120 fetches,
no errors. Identical to the `+10m` fetch (record count and SHA-256 of the sorted rows):
`+90s` 30/30, `+2m` 30/30, `+3m` 30/30. The smallest offset tested, `+90s`, is 30 s after the
minute closes. With the plan's 30 s margin that is 60 s, which is also the documented bound,
so **`ITS_MINUTE_LAG` = `1m`** (default in `config.go`, and in `.env.example`). Table:
`docs/research/README.md` §1.1. It was measured on one Monday late morning only.

### Criteria

| | Result |
|---|---|
| **E1** | Over the four `veiculos-*` samples, `Events` gives per supplier: 40 records, **40 events, 0 invalid, 0 unmapped, 0 dups** (derived with a script over the JSON, then written into `TestEventsOverTheRealSamples`). `A48011` has `event_time` `2026-10-12T13:37:43Z`, `operator` `rio-sppo`, `feed` `its:conecta`. Every `sonda` event has `mode` `brt` and `route_id` null. Also asserted: null `line`/`route_id`/`heading` counts per sample (zirix 5/40, maxtrack 5/5, sonda 0/40 and one null heading). The first-40 samples hold no duplicates or invalid rows, so those paths are tested on copies of a real record with one field changed (`TestDuplicatesInvalidAndUnmapped`, `TestDuplicatesAcrossPagesAreDroppedByMinuteEvents`) |
| **E2** | Every event from E1 and every `feed-status` message produced in the Redpanda tests validates against its schema (`packages/contracts`). `TestGoTypesMatchSchemaFields` fails if a Go JSON field is missing from the schema or the reverse, and checks every property is `required`. The schema also rejects bad lat, mode, feed, vehicle, speed, heading and minute (`TestSchemaRejectsBadPositions`) |
| **E3** | Each sample (and the real 422 body) is written as an envelope and read back with `bytes.Equal` and a matching `body_len`. Invalid UTF-8, CRLF, a body containing the header separator and a 200 KB body also survive. A file cut inside its last member at five different points returns the earlier envelopes and `ErrTruncated`. Damage in the middle gives `ErrCorrupt` and the file is not touched |
| **E4** | `httptest` serving the captured `paged-p{0,1,2}`: **3 requests** (= the captured pages), each cursor equals the previous `next_cursor`, the query is exactly `minuto_utc`, `limit=5000` and `cursor`, the minute is complete. A new adapter over the same `RAW_DIR` makes **0 requests** |
| **E5** | Same envelope processed twice gives byte-identical events (`TestEventsIsDeterministic`). Against real Redpanda (testcontainers): 14 events published, 14 distinct identities, 4 feed-status messages. Restart with the checkpoint intact republishes nothing. Restart with the checkpoint **deleted** republishes from the raw log with **0 provider requests**: 28 records, still **14 distinct identities**, and the checkpoint is rewritten |
| **E6** | 503 then success: 4 requests, page 0 has **2 envelopes** (503, then 200), the minute is complete, and `feed-status` is `ok` on the real broker. The captured 422: **1 request, no retry**, the body stored as received, the minute missing, a `failed` status for **each of the 4 mapped feeds** on the real broker, none sent twice. A reset mid-body (promised 100,000 bytes, sent a few) is stored with its partial body and an error, then retried. Also tested: a 200 with an HTML body is retried; 5 attempts then give up (429 with `Retry-After: 0`); a rejected minute waits 5 minutes and a failed one 1 minute; the limiter spaces retries too |
| **E7** | One run, **60 minutes** (15:26:44–16:26:46 UTC), against the real API and the compose Redpanda. See below |
| **E8** | Adapter stopped 15:51:47, restarted 15:56:49 (5 min). See below |
| **E9** | Redpanda stopped 16:16:44–16:19:45 (3 min). See below |
| **E10** | `make lint`: **0 issues**. `make test`: exits 0, **0 skipped**, 67+ passing tests. Runtime of 3 runs: 16.70 s, 10.52 s, 17.25 s, **median 16.70 s** |
| **E11** | Docs in §4 updated (below). `grep -n "\[VERIFY" docs/tasks/TASK-its-adapter.md` lists only the word `[VERIFY]` in this document's own rules and results (§0, §4, §5 E11 and §7) and one real item: the MinIO/LocalStack licensing note in §2.7, **deferred to the AWS infra task**, which will test against a real S3 bucket instead |

### E7: 60-minute live run

- **Complete:** the run covers 118 consecutive minutes (14:27–16:24; the first 59 were the
  start-up backfill of the 1 h window and the last 59 were live). **118/118 are complete in
  the raw log and published.** Minute 15:25, fetched by the backfill, plus the 59 live minutes
  (15:26–16:24) make **60 consecutive minutes, 60/60**. No attempt failed: 303 requests, all
  HTTP 200, so no `failed` message was needed.
- **Rate:** the spacing of request starts (taken from `fetched_at − duration_ms`, accurate to
  about 1 ms) has a minimum of **0.992 s** and a median of 1.027 s, and no 0.99 s window held
  more than 1 request. That holds through the backfills too. Overall 0.085 requests/s.
- **Requests per minute:** median **3** (57 live minutes took 3 and 2 took 2; limit 4).
- **Volume (59 live minutes, medians):** 10,431 records/min, 10,414 events/min (about 174
  events/s), **367.4 KiB gzip/min** in the raw log against research's 367 KB: **+0.1%**,
  inside ±50%. **Topic bytes:** `positions.v1` is 43,044,212 bytes on disk (zstd) for 1,152,042
  records in 118 minutes, so **364,781 B/min** (356 KiB) and 37.4 B/record;
  `feed-status.v1` is 108,992 bytes. The raw log is 41 MB for the 118 minutes, and the complete
  15:00 hour is 22.06 MB (367.7 KB/min).
- **`lag_to_publish_ms`** (publish ack minus end of the minute), limit `ITS_MINUTE_LAG` + 30 s
  = 90 s:
  - the **51 undisturbed live minutes**: median 63.3 s, p95 about 64 s, max 64.8 s. **Met.**
  - all 59 live minutes: p95 230.6 s, because 8 minutes were deliberately disturbed (5 by E8:
    112, 188, 244, 299, 356 s; 3 by E9: 111, 171, 231 s). Those are the recovery times, not
    steady state.
- **Memory:** `ps` RSS at 10, 20 and 30 minutes: 12.4, 13.9, 15.3 MiB, **median 13.9 MiB**
  (limit 100). The highest every-minute sample was 54.8 MiB, during the start-up backfill. Since
  macOS `ps` can understate (memory compression), the peak was measured again with
  `/usr/bin/time -l` over a 150 s backfill: **maximum resident set 61.3 MiB** (64,258,048 B),
  peak footprint 52.4 MiB.

### E8: restart and backfill

- 5 minutes became due during the pause. **All 5 were fetched and published**, newest first
  (live priority), then oldest first, then back to live.
- **No minute has more than one complete page chain:** no page was fetched OK twice in the
  whole raw log.
- The checkpoint advanced past the gap and ended at `2026-10-12T16:24:00Z`.
- Request spacing after the restart: minimum **0.992 s** over 104 requests.
- On SIGINT the adapter stopped in 3 ms in the E8 stop, and in 0.13 s at the end of the run
  (limit 10 s).

### E9: broker down

- Redpanda was down 16:16:44–16:19:45. The raw log kept capturing: **the 3 minutes that fell
  due (16:15, 16:16, 16:17) were all fetched during the outage**, none missed.
- The publish cycle logged connection-refused warnings, rate-limited to one per 30 s.
- Three seconds after Redpanda was back, the checkpoint caught up and all 3 minutes were
  published (lags 231, 171, 111 s). The next minute was back to 63 s.

### Replay equals live, on real data

After the run, every event was recomputed from the raw log with `MinuteEvents` (the same code
a replay uses) and compared with `positions.v1`: **1,099,188 identities on each side, 0 missing
from the topic and 0 extra.** (A temporary test did this and isn't committed.)

### New facts about the provider

All in `docs/research/README.md` §1.1:

- **The provider repeats fixes across minutes.** The topic holds 1,152,042 records but
  1,099,188 distinct identities. All 52,854 extras (4.6%) are the same fix in a *different*
  minute's window (`sonda` 43,940, `zirix` 7,474, `maxtrack` 1,421, `conecta` 19), up to 70
  minutes for one parked bus. There were **0 same-minute repeats**, so none came from a
  re-publish. The consumer's `ON CONFLICT DO NOTHING` on `Identity()` is therefore required.
- `maxtrack` device `A2-096` reports two positions for one `datetime`: 57 conflicting
  duplicates in 118 minutes (the first is kept).
- `sonda` `(0, 0)` fixes: 1,035 of 103,180 records, dropped as invalid.
- `sonda` `direcao` is null on 19.8% and **above 360 (361–999) on 14.1%** of its records, so
  `heading_deg` is null for about a third of BRT fixes. They're nulled and counted, never
  corrected. Whether 906 means 186° is unverified.

### Resolved `[VERIFY]`s

- jsonschema: `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3 supports draft 2020-12 and was
  published 2026-06-28 (pkg.go.dev). Test dependency in practice: only test binaries import
  `packages/contracts`.
- franz-go v1.22.1 (`go doc`): the producer is idempotent with `acks=all` by default, but the
  **default compression is snappy, not zstd**, so `ProducerBatchCompression(ZstdCompression())`
  is set. `RecordDeliveryTimeout` is 30 s (the default is unlimited). `kadm` v1.19.0 creates
  the topics.
- `id_veiculo=<id>&limit=5` paginates: 5 + 5 + 4 over 3 pages.
- The 4xx sample is a real **HTTP 422**.

### Deviations from the plan, and why

1. **E7 ran 60 minutes in total, with E8 and E9 inside it, as a single run.** The plan's
   "medians of 3 runs" would have meant three hours of live traffic. The medians are over the
   minutes of one run. Stopping at 60 minutes was agreed with the user during the run (my
   driver had allowed 82).
2. **The lag measurement used four offsets, not three** (`+2m` added), because the documented
   60 s bound falls between `+90s` and `+3m`. Offsets are from the start of `M`.
3. **The raw header has a `cursor` field** (the request's cursor), which the plan didn't list.
   Chaining a page to the one before it needs it. Raw also has `Walk`, `Scan`, `Index` and
   `Loc`, so a startup scan streams headers (an hour is about 400 MB decompressed) and each
   page is read back by offset.
4. **Files are not strictly append-only:** `Log.Scan(..., repair=true)` cuts off an incomplete
   last member after a crash. Otherwise later appends land after the garbage and become
   unreadable. It only cuts a provably truncated tail, and a corrupt file is never touched.
5. **`Events` maps one envelope, and `MinuteEvents` maps all pages of a minute.** Duplicates
   can span pages, so the de-duplication that matters has to be per minute.
6. **A missing `providers_status` key counts as `provider_not_ok`** with a null
   `provider_status`. Zero vehicles with an `ok` status is `ok` with `records: 0`.
7. **The checkpoint waits for a minute that is incomplete**, so after a crash everything after
   it is re-published, including minutes that were already acked past a failed one. A minute
   missing for 23 h would make a restart re-publish up to that much. Gap reports are capped at
   7 days. The `failed` messages are queued in memory and not re-derived after a crash; the
   next failed or successful attempt publishes its own message.
8. **Retry timing:** a failed minute is retried after 60 s, and a minute that got a 4xx after
   5 minutes (the plan said "the next cycle").
9. **`make adapter-its` loads `.env`** with `set -a`, because `go run` doesn't.
10. **The run left topics on the local Redpanda.** I deleted `positions.v1` and
    `feed-status.v1` when I was done. They held only this run's data.

### Not tested, and open

- No real provider failure happened in the run (303 requests, all 200), so retries, `Retry-After`
  and truncated bodies are tested only against `httptest` serving captured bytes.
- Re-publish after a *partial* publish is tested through the deleted-checkpoint restart, not by
  killing the process in the middle of a minute.
- One 60-minute window. Day and night volumes, weekends and long outages aren't covered.
- Where the user's decision is needed: whether `sonda` headings above 360 should stay null.
