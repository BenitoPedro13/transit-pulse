# TASK-line-routes (D3)

> Written 2026-10-13. D1 and D2 are complete. The next step in
> `docs/research/ux-direction.md` §3.2: selecting a line shows its static GTFS
> routes and stops on the existing dashboard, in live and replay views.
> **Decision (user, 2026-10-13): export static files**, rather than adding
> Postgres tables and API endpoints. No deployment is part of this task.

## 1. Current scenario

- `dashboard.tsx` selects `linha` through the URL. `panel-linhas.tsx` lists its
  observed vehicles; `live-map-canvas.tsx` dims other vehicles and fits the selected
  line's observed positions. Replay swaps vehicle layers and preserves selection.
- The API and `history-hour.v1` carry the line identifier but do not publish route
  geometry. No GTFS loader exists. Static assets are served by the web app.
- Research captured SMTR's GTFS on 2026-10-12; its ZIP hash and metadata are recorded
  in `docs/research/README.md` §2. The committed trimmed ZIP contains lines 343,
  565 and BRT 22: 36 trips, 250 stops, 2,598 shape points, eight distinct
  `(route_id, direction_id, shape_id)` groups. BRT 22 has multiple shapes per direction.
- A stale feed is acceptable for drawing routes (user, 2026-10-12). The old
  `TASK-line-page.md` remains deferred: headways and vehicle direction inference
  are outside this task. Static geometry must never claim to be observed travel
  or the schedule in force at the replay date.

## 2. Planned changes

### 2.1 Capture and export once, serve files

- `apps/gtfs`: a Go command for conditional fetch plus export, and offline export
  from a retained raw response. `make gtfs` prepares `apps/web/public/gtfs/` before
  the production build; `make gtfs-export` regenerates it without a network request.
- Fetch `https://dados.mobilidade.rio/gtfs/schedule` at most once per 24 h; persist
  attempt time and validators under `var/gtfs/`. A normal repeat makes zero requests;
  the next eligible request sends `If-None-Match` / `If-Modified-Since` when available.
  No automatic retry loop. A failed request leaves the previous export available.
- Use the existing `packages/raw` envelope with source `gtfs` (the package adds
  `/v1/` itself). Store response bytes before parsing. `minute_utc` is the fetch
  minute, `page=0`, `attempt=1`, cursors null. Record source URL, capture time,
  validators and ZIP SHA-256. A 304 reuses the prior raw location.
- Reuse `RAW_DIR` and contact conventions; document any new environment setting.
  Bound HTTP body (64 MiB), total uncompressed ZIP content (256 MiB) and request
  duration (2 min). Never extract archive paths onto disk.
- `packages/gtfs`: standard-library ZIP/CSV parser plus deterministic static exporter.
  A manifest names content-addressed per-line files under `/gtfs/<feed-hash>/`.
  The manifest changes only after every file is written; retain older feed directories.
  Generated assets are ignored by Git. Offline export is deterministic for the same
  retained raw response and metadata, and requires no database or broker.

### 2.2 Preserve the provider's geometry and stop sequences

- Read `routes`, `trips`, `shapes`, `stops`, `stop_times`, `agency`, and `feed_info`
  by CSV header names, accepting a BOM, reordered headers and unknown columns.
  Missing required columns, invalid coordinates, missing referenced stops/shapes,
  duplicate identifiers and invalid sequences fail with contextual errors.
- Group trips by route, direction, shape **and ordered stop sequence**; preserve
  distinct stop variants and repeated stops on loops. Deduplicate identical geometry
  and stop records in the per-line file. Sort points/stops by numeric sequence,
  not source row order. Keep original coordinates and opaque identifiers.
- Match selected `linha` by exact `route_short_name`. Retain every matching route
  and agency; never guess a vehicle's direction or snap its position to a shape.
  Blank short names are excluded from selectable line exports and counted.
