# TASK-replay-flake

> Written 2026-10-12. Small. Fixes the intermittent `TestConsumer/replay_equals_live` reported in
> `docs/tasks/TASK-live-map.md` §7 (B8). **Do this before `TASK-aws-infra.md`**: §4.1 commits only
> on green tests, and the deploy task commits several times.

## 1. Current scenario

- `go test ./apps/consumer/` fails `replay equals live` in about 2 of 7 runs.
- **The failure that was kept:**
  - Row: `rio-brt/its:sonda/803043`, `event_time` 2026-09-16.
  - The live table (path A) holds the copy from raw minute 14:37, and the rebuilt table (path B)
    holds the copy from 14:38.
  - The earliest-copy rule (architecture §10 #7) says 14:37 is right, so path B is the one that's
    wrong in that run.
- **The hypothesis (not verified yet):**
  - `0001_positions.sql` runs `add_retention_policy('positions', drop_after => '3 days')`.
    TimescaleDB schedules that job's first run right away (`total_runs 1` was seen just after a
    migration).
  - Path B's database is created and migrated by the `rebuild` binary itself
    (`apps/rebuild/main.go:78`), and the rebuild starts inserting immediately. If the first
    retention run starts *during* the rebuild, it drops the 2026-09-16 chunk, which is far older
    than 3 days.
  - The 14:37 copy is then gone, and the 14:38 copy arrives later in the rebuild and inserts
    into an empty slot.
  - Path A's database was migrated earlier (`consumer_test.go:127`), so its first run has
    already fired on an empty table.
- **It gets worse with time.** Every fixture is a 2026-10-12 capture. From about 2026-10-16
  onward, *every* fixture row is older than 3 days, so any test whose inserts overlap a
  retention run will lose rows. That covers the consumer, rebuild, store and api tests (each
  calls `store.Migrate`). Today only the one stale 2026-09-16 fix is exposed.

## 2. Planned changes

### 2.1 Confirm the cause first (no code change)

1. Run `go test -count=1 -run 'TestConsumer/replay_equals_live' ./apps/consumer/` 10 times, and
   record the pass/fail count.
2. In a failing run, record, before the database goes away, `timescaledb_information.job_stats`
   for the `policy_retention` job on `consumer_replay_b`: `last_run_started_at`,
   `last_run_status`, `total_runs`. Do this with a temporary `t.Log` in the test that is never
   committed.
   - **Confirmed** if `last_run_started_at` falls between the rebuild's start and end.
   - **If it doesn't**, stop. The earliest-copy rule then has a real ordering bug in
     `store.WritePositions` or in `apps/rebuild`. That's a different task: write down what was
     found here and come back to planning.

### 2.2 The fix: tests own the retention job's timing

Production keeps the policy exactly as it is. Tests make the job's first run happen *before* any
row is written, then switch scheduling off.

- **New `testharness.SettleRetention(t, connString)`** in `packages/testharness/harness.go`:
  1. Poll `timescaledb_information.job_stats` (joined to `jobs` on `proc_name = 'policy_retention'`
     and `hypertable_name = 'positions'`) until `total_runs >= 1` and the job isn't running.
     Every 100 ms, with a 30 s timeout that fails the test.
  2. Then `SELECT alter_job(job_id, scheduled => false)` for that job.
  3. Fail the test if no such job exists, so the helper can't silently do nothing.
  - **Why wait and then pause, instead of pausing straight away:** `alter_job` doesn't stop a
    run that has already started. Waiting for the first run (on an empty table, so it drops
    nothing) closes the race.
  - `[VERIFY: TimescaleDB 2.30 docs for alter_job(scheduled => false) and the job_stats columns
    (total_runs, job_status / last_run_status)]`
  - testharness doesn't import `packages/store`; it's plain SQL on a connection string.
- **Call it right after every `store.Migrate` in a test that writes positions:**
  `apps/consumer/consumer_test.go:127`, `apps/rebuild/rebuild_test.go:82`,
  `apps/api/api_test.go:242` and `:612`, and `packages/store/store_test.go:72`. Leave
  `store_test.go:91` and `:107` alone: they test Migrate itself (concurrency, re-running).
- **Path B in `replay equals live`:** before it runs the `rebuild` binary, the test migrates
  `dsnB` itself (`store.Migrate`, which goose makes a no-op when repeated), calls
  `SettleRetention`, and only then runs the binary. The binary's own Migrate then does nothing.
- **Unchanged:** the store test that checks the policy exists with `drop_after = 3 days`
  (`store_test.go:124`). The migration, the consumer, rebuild and the API are untouched.

### 2.3 Alternatives rejected

- **Drop the retention policy from tests' databases.** That hides whether production's migration
  still creates it, and it means a test-only migration path.
- **Filter rows older than N days out of `sameTables`.** That hides a real difference instead of
  removing the race, and it gets wrong as the fixtures age.
- **Have the consumer or rebuild refuse fixes older than the retention window.** It might be a
  good rule (a 26-day-old fix in a live poll is odd), but it's a product decision about stale
  data, not a test fix. It's noted in §3 for later.

