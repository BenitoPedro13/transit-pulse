# TASK: The positions consumer, the schema, and rebuild from the raw log

**Status (2026-10-12): planned, not started.** Written for a Sonnet session to execute end to
end. The user approved the decisions in §2.1 on 2026-10-12. Read `CLAUDE.md` first, then `docs/tasks/TASK-its-adapter.md` §7 (the facts this task
builds on), then this document.

## 0. Rules for whoever executes this

- **Follow the order in §6.** Step 1 measures things that later steps depend on.
- **Check every library and TimescaleDB API against its own current docs before using it**
  (CLAUDE.md §2.0). Versions and function names here are what was known on 2026-10-12.
  Each one marked `[VERIFY]` is resolved in §7 with a link.
- **Tests run against real infrastructure and real samples** (CLAUDE.md "Tests"). Events in
  tests come from the captured samples in `docs/research/samples/` through the real mapping
  code. The one hand-built input allowed is the malformed topic message in E6, because it
  stands for a broken message, not a provider response.
- **Don't widen the scope.** There are no aggregates, no delays by line, no API, no map, no S3,
  no compression and no GTFS here (§2.8).
- **Stop and ask the user** if a measurement crosses a limit that says "stop" (§5 E7), or if
  TimescaleDB rejects something the design depends on (the conditional upsert in §2.4).
- When done: fill §7, update the docs listed in §4, run `make lint` and `make test`, and
  commit **without a `Co-Authored-By` trailer**. Don't push.

## 1. Current scenario

- `apps/adapter-its` writes every ITS response to the raw log (`packages/raw`, hourly gzip
  under `RAW_DIR`). It publishes `positions.v1` (keyed `operator/feed/vehicle`, 6 partitions,
  24 h retention) and `feed-status.v1` (keyed `feed/minute`, compacted).
- Delivery is at-least-once. A re-publish after a crash puts the same event on the topic
  twice (`packages/event`).
- **Nothing reads the topics.** TimescaleDB runs (`make up`) with `timescaledb` and `postgis`
  installed (`infra/postgres/initdb/001-extensions.sql`) but has no tables.
