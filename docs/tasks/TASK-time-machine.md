# TASK-time-machine (D2)

> Written 2026-10-12. The second step of `docs/research/ux-direction.md` §3.2. **Prerequisite:
> `TASK-dashboard.md` (D1) is done.**
>
> What it adds:
> - a timeline along the bottom of the dashboard, whose track is a chart of vehicles per minute,
>   with failed minutes drawn as gaps;
> - scrubbing through the last hours;
> - play at 1×, 10× or 60×, with fading trails;
> - a way back to live.
>
> The hour files it serves are **the same format the static replay will use** (architecture §7
> #4), so the replay days later run on this screen unchanged.
>
> Read first: `apps/web/AGENTS.md` (Next 16), deck.gl 9.4 `TripsLayer` docs, Zustand's current
> docs (`zustand/vanilla`), and the global `~/.claude/CLAUDE.md` rules on Zustand.

## 1. Current scenario

- **The API keeps only the newest snapshot**, refreshed when `feed_status` moves
  (`apps/api/snapshot.go`). It holds the newest fix per vehicle within 300 s, with one vehicle
  shown once even if two feeds report it.
- **The database holds 3 days of `positions`** (no time index; a scan of the newest chunk took
  430 ms median locally) and every `feed_status` row (kept forever).
- **About 9,300 fixes a minute** reach the database after de-duplication: about 3.6 per vehicle
  per minute.
- **The recorded run** in `transit_pulse_run` is 118 minutes and ends at 13:24 (`TASK-live-map.md`
  §7). It's the test bed for this task.
- **D1 leaves room for this:** the bottom of the screen is free, and the selection state is in
  the URL.

## 2. Planned changes

### 2.1 The history format: `history-hour.v1` (a new contract)

One file per UTC hour. A JSON Schema at `packages/contracts/history-hour.v1.schema.json`, with a
validator test like the other contracts.

```jsonc
{
  "schema": "history-hour.v1",
  "hour_utc": "2026-10-12T13:00:00Z",
  "complete": false,                 // true once the hour is closed AND older than the backfill window (24 h)
  "sampling": "newest_fix_per_vehicle_per_minute",
  "vehicles": [                      // index table; fixes refer to it by position
    { "operator": "rio-sppo", "vehicle": "A29001", "mode": "bus", "line": "474" }
  ],
  "fixes": {                         // columnar, same length, ordered by (v, t)
    "v":   [0, 0, 0],                // index into vehicles
    "t":   [12, 71, 133],            // seconds since hour_utc (event_time), 0–3599
    "lat": [-2290123, …],            // degrees × 1e5, integers
    "lon": [-4321234, …],
    "spd": [184, -1, …]              // km/h × 10, integer; -1 = null (missing is not zero)
  },
  "minutes": [                       // exactly 60, one per minute of the hour
    { "vehicles": 2611, "feeds": { "ok": 3, "failed": 0, "provider_not_ok": 0, "gap": 0, "missing": 0 } }
  ]
}
```

- **Sampling:**
  - for each vehicle and each minute, the **newest real fix** in that minute (no interpolation);
  - the vehicle-per-feed choice is **the same rule the snapshot already uses**;
  - the screen says it's one position per minute per vehicle.
  - Why: all ~3.6 fixes a minute would triple the payload and add nothing visible at the zoom
    people play at.
- **`line` per vehicle** is its line in that hour (the newest one). If a vehicle changes line
  within the hour, it gets two index entries.
- **Per minute:**
  - `vehicles` is the number of distinct vehicles with a fix in that minute;
  - `feeds` counts feeds by their `feed_status` for that minute, and `missing` counts feeds with
    no row at all;
  - a minute where every feed `failed`, or where no row exists, is drawn as a gap in the chart,
    never as 0 vehicles (invariant 6).
- **Size target:** ≈ 2,600 vehicles × 60 ≈ 156 k fixes, **≤ 1.5 MB gzip per hour**. Measured in
  §6. If it's over, record the size and the share each column takes, then stop and plan.

### 2.2 API: history in memory, files precomputed

- **Configuration:** `HISTORY_HOURS` (default 3) sets how many hours are kept in memory.
- **On start,** one streaming query loads `positions` with `event_time` in
  `[newest minute − HISTORY_HOURS h, newest minute]`, ordered by `event_time`.
  - Rows are folded into the per-minute sample as they stream, so nothing holds 1.7 M rows.
  - The same is done for `feed_status` over that range.
  - Record the start time, median of 3.
- **On each refresh that moves the newest minute,** the new minute's sample is taken. A narrow
  query (`event_time` in that minute) is cheaper than diffing snapshots `[VERIFY: measure both;
  the query still scans the newest chunk, so time it]`.
  - **Late repeats and backfilled minutes** (up to 23 h late) update earlier minutes. To keep
    that cheap, re-query **only minutes whose `feed_status` row changed** (the existing probe
    already reads `feed_status`).
- **Precomputed bytes:**
  - each hour's file is built and gzipped once and kept in memory with its ETag;
  - only the current hour, and any hour a late minute touched, is rebuilt;
  - hours that fall out of the window are dropped.
- **Endpoints:**

| Endpoint | Body | Caching |
|---|---|---|
| `GET /v1/history` | `{ newest_minute, hours: ["2026-10-12T11:00:00Z", …], window_hours }` | per refresh, `public, max-age=10, s-maxage=10` |
| `GET /v1/history/{hour}` (`2026-10-12T13`) | a `history-hour.v1` file | ETag; `complete: true` → `public, max-age=31536000, immutable`; otherwise `public, max-age=30, s-maxage=30` |

  - An hour outside the window → 404 with a JSON body.
  - A request still never queries the database (the existing query counter proves it).
- **Memory budget:** API RSS **< 100 MB** with 3 h loaded (the AWS task's limit is 128m).
  Measured in §6.

### 2.3 Web: playback state in a Zustand store (the global rule's one real case)

The playhead changes every animation frame, and the map, the timeline and the letreiro all read
it without any other reason to be co-located. That is exactly the case the global CLAUDE.md
keeps Zustand for.

- `store/playback.ts`: `createStore` from `zustand/vanilla`. State:
  - `mode: 'live' | 'replay'`;
  - `playhead` (epoch ms);
  - `playing`;
  - `speed: 1 | 10 | 60`;
  - `smoothing: boolean` (default true).
- **The provider** creates the store per mount, inside `useState`. Never a module-level store.
- **Components use selector hooks only:** `usePlayhead()`, `usePlaybackControls()`,
  `usePlaybackMode()` in `hooks/use-playback.ts`. Nothing calls `useStore` directly.
- **The clock:** `usePlaybackClock()` runs a `requestAnimationFrame` loop with cleanup. It's a
  real Effect (the browser's frame clock), and it advances `playhead` by `Δreal × speed`.
  - Reaching the newest minute stops play and switches to `live`.
  - A hidden tab pauses it (`visibilitychange`).
- **Text that shows the playhead** (letreiro, slider `aria-valuetext`) subscribes through a
  selector **rounded to the minute**, so it re-renders once a minute of playback, not 60 times a
  second.
- **Data:**
  - `historyIndexOptions()` and `historyHourOptions(hour)` are `queryOptions` factories in
    `hooks/use-history.ts`;
  - a `complete` hour has `staleTime: Infinity`; the current hour 30 s;
  - the next hour is prefetched when the playhead is within 10 min of it.
- **URL:** `?t=2026-10-12T12:47Z` joins D1's parameters. Opening it lands in replay at that
  minute, paused.

### 2.4 Web: rendering

- **Trails:** `TripsLayer` (`@deck.gl/geo-layers`, the same 9.4 line; a new dependency).
  - Each vehicle's fixes become paths, **split wherever two consecutive fixes are more than
    300 s apart**. A gap is never bridged (invariant 1).
  - Timestamps are seconds since the start of the loaded window, to stay within float32
    precision `[VERIFY: TripsLayer docs on timestamp precision]`.
  - `trailLength` is 300 s, with fade.
- **Vehicle heads:**
  - **Smoothing on:** the position at `playhead` is interpolated between the two bracketing
    fixes of the same path segment, and drawn with D1's icons (shape = mode). A visible label
    near the timeline says "movimento suavizado entre posições reais (1 por minuto)".
  - **Smoothing off ("Só posições reais"):** each vehicle is drawn at its newest fix at or
    before `playhead`, if that fix is ≤ 300 s old (the snapshot's rule). There are no trails, and
    positions jump once a minute.
  - **The selected bus and line** behave as in D1: dimming, card, Seguir. The card reads the
    fix at the playhead and says "posição real de 12:46:31" or "≈ posição suavizada".
  - **Colour by speed** uses the fix's `spd`; null stays hollow.
- **Live mode** is D1, unchanged. Switching between live and replay swaps layers; it never
  mixes them.
- **Replay never looks live:**
  - the letreiro reads "REPLAY 12:47 · 2.598 ÔNIBUS";
  - the status strip reads "Reprodução de 12/10 12:47 — não é ao vivo";
  - the "AO VIVO" button is the way back.

### 2.5 Web: the timeline

```
┌────────────────────────────────────────────────────────────────────────────────┐
│ ▶  1× 10× 60×   ▁▂▃▅▆▇▇▇▇▆▇▇▇▇░░▇▇▇▇▇▇▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇|        AO VIVO │
│                 11:00          12:00 ░ falha na coleta    13:00   ↑12:47        │
└────────────────────────────────────────────────────────────────────────────────┘
```

- **Track:**
  - an SVG area of `minutes[].vehicles` across the loaded window;
  - gap minutes (§2.1) are hatched columns, explained in text in a legend ("░ falha na
    coleta");
  - chart choices (scale, colours, hatching) follow the `dataviz` skill.
- **Scrubber:** shadcn `slider` (Radix).
  - Keyboard: ←/→ ±1 min, PageUp/PageDown ±10 min, Home/End for the window's ends.
  - `aria-valuetext` "12:47, 2.598 ônibus".
  - Dragging pauses play.
- **Controls:**
  - play/pause is a button with an `aria-label` that names the action;
  - speed is a toggle group;
  - "Só posições reais" is a switch;
  - "AO VIVO" is a button, disabled while live.
- **Phone:** play and the scrubber are the top row of D1's bottom panel. The chart track shows
  when the panel is expanded.
- **Reduced motion:**
  - nothing ever plays by itself;
  - play advances one minute per tick at 1 tick/s, with no trails and no smoothing, until the
    user turns smoothing on themselves;
  - camera moves are `jumpTo`.
  - Play needs a click in every case, and pause is always one key away (WCAG 2.2.2).

### 2.6 Alternatives rejected

- **Sending every fix (~3.6 per vehicle per minute):** the payload is about 3× larger, for
  detail nobody sees at playback speed.
- **Pre-baked frames ("where everyone is at minute M"):** repeating a fix across minutes makes
  an old position look current. Fixes plus the 300 s rule keep that honest.
- **Querying the database per history request:** it breaks "readers never touch ingestion"
  (invariant 5).
- **React state, or TanStack Query, for the playhead:** that's 60 updates a second fanning out
  to every subscriber. It's the documented reason for Zustand.
- **Animating in live mode:** live stays a snapshot per refresh. Motion exists only in replay,
  where it's labelled.

## 3. Why

- **It's the "play" in "a dashboard people could play":** press play and watch Rio's morning
  happen in three minutes.
- **It shows the honesty layer in a way people can see:** failed minutes are holes in the chart,
  and a gap is never bridged.
- **It fixes the replay contract** (architecture §7 #4) before the replay task, so the static
  export later writes these same files.
- **Cost:** one contract, two endpoints, about 10 MB of API memory, and 2 web dependencies
  (`@deck.gl/geo-layers`, `zustand`). One to two Sonnet sessions; Part A (API + contract, §5
  T1–T5) can be its own session.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `packages/contracts/history-hour.v1.schema.json` + validator test | new | §2.1 |
| `apps/api/history.go` (+ test) | new | sampling, window, hour files |
| `apps/api/server.go`, `config.go`, `main.go` | edit | 2 endpoints, `HISTORY_HOURS` |
| `apps/web/store/playback.ts`, `components/playback-provider.tsx` | new | Zustand |
| `apps/web/hooks/use-playback.ts`, `use-history.ts` (+ tests) | new | selectors, clock, queries, pure rules (path splitting, interpolation, gap minutes) |
| `apps/web/components/timeline.tsx`, `timeline-chart.tsx` | new | |
| `apps/web/components/ui/slider.tsx`, `switch.tsx` | new | shadcn CLI |
| `apps/web/components/live-map-canvas.tsx`, `letreiro.tsx`, `dashboard.tsx`, `panel-onibus.tsx` | edit | replay layers, labels |
| `apps/web/package.json` | edit | `zustand`, `@deck.gl/geo-layers` |
| `.env.example` | edit | `HISTORY_HOURS` |
| `docs/architecture.md` | edit | §5 (history in the API), §7 #4 resolved by `history-hour.v1` |
| `CLAUDE.md`, `README.md` | edit | stack (Zustand, geo-layers), endpoints, status |

## 5. Verification

**Part A: API and contract** (Go, testcontainers, real recorded fixtures)

- **T1, contract:** every file the API serves validates against `history-hour.v1`. A test feeds
  one bad file (wrong `minutes` length) and the validator rejects it.
- **T2, sampling is real and exact:**
  - for 5 vehicles from the fixtures, the API's `(t, lat, lon, spd)` in an hour equals an
    independent SQL query: the newest `event_time` per `(vehicle, minute)` with the snapshot's
    feed choice;
  - every value matches, after the ×1e5 / ×10 rounding;
  - `spd` is −1 exactly where `speed_kmh IS NULL`.
- **T3, gaps:** a fixture minute whose `feed_status` is `failed` for every feed gives
  `feeds.failed = 3, vehicles = 0`, and the web rule (T7) marks it as a gap, not 0.
- **T4, late data:**
  - write a repeat with an earlier `raw_minute` for an old minute into the database;
  - after the next refresh, only that hour's ETag changes;
  - the other hours' bytes are identical.
- **T5, isolation and budgets:**
  - 1,000 history requests run 0 queries;
  - on `transit_pulse_run` (118 min, so 2–3 hour files), record:
    - API RSS (< 100 MB);
    - start-up load time;
    - each hour file's gzip size (≤ 1.5 MB);
  - medians of 3.

**Part B: screen**

- **T6:** on the recorded run, pressing play at 60× from the window's start reaches the newest
  minute in ≈ 118 s (± 5 s) and switches to live by itself.
- **T7:** Vitest on the pure rules, with input taken from a real hour file saved from T5:
  - path splitting at > 300 s;
  - interpolation inside a segment only;
  - "newest fix ≤ 300 s" for smoothing off;
  - gap minutes.
- **T8, honesty, checked on screen:**
  - pick a vehicle in the data with a gap of more than 5 min; its trail breaks there
    (screenshot);
  - the smoothing label is visible whenever smoothing is on;
  - "Só posições reais" shows heads only at real fixes;
  - the letreiro and the status strip say REPLAY in every replay state.
- **T9, accessibility:**
  - axe reports 0 violations in both schemes with the timeline shown;
  - keyboard-only scrub covers ±1, ±10, Home and End, and play/pause works by Space or Enter on
    the button;
  - with reduced motion, there are no trails, no smoothing and nothing plays by itself.
- **T10, performance:**
  - at 60× with trails and ~2,600 vehicles: median frame rate of 3 runs of 10 s each **≥ 30
    fps** on the Mac;
  - a scrub to a different hour shows the new minute within 1 s once the hour is cached;
  - record both.
- **T11:** screenshots at 1440 × 900 and 390 × 844, dark and light, while playing and paused,
  saved to `docs/tasks/assets/time-machine-*.png`.
- `make lint`, `make test` and `make web-build` pass. Commit per part (no `Co-Authored-By`).

## 6. Results

### Part A: API and contract

Done 2026-10-12. Files: `packages/contracts/history-hour.v1.schema.json`, `apps/api/history.go`,
`history_test.go`, edits to `server.go`, `config.go`, `main.go`, `api_test.go`, `.env.example`.

**Where the build differs from §2.1–2.2** (each is a decision the plan left open or got slightly wrong):

- **Whole UTC hours, not a rolling cut.** The window is the last `HISTORY_HOURS` hour slots ending
  with the current hour, so with 3 it holds 2 h 01 min to 3 h of data. A file is always a whole hour.
- **The newest minute is the newest every feed has a row for** (`min` over feeds of their newest
  `feed_status` minute), not the snapshot's `as_of_minute`. The four ITS feeds report some seconds
  apart; counting a minute only some had reported would draw a dip at the right edge that isn't
  real. Minutes after it are published as empty (`missing` = every feed).
- **`/v1/history` also sends `oldest_minute`** (the first minute in the window that has any
  `feed_status` row). The web app trims the lead-in before the recording started instead of
  drawing it as a gap.
- **The status read runs every tick, not only when the probe moves.** The probe sees only each
  feed's newest row, so a backfilled older minute would never trip it. The extra query reads the
  window's `feed_status` through its primary key (`feed = ANY(..)`, 472 rows on the run, 0.09 ms).
  A row that is new or changed (status or events) makes its minute dirty; the minute's fixes are
  re-read by `raw_minute` (the earliest raw minute, the consumer's rule), folded in, and the hours
  they land in are rebuilt. More than 20 dirty minutes reloads the window instead.
- **Fixes can be stamped up to 30 s past their raw minute's end** (1,954 of 1.1 M rows in the run),
  so the load and the merge do not clip at the newest minute; they sit in memory and are published
  when their minute is.
- **T3 says `failed = 3`; there are four feeds** (`its:conecta`, `maxtrack`, `sonda`, `zirix`), so the
  test asserts `failed = 4`.
- **A minute is a gap** (web rule, Part B) when `ok + provider_not_ok = 0`: no feed reported
  anything. A minute where only some feeds failed is a dip with its `feeds` counts, not a gap.

**Verification** (`go test ./apps/api`, real TimescaleDB, real sample positions)

| | Result |
|---|---|
| T1 | every served file validates; 5 bad files (59 minutes, `t = 3600`, `spd = -2`, unknown sampling, columns of different length) are rejected |
| T2 | hour 13 (the samples' 13:00–13:38 stretch) equals an independent SQL query (`row_number()` window, not the Go fold) on every `(operator, vehicle, line, t) → (lat, lon, spd)` after the rounding; `spd = -1` count equals the null-speed count (five vehicles' speeds are nulled in the test database, because the samples have none in that hour) |
| T3 | a minute with all four feeds `failed` reads `failed = 4`, 0 vehicles; a never-recorded minute reads `missing = 4`; minutes after the newest read `missing = 4` |
| T4 | a backfilled minute (the samples again, 1 h earlier) changes only its hour's ETag; the other two hours are byte-identical; the incremental history equals a fresh load byte for byte |
| T5 | 1,000 history requests run 0 queries; an idle refresh runs only the status read (3 refreshes, 3 queries) and leaves every ETag alone |
| mutation | reversing the tie rule fails T2 and T4; writing a null speed as 0 fails T2; skipping the dirty-minute merge fails T4 |

**Budgets on `transit_pulse_run`** (118 minutes, 1,098,615 positions in the window; macOS, local):

| | Median of 3 | Budget |
|---|---|---|
| start-up load (statuses + stream + fold + encode 3 hours) | 2.76 s (2.99, 2.76, 2.74) | – |
| peak RSS during the load | 79 MB (80, 79, 79) | < 100 MB |
| RSS 8 s after the load | 29–72 MB (macOS returns pages lazily) | – |
| hour 14 (33 min of data) | 427,591 B gzip, 2,583,933 B JSON | ≤ 1.5 MB |
| hour 15 (a full hour) | 796,754 B gzip, 4,661,550 B JSON | ≤ 1.5 MB |
| hour 16 (24 min of data) | 360,227 B gzip, 2,137,333 B JSON | ≤ 1.5 MB |
| merge query for one minute (`raw_minute` + `event_time` bounds), psql `EXPLAIN ANALYZE` | 35 ms (35.3, 35.6, 79.2 cold) | – |

The RSS peak is for 2.4 h of data; a full 3 h is about 1.25× the minutes, so expect the peak near
100 MB: measure it again in the 128 MB container when the AWS task lands. Two things keep the peak
down: `GOGC` is set to 20 during the load, and `debug.FreeOSMemory()` runs after it. The merge
query is a parallel sequential scan of the chunk, as `[VERIFY]` in §2.2 feared: 35 ms on this 1.1 M
row database, so roughly 0.4 s on a day's chunk of 13 M rows, once a minute.

### Part B: screen (not started; stopped 2026-10-12 at the user's usage limit)

State of the tree: `zustand` 5.0.15 and `@deck.gl/geo-layers` 9.4.0 are installed, and `components/ui/slider.tsx` and `switch.tsx`
are generated by the shadcn CLI. All of that is uncommitted and nothing else of Part B is written.

Decisions made while reading the code, to apply when resuming:

- **The shadcn slider and switch use Base UI attributes** (`data-checked`, `data-horizontal`); Radix emits `data-state` and
  `data-orientation`. Patch both, as was done for `toggle.tsx`. The slider also needs `aria-valuetext` on its thumb.
- **Shape of the work:** pure rules in `hooks/history-window.ts` (build one columnar window from the hour files, sorted by
  vehicle then time; segments split at > 300 s; heads at a playhead with and without smoothing; gap minute = `ok + provider_not_ok = 0`;
  trim the lead-in before `oldest_minute`), `store/playback.ts` (Zustand vanilla, store per mount), `hooks/use-playback.ts`,
  `hooks/use-history.ts` (`useQueries` with a module-level `combine` so the window is built once per response), then
  `timeline*.tsx`, then the map layers (`TripsLayer` in binary-attribute form; heads reuse the D1 `IconLayer`).
- **Frame-rate subscriptions stay in small leaf components** (the map scene and the scrubber). The dashboard, letreiro and status
  strip subscribe to the playhead rounded to the minute only.
- **Timeline placement** copies `ColorLegend`: a child of the panel, `lg:fixed` on desktop.
- **Test data for T7:** a trimmed real file from `transit_pulse_run` (hours 15 and 16 have 4,420 vehicles, 2,231 of them with a gap
  over 300 s, and 2,366 that change line). Keep about 25 vehicles, including a few of each. Hour 16 has 35 minutes with no
  `ok` feed, the lead-in before 14:27 is `missing = 4`, so both gap kinds are in real data.
- Deviation to record: all hours of the window are fetched when the timeline mounts (at most 3 files), so the "prefetch the next hour"
  rule is unnecessary.