- Add JSON Schemas for the manifest and per-line payload. Include feed provenance,
  declared start/end dates, route/agency names, pattern headsign/direction, geometry
  and ordered stop references. Hash-derived file names avoid using provider IDs as paths.

### 2.3 Existing dashboard, one overlay

- `hooks/use-line-routes.ts`: TanStack Query factories for the manifest (enabled only
  with a selected line) and that line's immutable file. Consume cancellation signals,
  key payload queries by feed hash and line, and distinguish loading, missing and error.
  A line change immediately removes the previous line's overlay, including while
  the next payload loads. No provider request occurs in the browser.
- `components/panel-route.tsx`: a selected-line subsection with route names, an
  accessible pattern selector, and the selected pattern's ordered stop list. Default
  to a deterministic real pattern; name its destination and agency. Native controls
  provide keyboard and touch access; stop details are readable without the map.
- `panel-linhas.tsx` includes this subsection alongside the existing vehicle list.
  Keep the labelled 44 px Recolher action outside scrolling content. Selection and
  playback time stay in the existing URL; pattern choice is local to the selected line.
- `components/route-reference.tsx`: MapLibre line layers below trails/vehicles and outlined
  circle stop markers. Use existing scheme tokens; static lines are
  visually distinct from fading vehicle trails. Expose the same names in the panel.
- `live-map-canvas.tsx`: fit the selected pattern once it is ready, using existing
  panel padding and reduced-motion settings. If geometry fails, retain the existing
  observed-vehicle fit. Pattern changes refit once; playback never repeatedly refits.
  Keep static geometry/data references stable across playhead frames.
- Playback verification exposed excessive full-window trail geometry submitted every
  frame. Add a cached, minute-sized view of the existing binary paths for the visible
  300 s trail interval, retaining the original endpoints that bracket it. This changes
  only submitted geometry, not sampling, interpolation, vehicle heads or gap rules.
  Prove visible segments are retained against the real history fixtures before measuring.
- Keep a visible map disclosure even with the phone panel collapsed:
  `Trajeto de referência · GTFS estático`. Expanded details include capture date,
  declared validity, SMTR attribution and that geometry is not historical proof.
  Missing exact match: `Sem trajeto no GTFS para esta linha`. Load errors have a retry
  action and never remove the observed vehicles or stop playback.

### 2.4 Alternatives considered

- Postgres GTFS tables and new API endpoints: declined by the user; geometry has
  no per-view query requirement and static files support the eventual replay site.
- Downloading the ZIP in each browser: duplicates provider traffic and ZIP parsing.
- One full-city geometry payload: loads unused lines on every selection.
- A single guessed direction/shape: hides the provider's actual variants.
- Vehicle map matching/headway metrics: belongs to the deferred rider-tool scope.

## 3. Why

Route context makes selected-line exploration understandable: where a line goes,
its destinations and stops. Static projections are rebuildable from byte-exact
captures and remain available when the live backend shuts down. This adds one
small Go command, a parser/exporter and existing-library web layers, with no new
runtime dependency, database migration or AWS resource.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `apps/gtfs/*.go` | new | Fetch, saved validators/raw location, offline export, tests |
| `packages/gtfs/*.go` | new | Parser, projection types, static publisher, real-fixture tests |
| `packages/contracts/line-routes*.schema.json` | new | Manifest and line payload contracts |
| `apps/web/hooks/use-line-routes.ts` (+ tests) | new | Queries, types and pure selection rules |
| `apps/web/components/panel-route.tsx`, `route-reference.tsx` | new | Accessible stop list and static overlay |
| `apps/web/components/dashboard.tsx`, `panel-linhas.tsx`, `live-map-canvas.tsx` | edit | Shared route state, integration and fit |
| `apps/web/hooks/history-window.ts` (+ tests) | edit | Cached visible trail geometry; preserve real segments and gaps |
| `Makefile`, `.gitignore`, `.env.example` | edit | Export commands, generated files, config |
| `README.md`, `CLAUDE.md`, `docs/research/ux-direction.md` | edit | Setup, status, D3 link |
| `docs/research/runs/20261013-line-routes/`, `docs/tasks/assets/line-routes-*` | new | Reusable checks, measured results, screenshots |