- Facts from the 118-minute live run (`TASK-its-adapter.md` §7, research §1.1) that shape
  this task:
  - **The provider re-serves the same fix in later minutes.** 52,854 of 1,152,042 records
    (4.6%) repeat an identity from an earlier minute, up to 70 minutes later. So the database
    has to de-duplicate on `Identity()` (architecture §10 #7).
  - A repeat lives in a different minute, so its `raw_ref` and `received_time` differ from
    the first copy. Whether its position or speed can also differ is not measured yet
    (§2.4, §6 step 1).
  - **Arrival order is not minute order.** After a restart the adapter publishes the newest
    minute first, then the backlog oldest-first (adapter task E8). A rebuild from the raw log reads in
    minute order.
  - Volume: 1,099,188 distinct identities in 118 minutes, which is **9,315 rows/min, about
    13.4 M rows/day** after de-duplication. During the run the adapter published about 174
    events/s.
- `mapping.go` (`Events`, `MinuteEvents`) lives in `package main` of `apps/adapter-its`, so
  no other binary or test can turn raw bytes into events. CLAUDE.md requires a replay test
  ("rebuild a projection from the raw log and assert it equals the live projection"), and
  that test needs it.

## 2. Planned changes

### 2.1 Decisions this task implements (approved by the user, 2026-10-12)

1. **Earliest raw minute wins on a repeat (refines §10 #7).** §10 #7 says
   `ON CONFLICT DO NOTHING`. With repeats across minutes and the adapter's newest-first
   recovery, `DO NOTHING` keeps whichever copy *arrived* first. So a live table and a rebuilt
   table would disagree on `raw_ref` and `received_time` for the repeated 4.6%, and the
   replay test could never pass on all columns. **Proposal:** keep one row per identity
   (unchanged), but make the kept copy the one from the **earliest raw minute**:
   `ON CONFLICT (…) DO UPDATE SET … WHERE excluded.raw_minute < positions.raw_minute`. The
   result then doesn't depend on order: replaying the same events in any order gives the same
   table. The cost is an occasional update instead of a skipped insert. The alternative is to
   keep `DO NOTHING` and leave `raw_ref`/`received_time` out of the replay comparison, which I
   reject: a weaker test hides real drift.
2. **No compression in this task.** renewable-pulse found that `INSERT … ON CONFLICT` on
   compressed chunks is unreliable (`renewable-pulse/docs/tasks/TASK-historical-backfill.md`
   §2.6, open TimescaleDB issues as of 2026-08-27). Repeats arrive up to 70 minutes late and
   the adapter backfills up to 23 h, so this table would hit that path. This task **measures**
   the uncompressed footprint instead (E7). If it doesn't fit, the executor stops and asks.
3. **Sonda headings above 360 stay null** (the adapter's current behaviour). The consumer
   stores `heading_deg` exactly as the event carries it, so this task works whatever you
   decide later. A change would go in `packages/its` and the contract, not here.

### 2.2 `packages/its/`: the ITS mapping, moved out of `package main`

- Move `apps/adapter-its/mapping.go` and `mapping_test.go` to `packages/its` unchanged apart
  from the package name and the exported names the adapter needs (`Events`, `MinuteEvents`,
  `Stats`, `FeedStats`, `MappedFeeds`). It's a move, not a rewrite: `git mv`, then fix the
  imports. E1 of the adapter task must still pass.
- Also move the adapter's status builders, so the adapter, the rebuild and the tests derive
  statuses one way:
  - `minuteStatuses(minute, stats, rawKeys)` (`apps/adapter-its/publish.go`, ~line 290) is
    already pure. Move it as `its.MinuteStatuses`. A complete chain gives `ok`, or
    `provider_not_ok` when the provider's status for that feed is absent or not `"ok"`.
  - `statusesFor(minute, status)` (`schedule.go`, ~line 140) reads `a.idx` under a lock.
    Split it: the adapter keeps the locked part that collects the raw keys of every attempt,
    and the pure part moves as `its.EmptyStatuses(minute, status, rawKeys)`, one message per
    mapped feed with zero counts.
  - `feedOperators` and `mappedFeeds` move with them.
- **The rebuild's rule for a minute** (§2.5) reuses these. A complete chain gives
  `MinuteStatuses`. No complete chain gives `EmptyStatuses(…, gap)`, with the raw keys of any
  attempts. A rebuild runs over a closed past range, so an incomplete minute is final. That's
  what the adapter reports as `gap` once the minute leaves its backfill window. Live, the
  same minute first gets `failed` and later `gap`. Latest-wins (§2.3) ends on `gap` too, so
  the two tables agree.
- The adapter imports `packages/its`. Its behaviour doesn't change: its existing tests must
  pass unedited apart from imports.

### 2.3 `packages/store/`: the schema and the two writes

One package owns the database shape, so the consumer and the rebuild can't drift apart.

- **Migrations:** SQL files in `packages/store/migrations/`, embedded with `embed.FS`, applied
  by **goose v3** (`github.com/pressly/goose/v3`) through its `Provider` API on a pgx pool,
  `[VERIFY: current version, the Provider API, and that its Postgres session locker stops two
  processes from migrating at once]`. `store.Migrate(ctx, pool)` runs on consumer and rebuild
  start-up. The extensions stay in `infra/postgres/initdb`, as now.
- **`0001_positions.sql`:**

  ```sql
  CREATE TABLE positions (
    operator      text             NOT NULL,
    feed          text             NOT NULL,
    vehicle       text             NOT NULL,
    event_time    timestamptz      NOT NULL,
    city          text             NOT NULL,
    mode          text             NOT NULL,
    authority     text             NOT NULL,
    source_kind   text             NOT NULL,
    line          text,
    route_id      text,
    lat           double precision NOT NULL,
    lon           double precision NOT NULL,
    speed_kmh     real,
    heading_deg   real,
    received_time timestamptz      NOT NULL,
    raw_key       text             NOT NULL,
    raw_minute    timestamptz      NOT NULL,
    raw_page      integer          NOT NULL,
    PRIMARY KEY (operator, feed, vehicle, event_time)
  );
  ```

  - Every field of `position.v1` gets a column, with nulls kept as nulls (missing ≠ zero).
    `speed_kmh` and `heading_deg` are `real`, because the provider sends whole numbers
    `[VERIFY on the run's raw log that no value loses precision as float32; otherwise use
    double precision]`.
  - It becomes a hypertable on `event_time` with **1-day chunks**, and gets a **3-day retention
    policy** (architecture §8) `[VERIFY: the TimescaleDB 2.30 names, e.g. create_hypertable
    with by_range and add_retention_policy]`. Retention drops whole chunks, so up to 4 days
    are on disk at any time. E7 projects with 4.
  - The primary key includes `event_time`, which a unique index on a hypertable requires.
- **`0002_feed_status.sql`:** `feed_status (feed, minute_utc timestamptz, operator, status,
  records, events, invalid, unmapped, conflicting_duplicates, provider_status, raw_keys
  text[], PRIMARY KEY (feed, minute_utc))`.
  - It's a plain table, kept forever. At about 5,760 rows/day (4 feeds × 1,440 minutes) it is
    the coverage history that later tells "no data" from "no vehicles".
  - `status` gets a `CHECK` with the four contract values.
- **`store.WritePositions(ctx, tx, []event.Position) (written int64, err error)`:**
  - De-duplicates the batch in Go first, keeping the earliest `raw_minute` per identity. One
    statement must not touch a row twice, or `DO UPDATE` fails.
  - Inserts with one `INSERT … SELECT FROM unnest($1::text[], …)` per 5,000 rows.
    `CopyFrom` can't express `ON CONFLICT`.
  - Applies the §2.1 #1 rule on conflict: update all non-key columns only when
    `excluded.raw_minute < positions.raw_minute`.
- **`store.WriteFeedStatus(ctx, tx, []event.FeedStatus)`:** upsert on `(feed, minute_utc)`.
  The latest message wins, except that `failed`/`gap` never overwrite `ok`/`provider_not_ok`.
  A minute with a complete chain is final. This keeps the result order-independent too.

### 2.4 `apps/consumer/`: topics → database

- One binary and one consumer group, `transit-pulse-consumer`, reading `positions.v1` and
  `feed-status.v1`, with franz-go v1.22.1 (already in `go.mod`):
  - `kgo.DisableAutoCommit()`, with `kgo.BlockRebalanceOnPoll()` and `AllowRebalance()`
    after each batch `[VERIFY: franz-go's documented manual-commit pattern]`;
  - start at the earliest offset when the group has none. The topic holds ≤ 24 h.
- **The loop:**
  1. Poll.
  2. Decode each record strictly into `event.Position` / `event.FeedStatus` (unknown fields
     are an error).
  3. In **one transaction**, run `WritePositions` and `WriteFeedStatus`.
  4. Commit the transaction.
  5. Only then call `CommitRecords` for the polled records.

  A crash between steps 4 and 5 re-delivers the batch, and §2.3 makes that harmless.
- **A database error:** commit no offsets, keep the batch, retry with backoff from 1 s up to a
  30 s cap, and log at most once per 30 s (the adapter's pattern). It **never skips** a batch
  because the database failed.
- **An undecodable record:** log topic, partition, offset and the first 200 bytes, count it,
  and skip it. It's the only skip. The raw log is the source of truth, so `rebuild` recovers
  whatever the event would have been.
- **The consumer falls more than 24 h behind**, which loses topic data. The minutes are still
  in the raw log, and the fix is `make rebuild` for that range. The runbook line goes in the
  README. No automatic detection in this task.
- **Logging:** `log/slog` JSON. One line per committed batch: `records, positions, written,
  statuses, skipped_undecodable, oldest_record_age_ms` (commit time − Kafka record timestamp,
  which is E8's lag).
- **Config** (`config.go`, from the environment, like the adapter): `KAFKA_BROKERS` (exists)
  and `DATABASE_URL` (new). SIGINT/SIGTERM finish the batch in flight and exit 0 within 10 s.

### 2.5 `apps/rebuild/`: raw log → database

- `rebuild -from <RFC3339 minute> -to <RFC3339 minute>`, reading `RAW_DIR` and
  `DATABASE_URL`:
  - for each minute, oldest first, it picks the complete chain (`raw.Index.Chain`, the
    adapter's own rule);
  - maps it with `its.MinuteEvents` and derives its statuses (§2.2);
  - writes both with the same `store` functions, one transaction per minute.
- It is the "projections can be rebuilt" path (invariant 2), the recovery when the consumer falls more than 24 h behind (§2.4), and
  how the replay test gets its second table. It never fetches from a provider.
- `make rebuild FROM=… TO=…`. To compare against live, the target is a **second database**
  (`DATABASE_URL=…/transit_pulse_rebuild`). Rebuilding into the live database is allowed too,
  and changes nothing (E3).

### 2.6 Make, env, compose

- `Makefile`: `consumer` (loads `.env`, `go run ./apps/consumer`) and `rebuild`. No compose
  service for the consumer yet. That's the AWS infra task, as for the adapter.
- `.env.example`: `DATABASE_URL=postgres://transit_pulse:transit_pulse@127.0.0.1:5435/transit_pulse?sslmode=disable`.
- `packages/testharness`: gets a helper that creates a second database in the same container,
  if E4 needs one, instead of starting a second container.

### 2.7 Alternatives considered and rejected

- **`ON CONFLICT DO NOTHING`, as written in §10 #7:** order-dependent on the repeated 4.6%
  (§2.1 #1).
- **`CopyFrom` into a staging table, then `INSERT … SELECT … ON CONFLICT`:** two statements and
  a temp table per batch. `unnest` does it in one. Revisit only if E7's throughput misses.
- **A PostGIS `geometry` column now:** no query in this task needs it, and it adds bytes to the
  table whose size is the risk. The API or map task adds it when a spatial query exists.
- **Compression (columnstore) now:** §2.1 #2.
- **A hand-written migration runner** (renewable-pulse's `db.ts` style): goose is a maintained
  tool with locking, as CLAUDE.md §2 prefers. golang-migrate also works; goose embeds SQL from
  `embed.FS` with less glue `[VERIFY when choosing]`.
- **A dead-letter topic for undecodable records:** nothing reads one yet. A log line plus the
  raw log is enough.
- **Two binaries, one per topic:** two groups and two pools for 5,760 status rows a day. One
  transaction across both also keeps a minute's positions and status together.
- **A `vehicle_latest` table for the map:** that's the API task's read model.

### 2.8 Out of scope

Continuous aggregates and delays by line (need §10 #12), the API, the map, the AWS compose
file, S3 upload, compression, GTFS-RT, and any change to the adapter's behaviour.

## 3. Why

- It turns the published stream into a queryable store. The aggregates, the API, the map and
  the replay all sit on these two tables.
- It makes invariants 2, 3 and 6 testable end to end:
  - a table rebuilt from raw equals the live one;
  - the same response ingested twice changes nothing;
  - "failed" and "no vehicles" stay distinct rows.
- It answers architecture §8's open `[VERIFY]`: bytes per row, which decides whether 3 days of
  full detail fit on the box.
- **Cost:** two new binaries and one new dependency (goose). Locally, about 4 days × 13.4 M
  rows of disk, which E7 measures. The ITS mapping moves package, and the adapter's tests
  catch any break.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `packages/its/mapping.go`, `mapping_test.go` | moved from `apps/adapter-its` | package rename and exports only |
| `packages/its/status.go` | moved/extracted | the minute → `feed-status` rule (§2.2) |
| `apps/adapter-its/*.go` | edit | imports `packages/its` |
| `packages/store/store.go`, `migrations/0001_positions.sql`, `migrations/0002_feed_status.sql`, `store_test.go` | new | §2.3 |
| `apps/consumer/main.go`, `config.go`, `consume.go`, `consumer_test.go` | new | §2.4 |
| `apps/rebuild/main.go`, `rebuild_test.go` | new | §2.5 |
| `packages/testharness/harness.go` | edit, if needed | second database helper |
| `go.mod`, `go.sum` | edit | goose |
| `Makefile`, `.env.example` | edit | `consumer`, `rebuild`, `DATABASE_URL` |
| `docs/architecture.md` | edit | §10 #7 refinement, §8 bytes/row, §5 status |
| `docs/research/README.md` | edit | repeat-payload finding (§6 step 1), "Database load" measured |
| `CLAUDE.md` | edit | status line, stack table (store, goose), §4 tree, make targets |
| `README.md` | edit | status, `make consumer` / `make rebuild`, the >24 h runbook line |
| `docs/tasks/TASK-positions-consumer.md` | edit | §7 results |

## 5. Acceptance criteria

Each one is recorded in §7 with numbers. Timings are medians of 3 runs.

- **E1 Schema.**
  - `Migrate` on an empty testharness database creates both tables, and running it a second
    time is a no-op.
  - `positions` is a hypertable with 1-day chunks and a 3-day retention job, checked through
    `timescaledb_information`.
- **E2 Idempotency (real Redpanda + TimescaleDB).**
  - Events come from the captured ITS samples through `its.MinuteEvents` and are produced
    **twice** to `positions.v1`, with their statuses to `feed-status.v1`.
  - After the consumer drains: `count(*)` = distinct `Identity()` count.
  - A checksum of the whole table (an ordered `md5(string_agg(…))` over every column) is
    identical after the first and the second pass.
- **E3 Order independence.**
  - The same minutes are written in minute order into database A and in reverse order into
    database B.
  - Both tables are equal on **every column**: `EXCEPT` both ways returns 0 rows for
    `positions` and for `feed_status`.
  - It must include at least one identity that repeats across two minutes. The test first
    counts such identities in the samples it uses. If there are none, it appends a real
    captured envelope to the raw log twice: once under its own minute and once with only the
    header's `minute_utc` set to the next minute. The body bytes are unchanged, and a comment
    in the test says so. The test asserts that the kept row's `raw_minute` is the earlier one
    in both databases.
  - How to compare two databases: both live in the same testharness container (§2.6). The
    test reads each table with `SELECT * FROM … ORDER BY <primary key>` and compares the row
    slices with `reflect.DeepEqual`. It reports the first differing row. At sample sizes
    (hundreds of rows) that's simpler than cross-database SQL. E7/E8 use the same comparison
    as a throwaway script, chunked per hour.
- **E4 Replay = live (the CLAUDE.md test).** For the same raw envelopes:
  - path A, the live path: raw → events → topic → `apps/consumer` → database A;
  - path B, `apps/rebuild` → database B;
  - the tables are equal on every column, as in E3.
- **E5 At-least-once.**
  - **(a)** The consumer's batch handler takes an unexported `afterDBCommit func()` field,
    nil in production. The test sets it to cancel the consumer's context on the first call.
    The batch is in the database, but its offsets were never committed. A second consumer
    in the same group then drains the topic. The batch is re-delivered, the table checksum
    is unchanged, and no row is missing.
  - **(b)** The test stops the TimescaleDB container for 20 s mid-drain, then starts it again
    (`testcontainers` `Stop`/`Start`; the harness may need to return the container, and
    the mapped port can change on restart `[VERIFY]`, so connect through a DSN read after
    `Start`). The consumer logs retries, the group's committed offsets
    (`kadm` `FetchOffsets`) don't move during the outage, and every row arrives in the end.
- **E6 Undecodable record.** One malformed message among valid ones is skipped and counted,
  with its offset logged, and every valid record after it is written.
- **E7 Real data, offline: the 118-minute run** (`var/runs/20261012-its-adapter-raw/`). Check
  its SHA-256 sums first.
  - `make rebuild` over 14:27–16:24 into an empty database writes **1,099,188** position rows
    (the distinct identities measured in the adapter task) and **472** `feed_status` rows
    (118 × 4), all `ok`.
  - **Throughput:** rows written per second, median of 3 runs on a fresh database each time
    (each run is ~4 min at the target rate):
    **≥ 5,000 rows/s** on the Mac. At that rate a 23 h backlog (≈ 12.9 M rows) drains in
    ≤ 43 min. Re-measure on the t4g.small in the AWS task.
  - **Footprint:** report bytes/row as `hypertable_size('positions')` / rows (table plus
    index), and project **4 days × 13.4 M rows**.
    - **Stop and ask the user if the projection exceeds 14 GiB.** That's 70% of a 20 GiB data
      volume, renewable-pulse's size and disk alarm level, which this box copies.
    - The options to put to the user: slimmer columns (enums for the repeated text),
      compression with the `ON CONFLICT` caveat, shorter retention, or a bigger volume.
  - Run `rebuild` a second time into the same database: the checksum is unchanged (E2 on real
    data).
- **E8 Live smoke check, 5 minutes:** `make up`, `make adapter-its` and `make consumer` on a
  fresh database. Stop when 5 live minutes are in.
  - Every live minute has its position rows and 4 `feed_status` rows.
  - Then `make rebuild` over the same minutes into a second database. Rows whose `raw_minute`
    is in that range are **equal on every column** (E4 on live data).
  - The consumer's `oldest_record_age_ms` and RSS are recorded as observed. They're **not
    pass/fail here.**
  - **Deferred to the AWS infra task (user, 2026-10-12):** the sustained live run, with p95
    `oldest_record_age_ms` ≤ 5 s over 30 minutes, consumer RSS ≤ 100 MiB, and TimescaleDB and
    Redpanda memory below their limits with no OOM kill. Those limits are about the 2 GiB
    t4g.small, so they're measured there, not on the Mac. The user wants something on screen
    sooner, and a long local run doesn't answer the box question anyway.
- **E9** `make lint` reports 0 issues. `make test` exits 0 with 0 skipped, and its runtime is
  the median of 3 runs.
- **E10** The docs in §4 are updated. `grep -n "\[VERIFY" docs/tasks/TASK-positions-consumer.md`
  lists only items explicitly deferred in §7, each with its reason.

## 6. Execution order

1. **Measure on the run's raw log** with a throwaway script in the session scratchpad, which
   only reads `var/runs/…`:
   - for identities that repeat across minutes, how many have a different lat, lon, speed,
     heading, line or route between copies;
   - the distribution of `event_time − raw minute`: anything in the future or older than
     24 h?
   - whether any `velocidade`/`direcao` value loses precision as `real`.

   Record it in research §1.1. A future or day-old `event_time` affects chunking and
   retention, so stop and ask.
2. `packages/its` move. `make test` is green with the adapter unchanged in behaviour.
3. `packages/store`: migrations and writes (E1, store-level E3).
4. `apps/rebuild` (E7 offline: footprint and throughput, the stop rule).
5. `apps/consumer` (E2, E4, E5, E6).
6. Live smoke check E8, about 10 min with the start-up.
7. Docs (§4), §7, lint, tests, commit.

## 7. Results

_Not started._
