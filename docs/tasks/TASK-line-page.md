# TASK-line-page (U1)

> Written 2026-10-12. The second step of `docs/research/ux-direction.md` (decisions recorded in its
> §7). **Prerequisite: `TASK-ui-lines.md` (U0) is done**: the `/linha/[line]` page, the tokens and
> the hooks exist. This task fills the line page with what makes it useful:
>
> - the route as a strip of stops, each direction separately;
> - every live bus placed along it;
> - the time gap to the bus ahead;
> - the interval the city promises.
>
> It runs on the recorded local data. It doesn't need the AWS deploy and can run in parallel with
> it (decision 4); the overlapping files are listed in §4.
>
> Read before writing: `apps/web/AGENTS.md` (Next 16), PostGIS 3.6 and goose docs for the
> schema, the GTFS reference (<https://gtfs.org/documentation/schedule/reference/>) for every
> field used.

## 1. Current scenario

- **The canonical event carries `line` and `route_id`, but no direction, `shape_id` or stop.**
  The raw ITS has `sentido` (`ida`/`volta`/`circular`/`""`) and `shape_id`. `zirix` almost never
  fills `shape_id`, and `sonda` (BRT) leaves it null (research §1.1). The event v1 dropped both on
  purpose.
- **Static GTFS** (`https://dados.mobilidade.rio/gtfs/schedule`, strong `ETag`, unchanged since
  2026-06-10, `feed_end_date` 2026-12-31) was inventoried but never ingested. It has:
  - 494 routes with `route_color`, 7,694 stops, and shapes with `shape_dist_traveled` in metres;
  - 99.5% of trips are `frequencies.txt` headways with `exact_times=0`;
  - `route_id` joins ≈ 99% of live vehicles and `route_short_name` = `line` joins 85–100%.
- **The trimmed sample** `samples/smtr-20261012-gtfs-trimmed.zip` (routes 343, 565 and BRT 22)
  shows that **one route direction can have more than one shape**. BRT `20000221110` has 2
  shapes per direction (`ivlc`/`n7of` and `csy6`/`hj1m`); 343 and 565 have one each.
- **The API** keeps one in-memory snapshot (`apps/api/snapshot.go`): the newest fix per vehicle
  within 300 s. It's refreshed only when `feed_status` moves. A request never queries.

## 2. Planned changes

### 2.1 GTFS in, as a raw file plus a projection (invariant 2)

- **`apps/gtfs`** has two subcommands. `make gtfs` runs both.
  - **`fetch`:**
    - a `GET` with `If-None-Match` from the last stored `ETag`, at most once per 24 h (state file
      in `var/gtfs/state.json`);
    - on a `200`, the body is stored **byte-exact** in the raw log through `packages/raw` with
      source `gtfs/v1` `[VERIFY: raw.Header's fields fit a non-ITS source; if they're
      ITS-specific, add an optional field rather than a second format]`;
    - a `304` stores nothing and logs it;
    - same User-Agent and contact rules as the adapter (invariant 4).
  - **`load <raw key>`** (default: the newest stored GTFS) parses the zip and writes the tables
    below in **one transaction, keyed by the body's SHA-256** (`feed_hash`). Loading the same zip
    twice changes nothing.
- **`packages/gtfs`** parses with `encoding/csv` from the zip in memory:
  - it handles a UTF-8 BOM and columns in any order (by header name);
  - an unknown column is ignored; a missing required column is an error;
  - **patterns:** trips grouped by `(route_id, direction_id, shape_id)`. Each pattern's stop
    sequence is taken from its trips' `stop_times`. If trips in a group have different
    sequences, the most common one is used and the variant count is reported (the loader logs
    it, and §6 records it for the full feed);
  - **main pattern per (route, direction):** the one with the most trips, ties broken by the
    longest shape. That's the strip the page shows. The others are still loaded and used for
    matching (§2.2).
- **Migration `0003_gtfs.sql`** (goose, embedded like the others):

| Table | Key | Columns |
|---|---|---|
| `gtfs_feed` | `feed_hash` | `raw_key`, `fetched_at`, `feed_start_date`, `feed_end_date`, `feed_version`, `loaded_at` |
| `gtfs_route` | `(feed_hash, route_id)` | `short_name`, `long_name`, `route_type`, `color`, `text_color`, `agency_id` |
| `gtfs_pattern` | `(feed_hash, pattern_id)` | `route_id`, `direction_id`, `shape_id`, `headsign`, `is_main`, `trips`, `length_m`, `geom geometry(LineString, 4326)` |
| `gtfs_pattern_stop` | `(feed_hash, pattern_id, seq)` | `stop_id`, `name`, `lat`, `lon`, `dist_m` |
| `gtfs_headway` | `(feed_hash, pattern_id, service_id, start_secs)` | `end_secs`, `headway_secs` |
| `gtfs_service_date` | `(feed_hash, service_id, date)` | `calendar.txt` expanded over the feed's date range, with `calendar_dates.txt` exceptions applied |

  - `pattern_id` is `route_id|direction_id|shape_id`, a deterministic ID.
  - `dist_m` comes from `stop_times.shape_dist_traveled` when it's present. Otherwise it's the
    stop's projection onto the shape. `[VERIFY on the full feed: the share of stop_times with
    shape_dist_traveled, and its unit, metres as the sample suggests]`.
  - Times past 24:00:00 (allowed by GTFS) stay as seconds after midnight of the service day.
  - **The active feed** is the newest `loaded_at`. Older feeds stay: the reliability task (U3)
    needs to know which promise was in force on a given day.
  - **Size:** the full feed's shapes are 18.8 MB as CSV. Record the table sizes in §6. The
    budget is < 150 MB on disk.

### 2.2 Placing a bus on its route: `packages/linematch` (pure Go, no I/O)

- **Candidate patterns for a vehicle:**
  - if `route_id` is in the active feed, that route's patterns;
  - otherwise every route whose `short_name` = `line` and whose `route_type` fits the vehicle's
    `mode` (`bus` → 700/702, `brt` → `[VERIFY on the full feed: the route_type of BRT routes;
    the sample's BRT route is 702]`);
  - otherwise **unmatched** ("sem trajeto no GTFS"). A vehicle is never guessed onto a line.
- **Projection:**
  - in a local equirectangular frame around the point (metres), find the nearest segment of each
    candidate pattern's polyline;
  - that gives `along_m` (distance along the shape) and `offset_m` (perpendicular distance);
  - a fix is **on-route** for a pattern when `offset_m ≤ 60 m` (the threshold comes from the
    measured `offset_m` distribution, p95 on known-good vehicles, recorded in §6; 60 m is the
    starting value).
- **Direction, from movement, provider-agnostic.**
  - Over the vehicle's on-route fixes in the last 10 min of history, compute `Δalong` between
    its oldest and newest on-route fix on each candidate pattern.
  - The vehicle is assigned to the pattern with `Δalong ≥ +150 m`; on a two-way street the
    opposite direction's shape gives a negative `Δalong`.
  - If several patterns of the **same direction** qualify (the BRT's variants), the main pattern
    wins if it qualifies, otherwise the one with the smallest median `offset_m`.
  - **Not assigned, kept separate, never hidden:**
    - "parado ou sentido indefinido": on-route but `|Δalong| < 150 m`, typically at a terminal;
    - "fora do trajeto": no on-route fix in the last 10 min;
    - "sem trajeto no GTFS": unmatched.
  - Each of these lists is shown by name on the page, with a count.
  - **Circular routes** have one pattern, and forward movement is enough.
  - **Why not the provider's `sentido`:** the canonical event doesn't carry it (only some
    suppliers fill it, and "nothing after the adapter knows the provider"). It's used **only to
    validate** this rule (§5 L3).
- **Placement on the strip:** an assigned vehicle is drawn on its direction's main pattern at
  its `along_m` there, if it's on-route for the main pattern. Otherwise it's listed under "em
  outro trajeto da linha" with that pattern's headsign.
- **Gap to the bus ahead** (on the same main pattern, sorted by `along_m`):
  - **The time gap** is how long ago the leader was where the follower is now. It's interpolated
    between the two leader fixes in history whose `along_m` bracket the follower's `along_m`.
    That's a value derived from two observed fixes, so the payload marks it `derived: true` and
    the UI says "≈".
  - **Fallbacks.** If the leader's history doesn't reach back that far (it joined the route
    later, or there's < 30 min of history), there's no time gap. The payload carries only
    `gap_m` (metres along the route), and the UI shows "a 3,2 km". A time is never invented.
  - **Flags:**
    - `gap_large` when the time gap is > 2 × the promised headway ("buraco");
    - `bunched` when it's ≤ 120 s, TransitMatters' definition (ux-direction §3);
    - no flag when there's no promise or no time gap.
- **Promised headway now:**
  - today's active `service_id`s in America/Sao_Paulo, with times after midnight belonging to
    the previous service day;
  - then the headway rows of the direction's main pattern whose window contains now;
  - if several, the smallest headway, and §6 records how often that happens;
  - none → `null`, shown as "sem intervalo programado agora", never 0;
  - every promise carries the feed's date ("segundo o GTFS de 10/06/2026");
  - **outside `feed_start_date…feed_end_date`, the promise is `null`** with the reason
    `feed_expired`. That matters from 2027-01-01.

### 2.3 The API (`apps/api`): still precomputed, still no query per request

- **History in memory.**
  - On start, one query loads the last 30 min of `positions` (one scan of the newest chunk, the
    same cost class as the snapshot query: measure it and record it in §6).
  - After that, each new snapshot appends each vehicle's newest fix if its `event_time` is new.
  - Entries older than 30 min are dropped.
  - Two feeds reporting the same vehicle: history is kept per `(operator, feed, vehicle)`, and
    the line view uses the **same one-per-vehicle choice the snapshot already makes** (B2 in
    `TASK-live-map.md`).
- **GTFS in memory.** The active feed's patterns, stops and headways are loaded at start, and
  again when `max(loaded_at)` changes (checked with the existing 10 s state probe, one more tiny
  query).
- **On each refresh,** run `linematch` over the snapshot and **precompute every line's payloads,
  gzip them, and set their ETags**, as `/v1/vehicles/latest` does now.
- **New endpoints:**

| Endpoint | Body | Caching |
|---|---|---|
| `GET /v1/lines` | `[{line, route_ids, long_name, mode, color, text_color, vehicles_now}]`, every line in the snapshot or in the active feed (feed-only lines have `vehicles_now: 0`) | per refresh; `public, max-age=10, s-maxage=10` |
| `GET /v1/lines/{line}/route` | per direction: `direction_id`, `headsign`, `length_m`, the main pattern's shape as an [encoded polyline](https://developers.google.com/maps/documentation/utilities/polylinealgorithm) (precision 5, Douglas–Peucker to 5 m), stops `[{stop_id, name, lat, lon, dist_m}]`; `feed: {hash, start, end}` | ETag = `feed_hash + line`; `public, max-age=3600` |
| `GET /v1/lines/{line}/live` | `as_of_minute`, `feed_date`, per direction: `promised_headway_secs` (or null) + `promise_reason`, `vehicles [{vehicle, operator, along_m, age_s, speed_kmh, gap: {seconds?, meters, derived}, flags}]` ordered by `along_m`; plus `stopped_or_unknown`, `other_pattern`, `off_route`, `no_gtfs` lists | per refresh, as above |

  - An unknown `{line}` (in neither the snapshot nor the feed) → 404 with a JSON body.
  - `{line}` is matched exactly, URL-decoded.
- **Budgets, all measured in §6:**
  - API RSS < **100 MB** after loading the full feed and 30 min of history (the AWS task's
    limit is 128m);
  - `linematch` over a full snapshot (≈ 2,600 vehicles) < **500 ms**, median of 3, on the Mac;
  - `/route` for the longest line < **40 KB** gzip;
  - `/live` for the busiest line < **8 KB** gzip.

### 2.4 The web: the strip, directions, the line on the map

- **The route payload** has its own hook, `useLineRoute(line)`, built on the `lineRouteOptions`
  factory, with `staleTime` 1 h. The live payload uses `useLineLive(line)`, refetched like the
  snapshot. Both live in `hooks/use-line.ts`, next to the pure rules: polyline decoding,
  `formatGap`, flag text. U0's snapshot-based line view stays as the fallback when `/route`
  is 404 (a line that isn't in the GTFS: `ESPECIAL` and so on).
- **The direction switch** is two links, `?sentido=0` / `?sentido=1`, each labelled by its
  headsign ("→ Copacabana"), with `aria-current="page"` on the active one. It's URL state, so it
  can be shared, and needs no ARIA widget. The default is the direction with more buses now.
- **The strip** (the `RouteStrip` component) is an `<ol>` in route order that interleaves stop
  items and bus items by `dist_m` / `along_m`:
  - a stop: its name;
  - a bus: the vehicle ID, the age ("visto há 40 s"), and the gap to the bus ahead ("≈ 4 min do
    próximo" or "a 3,2 km do próximo");
  - **a `gap_large` gap is an item of its own** between the two buses, with an icon **and**
    text ("intervalo de ≈ 28 min — mais que o dobro do prometido (12 min)");
  - `bunched` gets text too ("colado no próximo");
  - colour is never the only carrier (invariant 7);
  - **long runs of empty stops** (more than 4 between buses) are collapsed into a native
    `<details>`/`<summary>` ("7 pontos"), which is keyboard- and screen-reader-native;
  - the separate lists from §2.2 come after the strip, each with its heading and count.
- **The header** has the line badge in `route_color`/`route_text_color`. If that pair is under
  4.5:1, `--fg`/`--surface` are used instead and the colour becomes a 4 px stripe; record which
  lines needed it. Next to it: the long name, the promise ("prometido: a cada 12 min, 06h–09h ·
  GTFS de 10/06/2026") and "N ônibus nesta direção agora".
- **On the map,** the main pattern's shape is a deck.gl `PathLayer` in the route colour, with a
  white casing so it reads on the basemap at ≥ 3:1. This direction's buses are drawn on top;
  the other direction's buses are hollow, and the legend explains this in text. `fitBounds`
  fits to the shape, not to the buses.
- **The lines page** (`/`) uses `/v1/lines` for long names and colours. Matching still uses the
  line number, plus a match on words of the long name ("copacabana" finds 474), as a second
  group of results under its own heading.

### 2.5 Alternatives rejected

- **Projecting in PostGIS (`ST_LineLocatePoint`) in the consumer and storing it.** It would
  persist a view the UI needs only live, and it would put geometry work on the ingestion path.
  U3 (reliability) needs stored passing times and will decide that with real measurements;
  this task is used as the **independent check** instead (§5 L4).
- **Adding `direction`/`shape_id` to event v2 from the provider's fields.** Only some suppliers
  fill them, it's provider-specific, and it can't be validated without a second method anyway.
- **ETAs.** Out, by decision 2.
- **Map-matching (OSRM/Valhalla).** A stretch goal per CLAUDE.md. Nearest segment on the GTFS
  shape is enough at bus-stop spacing.

## 3. Why

- **The line page becomes the thing ux-direction §4.2 promises:** where my bus is, which way
  it's going, whether there's a hole behind it, and what the city promised.
- **The GTFS projection, the history and the matching are the base of U3** (passing times and
  headways over days) and of the replay of a line (U4).
- **Cost:** one migration, two Go packages, one binary, three endpoints, about 100 MB of API
  memory, and one GTFS fetch per day. It's probably 1–2 Sonnet sessions; Part A (§5, L1–L4) can
  be a session of its own.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `packages/gtfs/*` | new | parser, patterns; tests on the trimmed sample |
| `packages/linematch/*` | new | projection, direction, gaps, promise |
| `packages/store/migrations/0003_gtfs.sql` | new | §2.1 |
| `packages/store/gtfs.go` | new | idempotent load, active-feed read |
| `apps/gtfs/main.go` | new | `fetch`, `load` |
| `apps/api/*.go` | edit | history, GTFS cache, line payloads, 3 endpoints |
| `apps/web/hooks/use-line.ts` + test | new | |
| `apps/web/components/route-strip.tsx`, `line-header.tsx`, `direction-switch.tsx` | new | |
| `apps/web/components/line-view.tsx`, `live-map-canvas.tsx`, `line-search.tsx` | edit | shape layer; long names |
| `docs/research/samples/its-20261012-lines-343-565-22.json.gz` | new | §5 L2 |
| `Makefile` | edit | `gtfs` |
| `.env.example` | edit | `GTFS_URL`, `GTFS_STATE_DIR` |
| `docs/tasks/TASK-aws-infra.md` | edit | a daily `gtfs` timer in compose/systemd, **only if the AWS task hasn't started yet**; otherwise add it as a follow-up there |
| `docs/architecture.md` | edit | §5 (GTFS projection, line views), §10 #12 (what the stale feed costs, measured) |
| `docs/research/README.md` | edit | the `sentido` ↔ `direction_id` finding (L3) |
| `CLAUDE.md`, `README.md` | edit | stack, layout, Make targets, status |

## 5. Verification

**Part A: data (Go)**

- **L1, the parser on the real sample:**
  - the trimmed zip gives exactly 3 routes;
  - 8 patterns, from the sample's `(route_id, direction_id, shape_id)` groups: 4 for BRT
    `20000221110` and 2 each for 343 and 565;
  - the right main pattern per direction;
  - stop counts per pattern equal `stop_times` counted independently (`awk` over the
    unzipped files, recorded here);
  - **loading twice** leaves every `gtfs_*` table's checksum unchanged (idempotency is a test,
    CLAUDE.md).
- **L2, the sample for matching** (a trimmed provider response, like the GTFS sample):
  - from the recorded run's raw log, take 30 consecutive minutes;
  - keep only records whose `servico` is 343, 565 or 22, still in the raw envelope format so the
    test goes through `its.MinuteEvents`;
  - record its source raw keys and minutes in research. Target ≤ 1 MB.
- **L3, direction against the provider's `sentido`** (validation only):
  - over L2, and over the full 118-minute run for every line, compare the assigned direction
    with `sentido`, wherever `sentido` is `ida`/`volta`;
  - first establish whether `ida` = `direction_id 0` from the data (record it);
  - **pass: ≥ 90% agreement** among vehicles assigned with a non-empty `sentido`;
  - **below 90%, stop**, record the disagreements by line and supplier, and come back to
    planning;
  - record the shares: assigned / parado-indefinido / fora do trajeto / sem GTFS.
- **L4, independent check of `along_m`:** for 50 random fixes from L2, PostGIS
  `ST_LineLocatePoint(geom, point) * ST_Length(geom::geography)` agrees with `linematch`'s
  `along_m` within **10 m** for all 50.
- **L5, gaps:**
  - for one pair of consecutive buses in L2, recompute the time gap by hand from the leader's
    two bracketing fixes (shown in §6) and match the API's value within 1 s;
  - a leader with too little history gives `meters` only, no `seconds`.
- **L6, the promise:**
  - for 565 at 08:00 and 23:00 on 2026-10-12 (a holiday: check `calendar_dates`), the
    `promised_headway_secs` equals a manual lookup in the sample's `frequencies.txt`, shown in
    §6;
  - a date after `feed_end_date` gives `null` with `feed_expired`.
- **L7, API (testcontainers):**
  - the sample GTFS and the L2 minutes are loaded;
  - `/v1/lines/565/live` agrees with L3–L6;
  - 1,000 requests run 0 queries (the existing counter);
  - `/v1/lines/NAOEXISTE/*` returns 404;
  - the §2.3 budgets are measured on the full feed and the 118-minute run (`transit_pulse_run`),
    medians of 3.

**Part B: screen**

- **L8:** `/linha/565?sentido=0` and `?sentido=1` show the strip in stop order, with the buses
  between the right stops. Spot-check 3 buses against their lat/lon on the map. Screenshots at
  390 × 844 and 1440 × 900, in both colour schemes, go in `docs/tasks/assets/line-page-*.png`.
- **L9:** a line not in the GTFS (`ESPECIAL`) shows U0's view with "sem trajeto no GTFS". A line
  with the promise `null` says so in text.
- **L10:**
  - axe reports 0 violations on the line page in both schemes;
  - the strip read with VoiceOver (macOS) announces stop, bus, age and gap in order;
  - the transcript of one screen goes in §6;
  - keyboard: the direction links, every `<details>` and the map controls are reachable.
- **L11:** `prefers-reduced-motion` means no animated map transitions (`fitBounds` with
  `duration: 0`).
- `make lint`, `make test` and `make web-build` pass. Commit per part (no `Co-Authored-By`).

## 6. Results

*(Filled in by the executing session.)*
