# TASK: The positions consumer, the schema, and rebuild from the raw log

**Status (2026-10-12): done** (results in §7). Written for a Sonnet session to execute end to
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
  `[resolved, §7: v3.29.0; the Provider API and the Postgres session locker exist, and two
  concurrent Migrate calls both succeed]`. `store.Migrate(ctx, pool)` runs on consumer and rebuild
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
    `[resolved, §7: the provider sends fractional values and float32 loses precision on 169,184
    `direcao` values, so both columns are double precision]`.
  - It becomes a hypertable on `event_time` with **1-day chunks**, and gets a **3-day retention
    policy** (architecture §8) `[resolved, §7: the docs now recommend CREATE TABLE … WITH (tsdb.hypertable …) and call
    create_hypertable() legacy; add_retention_policy is unchanged]`. Retention drops whole chunks, so up to 4 days
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
    after each batch `[resolved, §7: PollRecords, CommitRecords, AllowRebalance as documented]`;
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
  `embed.FS` with less glue `[resolved, §7: goose chosen; golang-migrate not evaluated further]`.
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
    the mapped port can change on restart `[resolved, §7: it does]`, so connect through a DSN read after
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

**Status (2026-10-12): done.** All ten criteria are met. E7's footprint hit the stop rule and the
user decided (below). E8 is the short smoke check; the sustained live run is deferred to the AWS
infra task, as agreed.

### Step 1: what the 118-minute raw log says (recorded in research §1.1)

A throwaway script over `var/runs/20261012-its-adapter-raw/` (SHA-256 sums checked first: all
four files OK) re-derived the identities from the raw bodies and got the adapter's own numbers:
1,152,239 valid records, **1,099,188 distinct identities, 52,854 repeats**.

- **Repeats are almost always identical.** **11** of the 52,854 copies differ from the first copy
  (lat and lon on all 11, `velocidade` on 9, `direcao` on 8, `servico` and `route_id` on 11), all
  `maxtrack`. So the kept copy decides the stored position, not only `raw_ref`: §2.1 #1 matters.
  Gaps between a repeat and its first copy are 1 to 69 minutes.
- **`event_time` minus the start of its raw minute:** median +21 s, p95 +53 s, maximum **+90 s**.
  None is after the response's `fetched_at`: nothing is in the future.
- **216 identities (0.02%) are more than 24 h older than their minute**, the oldest by 299 days
  (`sonda` 215, `zirix` 1); 97 are older than 3 days. Stored as they are, they land in 55
  one-day chunks. **Stop rule hit; the user chose to store them as they are** (2026-10-12). The
  54 extra chunks total 1.7 MB.
- **`direcao` and `velocidade` are not whole numbers:** 169,184 `direcao` and 4,030 `velocidade`
  values do not round-trip as float32 (e.g. `111.77`). So `speed_kmh` and `heading_deg` are
  **double precision**, not `real`.

### Criteria

