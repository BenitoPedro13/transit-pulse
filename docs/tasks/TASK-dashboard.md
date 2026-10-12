# TASK-dashboard (D1)

> Written 2026-10-12. The first step of `docs/research/ux-direction.md` §3 ("Rio em movimento").
> It replaces the earlier `TASK-ui-lines.md` (U0, superseded before it ran; same file, renamed).
> **Web only, plus one read of the existing API.** No schema, pipeline or API change.
>
> **Before writing code:**
> - Next.js 16 is newer than any model's training data. Read `apps/web/AGENTS.md`, and for each
>   Next feature used, the matching guide in `apps/web/node_modules/next/dist/docs/`.
> - Load the `frontend-design` skill (taste) and the `dataviz` skill (the speed scale, KPI tiles)
>   before writing UI.
> - Use the shadcn CLI to add components, never hand-copied code (CLAUDE.md §2).

## 1. Current scenario

- `/` (`app/page.tsx` → `components/vehicle-view.tsx`):
  - an `h1`, a status paragraph, then a framed map (≈ ⅓ of the screen) beside a 2,627-row TanStack
    table with a "filtrar por linha" box;
  - vehicles are an `IconLayer` (circle = bus, square = BRT) on Protomaps `namedFlavor("light")`;
  - data: `GET /api/v1/vehicles/latest` through `latestVehiclesOptions`, refetched every 15 s.