## 3. Why

- `make test` must be deterministic before the AWS task, which commits several times.
- The race would turn into a near-certain failure across four packages around 2026-10-16, when the
  fixtures pass 3 days old.
- **Cost:** one helper (~30 lines) and six call sites. No production code changes.
- **What it teaches about production** (record it in architecture §5 or §8): "replay = live" holds
  only for rows inside the retention window. Rebuilding minutes older than 3 days into the live
  database is pointless for `positions`, because the next retention run drops them. Rebuilding
  old ranges matters for the aggregates, which are kept forever. The odd old fix (`803043`,
  2026-09-16 in a 2026-10-12 poll) is a stale device report. Whether to ingest those is left to
  the delays-by-line task.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `packages/testharness/harness.go` | edit | `SettleRetention` |
| `packages/testharness/harness_test.go` | edit | it waits, then pauses; it fails when there's no job |
| `apps/consumer/consumer_test.go` | edit | call after Migrate; path B migrates and settles before running the binary |
| `apps/rebuild/rebuild_test.go` | edit | call after Migrate |
| `apps/api/api_test.go` | edit | call after both Migrates |
| `packages/store/store_test.go` | edit | call after the line-72 Migrate only |
| `docs/tasks/TASK-live-map.md` | edit | §7 B8: point to this task as the fix |
| `docs/architecture.md` | edit | one sentence: replay = live within the retention window |

## 5. Verification

- **F1:** §2.1 recorded here: the pass/fail count before the fix, and the `job_stats` timestamps
  from one failing run.
- **F2:** after the fix, `go test -count=1 -run 'TestConsumer/replay_equals_live' ./apps/consumer/`
  passes **20 of 20** runs.
- **F3:** `go test -count=1 ./...` passes **3 of 3** runs, with 0 skipped.
- **F4:** to show the fix holds once fixtures are old, temporarily change the migration's
  `drop_after` to `'1 second'` in a local, uncommitted edit. Then run the consumer, rebuild and
  api packages: they pass 3 of 3, because the first run lands on an empty table and the job is
  then paused. Revert the edit, and record here that it was reverted.
- `make lint` is clean. Commit (no `Co-Authored-By`).

## 6. Results

Executed 2026-10-12.

**F1: the cause is confirmed.**

- Before the fix, `go test -count=1 -run 'TestConsumer/replay_equals_live' ./apps/consumer/` failed
  **2 of 10** runs. Both failures: `positions: 174 rows vs 172` (the rebuilt table lost 2 rows; a
  different signature from the kept `803043` failure, same family).
- A temporary `t.Log` of `job_stats` (never committed, reverted) over 14 more runs caught 1 failure.
  Database B's first retention run started at 17:19:17.989 (-03) = 20:19:17.989 UTC, and the rebuild
  ran 20:19:17.029 .. 20:19:18.004 UTC: the run started **0.96 s into the rebuild**. Database A's
  job had run about 5 s before its inserts.
- In the 13 passing runs, B's job had not run when the rebuild finished (`total_runs` was **NULL**,
  not 0). The first run's timing is up to the scheduler, so the race is real and independent of the
  rebuild, which only takes about 0.5 s.
- The first retention run fires between 20 ms and about 2 s after the policy is created (3 samples
  from `TestSettleRetention`).

**What changed**

- `[VERIFIED: tigerdata docs, jobs-automation/alter_job and informational-views/job_stats]`
  `alter_job(job_id, scheduled => false)` exists and is documented only as excluding the job from
  background runs; it says nothing about a run already in progress, so the helper waits for the first
  run first. `job_stats` has `total_runs`, `job_status` (`Running`/`Scheduled`/`Paused`). The docs
  don't say `total_runs` is NULL before the first run; observed on 2.30.2, so the helper treats NULL as
  "not yet".
- `testharness.SettleRetention` as planned, plus `TestSettleRetention` (waits then pauses; fails with
  "no retention job" on a database that isn't migrated).
- Call sites: `consumer_test.go` sandbox, `rebuild_test.go`, `api_test.go` (both), and the `migrated`
  helper of `store_test.go` (so every store subtest that writes rows is covered; the Migrate-itself
  tests were left alone). Path B in `replay equals live` migrates and settles `dsnB` before running the
  binary.
- **Cost:** `replay equals live` went from about 6.6 s to about 10 s, because each new database waits
  for its first retention run.

**Verification**

- **F2:** 20 of 20 passed (was 8 of 10 before).
- **F3:** `go test -count=1 -v ./...` passed 3 of 3, 0 skipped, 0 failed. Consumer package: 86 s, 91 s,
  88 s.
- **F4:** with `drop_after` temporarily set to `'1 second'` in `0001_positions.sql`, consumer, rebuild
  and api passed 3 of 3. Not run for `packages/store`, whose test asserts `3 days`. **The edit was
  reverted** (`git status` clean for the migration).
- `make lint`: 0 issues.
