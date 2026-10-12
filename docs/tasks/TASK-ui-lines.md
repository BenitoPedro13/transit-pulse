# TASK-ui-lines (U0)

> Written 2026-10-12. The first step of `docs/research/ux-direction.md` (decided 2026-10-12:
> rider tool + accountability, with the line page as the hub; no ETAs; mobile-first). **Web only,
> with no API, schema or pipeline change.** It reorganises what the snapshot already gives
> (`GET /v1/vehicles/latest`) around lines. `TASK-line-page.md` (U1) later fills the line page
> with the route, direction, stops and gaps; this task builds the pages and URLs it fills.
>
> Next.js 16 is newer than any model's training data. Read `apps/web/AGENTS.md`, and for each
> Next feature used, the matching guide in `apps/web/node_modules/next/dist/docs/`, before
> writing it (dynamic routes: `01-app/03-api-reference/03-file-conventions/dynamic-routes.md`).

## 1. Current scenario

- `/` is a single desktop page: the status strip, a map of every vehicle, and a 2,627-row table
  with a "filter by line" text box (`components/vehicle-view.tsx`).
- **Seen in the user's screenshot (2026-10-12):**
  - **The page is rendered dark** (the system's dark mode), but the secondary text uses
    `text-zinc-700`, which assumes a light background. The operator list, "2.627 veículos…", the
    legend and the filter's helper text are close to invisible on black.
  - That's a **WCAG 2.2 AA contrast failure** (1.4.3) that the earlier axe run missed: it ran in
    light mode only.
- **There's no way in for a rider.** Nothing lists the lines, a line has no page or URL, and on a
  phone the map and the table stack into one very long page.

## 2. Planned changes

### 2.1 Routes (App Router)

| URL | What | Notes |
|---|---|---|
| `/` | **Lines**: a search box, then every line that has vehicles in the snapshot | replaces today's page |
| `/linha/[line]` | **One line**: its buses on the map and as a list | `line` is the snapshot's `line` value, URL-encoded (`ESPECIAL`, `LECD…`, `0` are real values and keep their pages) |
| `/rede` | **A rede agora**: today's page (map of everything + table), unchanged apart from §2.4 | |

- A shared header in `app/layout.tsx`: the site name links to `/`; a nav link "A rede agora"
  goes to `/rede`; the **status strip moves into the header**, so every page shows live /
  stale / failed (invariant 6).
- `page.tsx` files stay Server Components. The interactive parts are small client components
  (global rule).
- **`/linha/[line]`:**
  - `params` is a Promise in Next 16 `[VERIFY: dynamic-routes.md in node_modules]`;
  - lines aren't known at build time, so nothing is pre-generated. Check how this interacts
    with `cacheComponents: true` `[VERIFY: the Cache Components guide on dynamic params without
    generateStaticParams]`;
  - the page renders its shell on the server, and the vehicles come from the existing TanStack
    Query hook on the client.

### 2.2 The lines page (`/`)

- **Search:** a native `<input type="search">` with a `<label>` ("Buscar linha"), and below it
  a plain list of links that narrows as you type. It isn't a combobox: no popup and no ARIA
  state machine, so there's nothing to get wrong (global rule).
  - **Matching:** the trimmed, case-insensitive prefix of the line value ("47" matches "474" and
    "472"). Prefix is used rather than contains, because cariocas type line numbers from the
    start.
- **Each list item is a link** to `/linha/<line>` with: the line, its mode (text "Ônibus" / "BRT"
  plus the same circle/square shape as the map; colour isn't the only signal), its vehicle count
  now ("6 ônibus agora"), and the age of its freshest fix.
- **Order:** natural sort on the line value ("2" < "10" < "343" < "ESPECIAL"; numeric parts
  compared as numbers), with BRT lines as their own group after the buses.
- **Vehicles with no line** (`line: null`) aren't a line. They're one sentence at the end: "N
  veículos sem linha identificada — veja em A rede agora", linked to `/rede`. Never hidden
  (missing ≠ zero).
- **Empty states, kept distinct:**
  - loading;
  - "a API não respondeu" (no data);
  - "nenhuma linha começa com '…'" (the search found nothing);
  - "nenhum veículo no último minuto gravado" (an empty snapshot).

### 2.3 The line page (`/linha/[line]`)

- **Header:** "Linha 474", the mode, and "N ônibus agora · último sinal há X s".
- **Mobile, the default:** the map first (≈ 50 svh, fitted to this line's vehicles), then the
  list. **From `lg` up:** two columns, as `/rede` has today.
- **Map:** the existing `LiveMap` given only this line's vehicles. A new `fitBounds` makes the
  map fit them (padding 48 px, max zoom 15) on first data and when the line changes, and not on
  every refetch, so a user who pans isn't yanked back. With 1 vehicle, centre it at zoom 15.
  With 0, the default Rio view.
- **List:** the existing `VehicleTable` columns minus Linha (it's the page) and Operador when the
  line has only one operator. On mobile it's a card list (`<ul>`, one `<li>` per vehicle:
  vehicle ID, age, speed, heading).
  - **Card or table?** The table stays at `lg` and up, and cards below. Both are built from the
    same rows, and both are real lists or tables for screen readers.
- **A line with no vehicles in the snapshot** (stale link, or the line stopped for the night)
  says "Nenhum ônibus da linha 474 no último minuto gravado (13:24)". It's a 200 page, not a
  404: the line is real; there's just nothing right now. If the feed failed, the header's status
  strip already says so.
- **Placeholder for U1:** one line of text, "Em breve: o trajeto, os pontos e o intervalo entre
  os ônibus", so the page doesn't pretend to be finished. U1 removes it.

### 2.4 Theme and contrast (fixes the screenshot)

- **Colour tokens** as CSS variables in `globals.css`, with one set for light and one for
  `@media (prefers-color-scheme: dark)`: `--fg`, `--fg-muted`, `--bg`, `--surface`, `--border`,
  `--focus`, `--bus`, `--brt`. Map them into Tailwind v4 with `@theme` `[VERIFY: Tailwind v4
  docs, @theme inline with CSS variables]`. Replace every hard-coded `zinc-*`/`blue-*` in
  `components/` with the tokens.
- **Every pair must reach 4.5:1** (text) and **3:1** (the vehicle shapes and the focus ring
  against the map and the background), in both schemes. Record each pair's ratio in §6. Compute
  it with a script in `scratchpad`, never by eye.
- **The basemap stays light in both schemes.** A dark basemap is out of scope (`TASK-live-map.md`
  §2.7). The map gets a `--border` frame, so it doesn't look like a hole in a dark page. The
  vehicle colours are already checked against the light basemap.

### 2.5 Hooks and rules (the global "every hook is abstracted" rule)

In `hooks/use-latest-vehicles.ts`, or a new `hooks/use-lines.ts` (pure functions next to the
hooks, as today):

- `toLineIndex(rows): LineSummary[]` gives `{ line, mode, operators, vehicles, freshestAgeSeconds }`
  for every non-null line, plus `unlinedCount`. When a line value appears under two modes (it
  shouldn't, `[VERIFY on the recorded run: SELECT line, count(DISTINCT mode) FROM positions …]`),
  it's two entries keyed by `(mode, line)`, and the URL becomes `/linha/<line>?modo=brt`.
- `compareLines(a, b)`: the natural sort in §2.2.
- `matchLines(index, query)`: the prefix match in §2.2.
- `useLineIndex()` and `useLineVehicles(line)` both select from the existing
  `latestVehiclesOptions` query (TanStack `select`). They make one request, shared by every page.
- **Vitest** for `toLineIndex`, `compareLines` and `matchLines`. The inputs are rows built from
  a **real snapshot** saved from the recorded run: `curl /v1/vehicles/latest` on the
  `transit_pulse_run` database, cut to ~200 vehicles across ≥ 10 lines, with nulls and a BRT
  line kept, saved as `apps/web/hooks/testdata/snapshot-20261012.json`. Real data, not
  hand-built (CLAUDE.md "Tests").

### 2.6 Alternatives rejected

- **A combobox/autocomplete for search:** an ARIA widget for no gain over a filtered list of
  links.
- **Line names ("Jardim Oceânico – Candelária") in this task:** they come from GTFS
  `routes.txt`, which arrives with U1. Showing only the number until then is honest.
- **Server-side rendering of the line list:** the data changes every minute and lives in the
  API, so the client query is the existing, measured path.

## 3. Why

- **The user sees a rider-shaped product today**, on data that already exists: pick a line, see
  its buses, works on a phone.
- **It fixes a real WCAG failure** in dark mode.
- **It sets up U1:** the URLs, the layout and the hooks that the route strip goes into.
- **Cost:** web only. No new dependency. About half a Sonnet session.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `apps/web/app/layout.tsx` | edit | header, nav, status strip |
| `apps/web/app/page.tsx` | rewrite | lines page |
| `apps/web/app/linha/[line]/page.tsx` | new | line page shell |
| `apps/web/app/rede/page.tsx` | new | today's page moved here |
| `apps/web/app/globals.css` | edit | colour tokens, light + dark |
| `apps/web/components/line-search.tsx` | new | client: input + filtered links |
| `apps/web/components/line-view.tsx` | new | client: map + list for one line |
| `apps/web/components/vehicle-cards.tsx` | new | the mobile list |
| `apps/web/components/live-map.tsx`, `live-map-canvas.tsx` | edit | `fitBounds` prop; tokens |
| `apps/web/components/*.tsx` | edit | `zinc-*` → tokens |
| `apps/web/hooks/use-lines.ts` + `.test.ts` | new | §2.5 |
| `apps/web/hooks/testdata/snapshot-20261012.json` | new | real, trimmed |
| `README.md`, `CLAUDE.md` | edit | layout tree, status line |

## 5. Verification

Run against `make api` on the recorded database (`transit_pulse_run`), in `next dev` and in
`next build && next start`.

- **V1:** `/` lists every line in the snapshot. Its count of lines and the "sem linha" number
  equal the distinct non-null `line` count and the null-`line` count computed with `jq` over the
  same snapshot JSON (independently of the app).
- **V2:**
  - typing "47" shows only lines starting with "47";
  - "xyz" shows the no-match message;
  - each result link goes to its page.
- **V3:**
  - `/linha/474` (or the busiest line in the run, whichever exists) shows N vehicles, where N
    equals `jq` over the snapshot for that line;
  - the map is fitted to them;
  - panning, then waiting for a refetch, doesn't move the map.
- **V4:** `/linha/NAOEXISTE` shows the "nenhum ônibus" message with status 200, and nothing
  invented.
- **V5:** at 390 × 844 (an iPhone viewport) there's no horizontal scroll on any page. Screenshot
  each page at that size and at 1440 × 900, in both colour schemes, into
  `docs/tasks/assets/ui-lines-*.png`.
- **V6:** axe reports **0 violations on all three pages in both colour schemes** (Playwright
  `colorScheme: 'dark'` and `'light'`). The §2.4 contrast table is filled in with ratios ≥ 4.5
  (text) and ≥ 3 (shapes, focus).
- **V7:**
  - keyboard only: Tab reaches the search, every result, the nav and the table, with a visible
    focus ring;
  - the status announcement still fires as `TASK-live-map.md` B7 measured (exactly once per
    change).
- `make lint`, `make test` (Vitest included) and `make web-build` pass. Commit (no
  `Co-Authored-By`).

## 6. Results

*(Filled in by the executing session.)*