## 5. Verification

- **V1, real geometry:** parse the committed trimmed ZIP and compare every exported
  coordinate, referenced stop name and numeric sequence with an independent CSV
  oracle. All three lines and BRT's four shapes remain represented. Schemas validate
  every exported file; a malformed file is rejected.
- **V2, parser boundaries:** derive malformed/changed archives from the real capture
  to check BOM/reordered headers, missing columns, unknown references, repeated stops,
  invalid coordinates and sequences. No fabricated provider positions.
- **V3, capture and rebuild:** local HTTP server serves the real ZIP with validators;
  byte-exact raw readback, repeat within 24 h = zero extra requests, eligible 304,
  non-200 response and invalid ZIP preserve the previous projection. Offline export
  gives identical manifest and payload bytes; interrupted publication never advertises
  a file that does not exist. These tests need no database.
- **V4, real full feed:** capture the current feed, save metadata and measure medians
  of three offline exports: elapsed time, peak RSS, total JSON/gzip bytes, largest
  line and number of lines/patterns/stops. Targets: export < 10 s, peak RSS < 512 MiB,
  total payload gzip < 15 MiB, per-line gzip < 500 KiB. Diagnose any exceeded budget
  before continuing. Report unmatched recorded line names without guessed joins.
- **V5, interactions:** at 1440×900 and 390×844 in both schemes, select bus/BRT lines,
  switch patterns, clear/reselect, load a shared `linha`/`t`, scrub and play. Rapidly
  switch with delayed files: no previous line is shown under a new selection. Check
  missing/failed assets, line without current vehicles, and Recolher after scrolling
  the stop list. Save screenshots and machine-readable results.
- **V6, accessibility:** zero axe WCAG A/AA violations in both schemes, keyboard
  pattern selection and ordered stop reading, 44 px touch controls, no horizontal
  overflow, reduced-motion camera moves without animation. Reference disclosure
  remains visible in compact mobile, live and replay states.
- **V7, performance/regression:** at 60× with trails and one route, median of three
  10 s runs ≥ 30 fps; cached selection/pattern switch displays within 1 s. Route files
  are not refetched on playhead ticks. The existing D2 loading/gap tests still pass.
- `make lint`, `make test`, `make web-build` pass. Commit implementation and evidence
  only after checks pass, without `Co-Authored-By`.

## 6. Results

**D3 complete, 2026-10-13.** The full retained SMTR feed is projected into static
per-line files. Selected lines have destination variants, an ordered stop list and
reference geometry in live/replay, including lines without observed vehicles.
No database migration, API endpoint, runtime dependency or deployment was added.