- **Dark mode is broken.** Secondary text uses `text-zinc-700`, so on a dark system theme it's
  nearly invisible (user's screenshot, 2026-10-12). That's a WCAG 1.4.3 failure; the earlier
  axe run was light mode only.
- **Fonts:** Geist and Geist Mono from `create-next-app`, never chosen.
- **No shadcn/ui and no component library yet.**

## 2. Planned changes

### 2.1 Layout

**Desktop (≥ 1024 px):** the map fills the viewport. Everything else floats over it.

```
┌──────────────────────────────────────────────────────────────────────────┐
│ ┌─────────────────────────────────┐                 ┌──────────────────┐ │
│ │▓ 13:24  2.627 ÔNIBUS NA RUA    ▓│  ← letreiro     │ Agora│Linhas│Ônibus│ │
│ └─────────────────────────────────┘                 ├──────────────────┤ │
│  ● Não está ao vivo · último minuto 13:24           │ 2.401 ônibus     │ │
│                                                     │   226 BRT        │ │
│            (full-bleed map, buses glow)             │ 61% em movimento │ │
│                                                     │ 18 km/h mediana  │ │
│                                                     │ ──────────────── │ │
│                                                     │ Mais ônibus agora│ │
│                                                     │ [474] 31  [SV..] │ │
│                                                     └──────────────────┘ │
│ Colorir: (•) modo ( ) velocidade   legenda …       Ver como tabela ▸     │
└──────────────────────────────────────────────────────────────────────────┘
```

**Phone (< 1024 px):** the map is `100svh`, with the letreiro overlaid at the top. A bottom
panel holds the same tabs.

```
┌────────────────────────┐
│▓ 13:24 2.627 ÔNIBUS   ▓│
│                        │
│        (map)           │
│                        │
├────────────────────────┤
│ Agora │ Linhas │ Ônibus│  ← collapsed: tabs + one summary line
│ 61% em movimento   ⌃   │  ← button "Expandir painel" (aria-expanded)
└────────────────────────┘
```

- **The bottom panel is a plain fixed `<section>`** with a toggle button that switches between
  about 25% and 70% of the viewport height. There's no drag gesture and no drawer library: a
  non-modal drawer is hard to get right, and a button is enough.
- **The map stays operable above the panel.** On an expanded panel the map gets that much
  bottom padding, so `fitBounds` keeps the selection visible.

### 2.2 Visual system (from ux-direction §3.1)

- **Tokens** are CSS variables in `globals.css`, with one set for light and one for
  `@media (prefers-color-scheme: dark)`. They're exposed to Tailwind v4 with
  `@theme inline` `[VERIFY: Tailwind v4 theme docs]`.
  - Names: `--baia`, `--morro`, `--calcadao`, `--neblina`, `--letreiro`, `--letreiro-bg`,
    `--ceu` (bus), `--coral` (BRT), `--focus`.
  - Start from the hex values in ux-direction §3.1, then **adjust them until every pair passes**:
    text ≥ 4.5:1; bus/BRT marks, the focus ring and the panel edges ≥ 3:1 against the basemap's
    dominant land and water colours.
  - Check with a script in `scratchpad` (WCAG relative luminance), never by eye. Record the
    final values and ratios in §6.
  - **Every `zinc-*`/`blue-*` utility in `components/` is replaced with the tokens.**
- **Fonts** (`next/font/google`, replacing Geist):
  - **Archivo**, variable, with the `wdth` axis, is the body and UI face. The condensed width
    (≈ 75) is for small-caps labels, and numbers use `tabular-nums`.
  - **Doto** is for letreiro text only, at **≥ 20 px**, because dot-matrix text is unreadable
    when small.
  - `[VERIFY: both families and Archivo's axes on Google Fonts / next/font]`
- **The letreiro (`components/letreiro.tsx`)** is a dark panel (`--letreiro-bg`) with amber Doto
  text and a faint dot grid behind it (a CSS radial-gradient). It's dark in both schemes.
  - **What it shows:**
    - by default: clock and count, "13:24 · 2.627 ÔNIBUS NA RUA";
    - with a line selected: "474 · 31 ÔNIBUS";
    - with a bus selected: "474 · A29001";
    - when the data isn't live, it starts with "SEM SINAL" or "ATRASADO" from the existing
      `statusOf` rule.
  - **The roll:** when its text changes, the old text slides up and the new text comes in from
    below in 300 ms, as real signs scroll. With `prefers-reduced-motion`, the text swaps with no
    motion.
  - **It's real text,** not an image or canvas, so screen readers read it. The animation moves
    duplicate copies marked `aria-hidden`.
- **Basemap:**
  - **Dark scheme:** Protomaps `namedFlavor("dark")` with background, earth and water overridden
    toward `--baia` (a "Guanabara at night" tint). `[VERIFY: the Flavor object's keys in
    @protomaps/basemaps 5.7 to override]`
  - **Light scheme:** `namedFlavor("light")`, slightly tinted.
  - **Sprites** follow the scheme (`sprites/v4/dark` | `light`).
  - **A scheme change** while the page is open swaps the style through a `matchMedia` listener,
    in a hook that has a cleanup function.
- **Vehicles:**
  - **Dark scheme:** additive blending on the vehicle layer, so dense corridors glow.
    `[VERIFY: deck.gl 9.4 / luma.gl 9 "parameters" for blend factors]`
  - **Light scheme:** normal blending.
  - **The shape still carries mode** (circle for bus, square for BRT), as today.
  - **With a line selected,** every other vehicle drops to 15% opacity. Use `updateTriggers`,
    not a new layer.
  - **The selected bus** gets a larger outlined ring.
- **Shadcn/ui:** `pnpm dlx shadcn@latest init`, then add `tabs`, `toggle-group` and `button`
  `[VERIFY: shadcn CLI support for Tailwind v4 + Next 16 + React 19.3]`. Theme them with the
  tokens; nothing keeps shadcn's default palette.

### 2.3 Panels and interactions

- **Agora** (default tab). These are counts from the snapshot, computed in pure functions
  (`hooks/use-fleet-stats.ts`):
  - **vehicles by mode** ("2.401 ônibus · 226 BRT");
  - **em movimento:**
    - the share with `speed_kmh ≥ 3` among vehicles **with** a speed;
    - "sem velocidade: N" shown next to it, never folded into "parado" (missing ≠ zero);
    - **median speed** of the moving ones, in km/h, with 1 decimal;
  - **"Mais ônibus agora":** the 8 lines with the most vehicles, as buttons ("474 · 31") that
    select the line;
  - **coverage:** today's status-strip content (operators and feeds), in a compact form;
  - each number is a `<dl>` pair, so screen readers get label and value together.
- **Linhas tab:**
  - a native `<input type="search">` labelled "Buscar linha", over a list of buttons that
    narrows as you type (no combobox);
  - **matching** is a prefix match on the line value, case-insensitive;
  - **order** is a natural sort ("2" < "10" < "343" < "ESPECIAL"), with BRT lines in their own
    group;
  - each button shows the line, the mode (shape and word) and the vehicle count;
  - vehicles with `line: null` are a sentence at the end ("N veículos sem linha identificada"),
    never hidden;
  - **selecting a line:**
    - the letreiro rolls;
    - the other vehicles dim;
    - the map fits the line's vehicles (padding 64 px, max zoom 15). It fits again only when the
      selection changes, not on refetch, so a user who pans isn't pulled back;
    - the panel shows that line's buses as buttons ordered by vehicle ID, plus a "Limpar linha"
      button.
- **Ônibus tab** (it appears when a bus is selected):
  - **selecting a bus:** click on the map (deck.gl `onClick`) or a button in the line's bus list;
  - **the card shows:**
    - the line, as a small letreiro;
    - the vehicle ID and the operator;
    - the speed, or "sem velocidade";
    - the heading, as an arrow rotated by `heading_deg` plus text ("135°, sudeste"), or "sem
      direção";
    - the age ("visto há 40 s");
  - **Seguir** is a toggle button (`aria-pressed`). While it's on, each new snapshot recentres
    on the bus: `easeTo` for 500 ms, or `jumpTo` with reduced motion. Panning by hand turns it
    off;
  - **if the bus isn't in the newest snapshot**, the card says "Não aparece no último minuto
    gravado (13:24)" and Seguir turns off. No old position is shown as current.
- **Colorir por** is a toggle group: **modo** or **velocidade**.
  - **Speed** uses 5 bins: < 3 km/h, 3–15, 15–30, 30–50 and > 50, on a sequential scale picked
    and validated with the `dataviz` skill. Each bin is ≥ 3:1 against the basemap.
  - **The legend** shows the ranges in text.
  - **`speed_kmh: null`** is drawn as a hollow ring of the same shape and listed in the legend
    as "sem velocidade".
- **Hover tooltip:** kept for the mouse. Click and keyboard both go through the panel.
- **URL state:** `?linha=474&onibus=A29001&cor=velocidade`, kept in one hook,
  `useDashboardParams()`, with `router.replace` and no scroll.
  - Reloading or sharing the URL restores the view.
  - An unknown line or bus is shown as "não está no último minuto gravado", not as an error.
  - `[VERIFY: useSearchParams under cacheComponents needs a Suspense boundary; the Next 16 guide]`
- **Ver como tabela:**
  - it links to `/tabela`, which keeps the same `?linha=`;
  - `/tabela` is today's `VehicleTable` with the tokens applied;
  - the table is the non-map alternative (invariant 7) and stays complete.

### 2.4 Hooks and pure rules (the global "every hook is abstracted" rule)

- **`hooks/use-fleet-stats.ts`:** `fleetStats(rows)`.
- **`hooks/use-lines.ts`:** `toLineIndex(rows)`, `compareLines`, `matchLines`, `useLineIndex()`.
- **`hooks/use-dashboard-params.ts`:** URL state.
- **`hooks/use-selected-vehicle.ts`:** the selected row, or `missing`.
- **`hooks/use-color-scheme.ts`:** a `matchMedia` subscription through `useSyncExternalStore`,
  like `use-now.ts`.
- **One request:** every derived hook reads the existing `latestVehiclesOptions` query through
  TanStack's `select`.
- **Vitest** covers `fleetStats`, `toLineIndex`, `compareLines`, `matchLines` and the
  speed-bin function. The input is a **real snapshot**: `curl /v1/vehicles/latest` from the
  recorded database (`transit_pulse_run`), cut to ~300 vehicles over ≥ 12 lines with nulls, BRT
  and `line: null` kept, saved as `apps/web/hooks/testdata/snapshot-20261012.json`.

### 2.5 Alternatives rejected

- **A drawer library (vaul) for the phone panel:** it's modal by default, and a non-modal drawer
  needs focus and drag handling we'd own. A toggle button does the job.
- **A manual theme toggle:** the system preference is enough. Fewer states to test.
- **Smooth vehicle motion in D1:** that belongs with the time machine (D2), where smoothing is
  labelled. D1 keeps today's snap per refresh.
- **A charting library for D1:** there's no chart yet; D1 is numbers only. D2 brings the one
  chart.

## 3. Why

- **The user wants a beautiful dashboard people play with** (2026-10-12). This is the visible
  part of that, on data that already exists.
- **It fixes the dark-mode contrast failure.**
- **It sets up D2:** the bottom of the screen is free for the timeline, and the selection state
  is in the URL.
- **Cost:** 2 fonts, shadcn (3 components), no API change. About one Sonnet session.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `apps/web/app/layout.tsx` | edit | Archivo + Doto, tokens |
| `apps/web/app/page.tsx` | rewrite | the dashboard |
| `apps/web/app/tabela/page.tsx` | new | the table view |
| `apps/web/app/globals.css` | edit | tokens, light + dark |
| `apps/web/components.json`, `components/ui/*` | new | shadcn CLI output |
| `apps/web/components/letreiro.tsx` | new | the signature |
| `apps/web/components/dashboard.tsx` | new | client: map + panels + URL state |
| `apps/web/components/panel-agora.tsx`, `panel-linhas.tsx`, `panel-onibus.tsx` | new | |
| `apps/web/components/color-legend.tsx` | new | replaces `legend.tsx` |
| `apps/web/components/live-map.tsx`, `live-map-canvas.tsx` | edit | full-bleed, basemap per scheme, blending, dimming, selection, `onClick`, `fitBounds`, follow |
| `apps/web/components/vehicle-style.ts` | edit | colours from tokens, speed bins |
| `apps/web/components/vehicle-view.tsx`, `legend.tsx`, `status-strip.tsx` | edit or remove | folded into the above |
| `apps/web/hooks/*.ts` + tests | new or edit | §2.4 |
| `apps/web/hooks/testdata/snapshot-20261012.json` | new | real, trimmed |
| `apps/web/package.json` | edit | shadcn dependencies (Radix, `class-variance-authority`, …) as the CLI adds them |
| `README.md`, `CLAUDE.md` | edit | stack (shadcn, fonts), layout tree, status |

## 5. Verification

Run against `make api` on `transit_pulse_run`, in `next dev` and in `next build && next start`.

- **D1.1, the numbers:**
  - the Agora counts (by mode, moving share, `sem velocidade`, median speed) and the top 8
    lines equal the same values computed with `jq` from the same snapshot JSON, independently
    of the app;
  - the commands and outputs are recorded in §6.
- **D1.2, the line flow:**
  - type "47" → only lines starting with "47";
  - select one → the letreiro reads "<line> · N ÔNIBUS", N matches `jq`, the others dim, and the
    map fits;
  - pan, wait for a refetch → the map doesn't move;
  - "Limpar linha" restores everything.
- **D1.3, the bus flow:**
  - click a bus → the card matches its row in `/tabela`;
  - Seguir recentres on the next refetch;
  - a manual pan turns Seguir off;
  - a bus ID absent from the snapshot (edit the URL) → the "não aparece" message, with no
    position.
- **D1.4, URL:** reloading `/?linha=…&onibus=…&cor=velocidade` restores the same view, and
  `/tabela?linha=…` shows the same vehicles.
- **D1.5, screens:**
  - screenshots at 1440 × 900 and 390 × 844, dark and light, in each state: default, line
    selected, bus selected, speed colours;
  - saved to `docs/tasks/assets/dashboard-*.png`;
  - no horizontal scroll at 390 px.
- **D1.6, accessibility:**
  - axe reports **0 violations in both schemes** (Playwright `colorScheme`) on `/` and
    `/tabela`;
  - the §2.2 contrast table is filled in;
  - keyboard only: Tab reaches the letreiro text, the tabs, the search, every line and bus
    button, Seguir, the colour toggle, the table link and the map controls, with a visible
    focus ring;
  - with reduced motion: no roll animation, and `jumpTo` instead of `easeTo`;
  - the status announcement still fires exactly once per change (`TASK-live-map.md` B7).
- **D1.7, performance:**
  - with all ~2,600 vehicles and the glow on, record the frame rate while panning, median of 3
    runs of 10 s each (Chrome performance panel or `stats.js`), on the Mac;
  - below 30 fps, record it and drop the glow before anything else.
- `make lint`, `make test` (Vitest) and `make web-build` pass. Commit (no `Co-Authored-By`).

## 6. Results

*(Filled in by the executing session.)*