| | Result |
|---|---|
| **E1** | `Migrate` on an empty database creates both tables. Run again, `goose_db_version` stays at 3 rows (version 0 and the two migrations). Two concurrent `Migrate` calls on a fresh database both succeed. Through `timescaledb_information`: `positions` is a hypertable on `event_time` with **1 day** chunks, has a `policy_retention` job with `drop_after` **3 days** (schedule 1 day), `compression_enabled` is false, `feed_status` is a plain table, and `speed_kmh`/`heading_deg` are `double precision` (`TestStore/migrate`) |
| **E2** | Events from the four ITS samples through `its.MinuteEvents`, for two raw minutes (so every identity repeats), plus their 8 statuses, produced to real Redpanda and consumed into real TimescaleDB: **160 rows = 160 distinct `Identity()`**, 8 `feed_status` rows. Produced **a second time**: the row counts and the md5 over every column of both tables are identical |
| **E3** | Store level (`TestStore/order independence`) and end to end (E4). The same two minutes are written in minute order into database A and in reverse into database B, in the same container. The 160 identities all repeat across the two minutes. A writes 160 rows then 0; B writes 160 then 160 (the earlier copy replaces the later one). Both tables read back **equal on every column** (`reflect.DeepEqual` on 160 position rows and 8 status rows, and equal md5), and every kept row has `raw_minute` = the earlier minute in both. Mutation check: with the `WHERE excluded.raw_minute < positions.raw_minute` clause replaced by `true`, the idempotency test (160 rows rewritten) and this test both fail |
| **E4** | **Test:** a raw log of captured bytes with five minutes: the real three-page chain, two four-page minutes built from the four supplier samples (same bodies, so every identity repeats), one incomplete minute (page 0 plus the real 422), and one absent minute. Path A publishes the newest minute first, `failed` then `gap` for the incomplete one, and `gap` for the absent one, through the consumer. Path B runs the **real `rebuild` binary** (built by the test). The tables hold 174 positions and 20 statuses and are **equal on every column**. **Live data:** see E8: 612,048 rows and 264 statuses, identical md5 |
| **E5** | **(a)** Batch size 40; the `afterDBCommit` hook cancels the consumer after the first batch. The database held **40 of 160 rows** and the group had **no committed offsets**. A second consumer in the same group drained the topics: **160 rows, 4 statuses, none missing** (every event of the minute is found in the table by identity). **(b)** The TimescaleDB container was stopped for 20 s mid-drain. A retry was logged, the group's committed offsets did **not move** during the outage (the consumer was still behind), and after `Start` the remaining rows arrived: 160 rows and 4 statuses. The test takes 39–41 s. **The mapped port changed on restart (55248 → 55249)**, which is why the consumer connects through `testharness.Proxy` |
| **E6** | Three broken messages among valid ones (not JSON; valid JSON with an unknown field; a status `"exploded"`): **all three skipped and counted** (`skipped_undecodable` 3 in the batch lines), each logged with topic, partition and offset (the offsets match the produced records), the logged bytes at most 200, and **every valid record after them written** (160 positions, 4 statuses) |
| **E7** | `make rebuild` over 14:27–16:24 into an empty database wrote **1,099,188 positions and 472 `feed_status` rows, all `ok`**. **Throughput:** 31,530, 34,085 and 31,215 rows/s on fresh databases (34.9, 32.2, 35.2 s): **median 31,530 rows/s** (limit 5,000), so a 23 h backlog of ≈ 12.9 M rows is about 7 min. **Footprint:** `hypertable_size('positions')` = 317,612,032 B = **289 B/row** (205 MB table + 96 MB primary-key index; the day's main chunk alone is 287 B/row). **4 days × 13.4 M rows = 14.4 GiB > 14 GiB: stop rule hit, the user was asked.** Options measured: a slim schema (smallint codes, no `raw_key`) 203 B/row ≈ 10.1 GiB; 2-day retention ≈ 10.8 GiB; a 30 GiB volume. **Decision (user, 2026-10-12): keep the schema, size the volume at 30 GiB** in the AWS task. A second `rebuild` into the same database wrote **0 rows and 0 statuses** and both md5 checksums were unchanged |
| **E8** | Fresh `transit_pulse` database on the compose stack. The adapter ran with `ITS_BACKFILL_WINDOW=1h` (a 23 h backfill is 70 min): it backfilled 17:21–18:21, caught up at about 18:24, and the **5 live minutes 18:22–18:26** were published 62.7–63.8 s after each minute ended. Every one of them has its position rows (10,157 / 10,123 / 10,026 / 10,052 / 9,769 with that `raw_minute`) and **4 `feed_status` rows**. All 66 minutes (17:21–18:26) have 4 rows, **all `ok`**: 612,048 positions, 264 statuses (the adapter counted 628,017 events; the 15,969 difference, 2.5%, are repeats). **Then `make rebuild` over the same 66 minutes into a second database** wrote 612,048 rows and 264 statuses in 19.2 s (31,919 rows/s), and the md5 over every column of both tables is **identical on both databases**. **As observed, not pass/fail:** the consumer's `oldest_record_age_ms` was 30–500 ms on live batches (median over all 205 batches including the backfill 194 ms, maximum 4,781 ms on one backfill batch), with no warning or error in its log. Its peak RSS over a second 202 s run (a restart from the committed offsets, 5 more live minutes) was **56.8 MB** (`/usr/bin/time -l` maximum resident set size; peak footprint 47.3 MB). SIGINT stopped the consumer and the adapter with exit 0 in 0.15 s each. `make consumer` and `make rebuild` were both run |
| **E9** | `make lint`: **0 issues** (gofmt, vet, golangci-lint, ESLint). `go test -count=1 ./...`: exit 0, **0 skipped**, 84 passing tests and subtests. Runtime of 3 runs: 89.1 s, 80.7 s, 89.6 s, **median 89.1 s** |
| **E10** | Docs in §4 updated (below). `grep -n "\[VERIFY" docs/tasks/TASK-positions-consumer.md` lists only this document's own rules and descriptions (§0, §3, E10 and the §7 headings). Every other `[VERIFY]` is marked `[resolved, §7: …]` |

### Resolved `[VERIFY]`s

- **goose:** `github.com/pressly/goose/v3` v3.29.0 (`go list -m -versions`). `goose.NewProvider`
  with `goose.DialectPostgres`, an `fs.Sub` of the embedded `migrations/`, and
  `goose.WithSessionLocker(lock.NewPostgresSessionLocker())`. The `*sql.DB` comes from
  `stdlib.OpenDBFromPool`; closing it does not close the pool (pgx docs). golang-migrate was not
  evaluated further.
- **TimescaleDB 2.30.2** ([`create_table`](https://www.tigerdata.com/docs/api/latest/hypertable/create_table),
  [`create_hypertable`](https://www.tigerdata.com/docs/api/latest/hypertable/create_hypertable),
  [`add_retention_policy`](https://www.tigerdata.com/docs/api/latest/data-retention/add_retention_policy)):
  `create_hypertable()` is now documented as legacy and `CREATE TABLE … WITH (tsdb.hypertable,
  tsdb.partition_column, tsdb.chunk_interval)` is the recommended form, but **its default is
  `tsdb.columnstore = true`**, so the migration sets `tsdb.columnstore = false` explicitly (§2.1
  #2). Both forms were tried on the 2.30.2 server and gave the same 1-day dimension and no
  compression. `add_retention_policy('positions', drop_after => INTERVAL '3 days')` creates a
  job with a 1-day schedule. A primary key that includes `event_time` was accepted.
- **franz-go v1.22.1** (`go doc`): `PollRecords`, `DisableAutoCommit`, `BlockRebalanceOnPoll`,
  `AllowRebalance` and `CommitRecords` work as the plan says. `BlockRebalanceOnPoll` blocks on
  **any non-empty poll result, including one that only carries an error**, so `AllowRebalance` is
  called after every poll (see the first deviation below).
- **float32:** not whole numbers (Step 1), so `double precision`.
- **Container restart:** the mapped port changes (55248 → 55249).

### Deviations from the plan, and why

1. **`AllowRebalance` after every poll.** The first version returned on a cancelled context
   before calling it, and `Close()` (leave group) then hung for the whole test timeout. The loop
   now calls it on every path out of a poll.
2. **`speed_kmh` and `heading_deg` are `double precision`**, not `real` (Step 1).
3. **The hypertable uses `CREATE TABLE … WITH (tsdb.hypertable …)`**, not `create_hypertable`
   (legacy in the current docs), with `tsdb.columnstore = false` and
   **`tsdb.create_default_indexes = false`**: no query alone by time exists yet, and the primary
   key is the only index, which keeps bytes/row down. The API task adds an index if a query needs one.
4. **The E4 test lives in `apps/consumer`** and runs the real `rebuild` binary, because `Rebuild`
   is in `package main` of `apps/rebuild` and the consumer's is in another. The rebuild also has
   its own test (`TestRebuildFromTheRawLog`, built from the real paged chain and the real 422).
5. **E5(b) uses `testharness.Proxy`** (a small TCP forwarder with a fixed address that looks up the
   container's current port on each dial) because the port moves on restart. The container is
   really stopped and started; only the consumer's address is stable.
6. **E8 ran with `ITS_BACKFILL_WINDOW=1h`**, so start-up took about 4 min instead of 70, and the
   5 live minutes were counted from 18:22 (my first wait script also counted start-up minutes as
   live and was corrected).
7. **Extra exports in `packages/store`:** `Querier`, `ReadPositions`, `ReadFeedStatus` and
   `Checksum`, used by the tests and by the comparisons in E7 and E8. The feed-status write ranks
   statuses (complete chain > gap > failed; the latest wins among equals) and skips a write that
   would change nothing.
8. **The consumer rejects more than invalid JSON.** A record that is valid JSON but could never be
   written (an empty required text, an unparseable minute, a NUL byte, a status outside the four
   contract values) is counted as undecodable and skipped. Otherwise one such record would make
   every attempt to write its batch fail and block the consumer for good. A database error that
   is not about connectivity is still retried forever, by design: it never skips a batch because
   the database failed, and logs at most once per 30 s.
9. **A stop signal between the transaction commit and the offset commit** leaves that batch's
   offsets uncommitted (the commit uses the cancelled context), so the batch is delivered again on
   the next start. It is the same case as a crash and harmless. The transaction itself is allowed
   8 s to finish after the signal.
10. **`rebuild` indexes one hour file at a time** and, if a file ends in a truncated member, uses
    the complete ones and says so; it never repairs or otherwise modifies the raw log.
11. **`DATABASE_URL` has no default** in the consumer and the rebuild, so neither can write to the
    wrong database by accident. I added the line to my local `.env` (git-ignored); anyone with an
    older `.env` has to add it from `.env.example`.
12. **Other work in the tree.** `apps/web`, the `tiles` target in the `Makefile` and one
    `.gitignore` line belong to another session's uncommitted work (the basemap). I did not touch
    them and did not commit them.

### Not tested, and open

- **The sustained live run** (30 min: p95 `oldest_record_age_ms` ≤ 5 s, consumer RSS ≤ 100 MiB,
  TimescaleDB and Redpanda memory, no OOM kill) is **deferred to the AWS infra task**, by the
  user's decision. Re-measure rows/s and bytes/row on the t4g.small there.
- **Index growth under live arrival order.** The 289 B/row figure is a bulk load in key order.
  The 30 GiB volume is the margin for it.
- **The retention job has not been seen dropping a chunk**: its configuration is checked, but
  nothing waited 3 days. A rebuild of minutes whose `event_time` is older than 3 days creates
  chunks that retention drops at its next run.
- **A poison record that the checks in deviation 8 do not catch** would block its partition,
  with a retry log every 30 s. The raw log would still have the data.
- **Compression** is untested on purpose (§2.1 #2).