| Check | Result |
|---|---|
| V1, geometry and contracts | Independent CSV oracle checks every fixture coordinate, stop and sequence; all three fixture lines and four BRT shapes preserved. Manifest and **all 494 full-feed line files** pass the JSON Schemas |
| V2, parser boundaries | BOM/reordered rows and columns preserve geometry. Missing columns, unknown stops/shapes, invalid coordinates/sequences and duplicate trips fail. Repeated stops remain ordered on loops |
| V3, capture/rebuild | Real fixture served over local HTTP: raw readback byte-exact; repeat within 24 h has zero extra requests; conditional 304, offline equality, 503 and invalid ZIP cases pass. Failed publication leaves the manifest untouched. CLI serializes processes with a state-file lock |
| V4, full feed | **494 lines, 961 patterns, 7,493 referenced stops**. Same ZIP hash as 2026-10-12; Last-Modified remains 2026-06-10. Snapshot line matches: **330/359**, with 29 unmatched labels disclosed rather than guessed |
| V4, isolated export | Runs **5.815 / 8.518 / 6.461 s**, median **6.461 s**. Median peak RSS **290,111,488 B (276.7 MiB)**. JSON **13,343,543 B**; gzip **3,319,000 B**. Largest gzip: line 2335, **29,802 B**. All budgets pass |
| V5, interactions | Bus 343 and BRT 22, all four BRT variants, clear/reselect, shared `linha`/`t`, delayed 343 followed by 565, missing ESPECIAL, file 503 and retry pass. Observed vehicles remain unchanged by pattern selection. Latest mode retains geometry |
| V6, accessibility/mobile | **Zero axe violations** at 1440×900 and 390×844 in both schemes. Keyboard pattern selection, ordered stop list, touch Recolher after scrolling, retained line selection, zero horizontal overflow and visible compact reference disclosure pass. Reduced motion has zero interpolation/trails and no autoplay |
| V7, playback | Apple M2 Metal GPU, actual map/trails at 60×: 10 s runs **16.90 / 31.39 / 35.96 fps**, median **31.39 fps ≥ 30**. Results are browser frame cadence on the Next development server; the individual runs vary |
| V7, cached pattern switch | Browser event-to-render times **186 / 155 / 250 / 146 ms**, all < 1 s. **Zero route-file requests during playback** |
| Trail regression | Two real-fixture tests prove every segment intersecting the visible 300 s interval survives clipping, every coordinate remains exact, no path crosses a gap, and buffers are reused within a minute. Browser rerun retains BRT 7001's 1,800 s gap, real-position mode and empty loading/failure replay layers |
| Repository checks | Go suite and new GTFS tests pass; `make lint` reports zero Go issues and web lint passes; **103 web tests** pass; final `make web-build` passes TypeScript and production compilation |

**Rendering decisions resolved during validation:** reference geometry uses MapLibre
line/circle layers below the separate deck.gl canvas. The first deck.gl reference
implementation missed the frame target; its measurements are retained in the run
folder. Playback still submitted the whole multi-hour trail history each frame,
so `activeTripsData` now caches the minute's visible interval plus its original
bracketing fixes. Paths remain separate and retain their original identity for
selection colours. No sampling, smoothing, head position or gap rule changed.

The reference badge originally collided with the desktop colour legend; it now
sits above it. Provenance includes capture date, Last-Modified, declared validity,
SMTR attribution and the limitation on interpreting static geometry historically.

Export measurements are taken separately from browser/compiler checks. An overlapping
run measured 13.33 s median; the final isolated run above passes. Automation IPC is
excluded from pattern-switch latency by measuring in the browser. These are local
measurements, not a claim about every device.

Evidence and rerun instructions: [`../research/runs/20261013-line-routes/README.md`](../research/runs/20261013-line-routes/README.md).
Screenshots and browser assertions: `assets/line-routes-*`; the D2 honesty regression
has separate `line-routes-history-*` files, preserving D2's original evidence.

Primary references checked 2026-10-13: [GTFS Schedule reference](https://gtfs.org/documentation/schedule/reference/)
(`routes`, `trips`, `shapes`, `stops`, `stop_times`), [deck.gl PathLayer](https://deck.gl/docs/api-reference/layers/path-layer)
and [ScatterplotLayer](https://deck.gl/docs/api-reference/layers/scatterplot-layer).
Read Next's installed `node_modules/next/dist/docs/01-app/03-api-reference/03-file-conventions/public-folder.md`:
public assets resolve from `/`; default HTTP caching is `max-age=0`. TanStack's
immutable query cache does not imply an immutable HTTP response header.
Final renderer references: [react-map-gl Source](https://visgl.github.io/react-map-gl/docs/api-reference/maplibre/source),
[Layer](https://visgl.github.io/react-map-gl/docs/api-reference/maplibre/layer),
[MapLibre style specification](https://maplibre.org/maplibre-style-spec/layers/)
and [TanStack cancellation](https://tanstack.com/query/latest/docs/framework/react/guides/query-cancellation).
