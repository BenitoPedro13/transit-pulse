# TASK-dashboard (D1)

> **Done 2026-10-12 (results in §6).** Written 2026-10-12. The first step of `docs/research/ux-direction.md` §3 ("Rio em movimento").
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

Done 2026-10-12. Verified against `make api`-equivalent on `transit_pulse_run` (the API was already
serving it on `:8080`: 2,627 vehicles, newest minute 2026-10-12T16:24Z = 13:24 in Rio), in `next dev`
while building and in `next build && next start` for every number below. The browser was system Chrome
driven by Playwright (`playwright-core`, `axe-core` 4.14.0) from a scratch project outside the repo;
the Claude-in-Chrome extension wasn't needed. The screenshots, axe and the flow scripts ran with
software GL (SwiftShader, as in `TASK-live-map.md` §7), so their timings are slow; the frame-rate runs
(D1.7) ran on the real GPU (`ANGLE (Apple, ANGLE Metal Renderer: Apple M2)`).

### `[VERIFY]` items resolved

- **shadcn CLI 4.21.4** supports Tailwind v4 + Next 16.4 + React 19.3. `shadcn init` prompts for a
  preset even with `-y`; the command that works non-interactively is
  `pnpm dlx shadcn@latest init -t next -b radix -p nova -y --no-monorepo --no-rtl`, then
  `shadcn add tabs toggle-group` (`toggle` came as a dependency of `toggle-group`; `button` from init).
  This CLI imports `cn` from a package named `cn` (0.4.0, repository `shadcn-ui/cn`, a clsx +
  tailwind-merge replacement), not from clsx/tailwind-merge. Added to `package.json`:
  `class-variance-authority`, `cn`, `lucide-react`, `radix-ui`, `shadcn`, `tw-animate-css`. The four
  generated components were then re-themed by hand onto the tokens (no `dark:` variants, no default
  palette).
- **Tailwind v4:** `@theme inline` maps `--color-*` and `--font-*` to the CSS variables; it works as
  planned. Dark is `@media (prefers-color-scheme: dark)`; shadcn's `dark:` variant is redefined with
  `@custom-variant dark (@media (prefers-color-scheme: dark))`.
- **Fonts on Google Fonts / `next/font`:** Archivo has `wdth` 62–125 and `wght` 100–900; Doto has
  `wght` 100–900 and `ROND` 0–100 (only `wght` is used). Both load through `next/font/google`
  (`axes: ["wdth"]` for Archivo, `weight: "variable"` for Doto). The condensed cut is `font-stretch: 75%`.
- **Protomaps `Flavor` keys** (`@protomaps/basemaps` 5.7.2, `Flavor` is exported): the overridden ones
  are `background`, `earth`, `water`, `buildings`, `park_a/b`, `wood_a/b`, `scrub_a/b`, `minor_a/b`,
  `other`, `minor_service`, `major`, `link`, `highway` and six label colours (dark); `background`,
  `earth`, `water`, `buildings`, parks, woods, scrubs and `minor_a` (light). The file is
  `components/basemap.ts`.
- **deck.gl 9.4 / luma.gl 9.4.2 blending:** the layer's `parameters` take `blend`,
  `blendColorOperation: "add"`, `blendColorSrcFactor: "src-alpha"`, `blendColorDstFactor: "one"`,
  `blendAlphaOperation: "add"`, `blendAlphaSrcFactor: "one"`, `blendAlphaDstFactor:
  "one-minus-src-alpha"` (`@luma.gl/core/dist/adapter/types/parameters.d.ts`).
- **`useSearchParams` under `cacheComponents`:** the Next 16.4 guide
  (`use-search-params.md`, "Prerendering") says the client tree up to the nearest `<Suspense>` renders in
  the browser, and recommends a boundary. `app/page.tsx` and `app/tabela/page.tsx` have one; the build
  prerenders both routes as static (`○`).

### Tokens, with every pair measured

Checked with a WCAG 2.x relative-luminance script (`scratchpad/contrast.mjs`, `final.mjs`), not by eye.
Basemap colours are the overrides in `components/basemap.ts`: dark earth `#0e252d`, water `#081a21`,
buildings `#0a1c22`, parks `#12322f`, background `#0b1f26`; light earth `#e7edeb`, water `#b9dce3`,
buildings `#d5dedc`, parks `#bfdccb`, background `#dde6e4`. "Worst" is the lowest of those five.

| Pair | Foreground | Background | Ratio | Needs | Result |
|---|---|---|---|---|---|
| dark: calçadão on baía | `#e8f0ee` | `#0b1f26` | 14.64 | ≥ 4.5 | ok |
| dark: calçadão on morro | `#e8f0ee` | `#12303a` | 12.01 | ≥ 4.5 | ok |
| dark: neblina on baía | `#8fa9ae` | `#0b1f26` | 6.83 | ≥ 4.5 | ok |
| dark: neblina on morro | `#8fa9ae` | `#12303a` | 5.60 | ≥ 4.5 | ok |
| light: calçadão on baía | `#0b1f26` | `#eef2f1` | 15.03 | ≥ 4.5 | ok |
| light: calçadão on morro | `#0b1f26` | `#ffffff` | 16.96 | ≥ 4.5 | ok |
| light: neblina on baía | `#4a6066` | `#eef2f1` | 5.90 | ≥ 4.5 | ok |
| light: neblina on morro | `#4a6066` | `#ffffff` | 6.65 | ≥ 4.5 | ok |
| letreiro text on its panel (both schemes) | `#ffb341` | `#07161b` | 10.33 | ≥ 4.5 | ok |
| letreiro text on a lit dot of the grid (worst case) | `#ffb341` | `#2a2c20` | 7.96 | ≥ 4.5 | ok |
| status marker, dark live | `#5fd39a` | `#12303a` | 7.47 | ≥ 3 | ok |
| status marker, dark stale | `#ffb341` | `#12303a` | 7.80 | ≥ 3 | ok |
| status marker, dark failed | `#ff8a7a` | `#12303a` | 6.07 | ≥ 3 | ok |
| status marker, light live | `#1b7f55` | `#ffffff` | 4.98 | ≥ 3 | ok |
| status marker, light stale | `#8a5a00` | `#ffffff` | 5.93 | ≥ 3 | ok |
| status marker, light failed | `#b3342a` | `#ffffff` | 6.10 | ≥ 3 | ok |
| dark: panel edge (--borda) vs earth/water/buildings/park/background, worst | `#5a808b` | — | 3.21 | ≥ 3 | ok |
| dark: panel edge on panel | `#5a808b` | `#12303a` | 3.24 | ≥ 3 | ok |
| dark: focus ring vs basemap, worst | `#ffb341` | — | 7.73 | ≥ 3 | ok |
| dark: focus ring on panel | `#ffb341` | `#12303a` | 7.80 | ≥ 3 | ok |
| light: panel edge (--borda) vs earth/water/buildings/park/background, worst | `#5c7279` | — | 3.46 | ≥ 3 | ok |
| light: panel edge on panel | `#5c7279` | `#ffffff` | 5.07 | ≥ 3 | ok |
| light: focus ring vs basemap, worst | `#0a5c8c` | — | 4.90 | ≥ 3 | ok |
| light: focus ring on panel | `#0a5c8c` | `#ffffff` | 7.19 | ≥ 3 | ok |

**Marks against the basemap** (worst of the five colours; every one is on parks, the darkest-contrast
land in dark and the most saturated in light):

| Scheme | bus | BRT | no speed / selected ring | bin 0 (<3) | bin 1 (3–15) | bin 2 (15–30) | bin 3 (30–50) | bin 4 (≥50) |
|---|---|---|---|---|---|---|---|---|
| dark | `#7fd4ff` 8.38 | `#ff6b5a` 4.93 | `#e8f0ee` 11.90 | `#af711f` 3.42 | `#c78533` 4.49 | `#de9b4b` 5.84 | `#f0b370` 7.48 | `#ffcd97` 9.48 |
| light | `#0a6fa8` 3.72 | `#c2412f` 3.50 | `#0b1f26` 11.56 | `#a2650a` 3.25 | `#8c5000` 4.39 | `#763c00` 5.92 | `#612900` 7.84 | `#4c1500` 10.12 |

All ≥ 3:1. The speed ramp is one hue (OKLCH hue 68°, amber, the letreiro's colour): faster is lighter
on dark and darker on light, adjacent steps differ by ΔL 0.068–0.070 (lightness is the encoding, so
the colour-blind-pair check doesn't apply to a one-hue ramp). Against the dark roads (`#143038`,
`#183843`, `#1d4350`) bin 0 is 3.4 / 3.1 / 2.6: roads are thin lines, not a dominant land colour, and
the white-outline halo of the old layer is gone (see deviations), so the bin 0 mark over a highway
casing is the one pair below 3:1; the 12 px mark is never fully inside a 2 px line.

**The glow.** In the dark scheme with "colorir por modo" a mark is drawn at alpha 190/255 with additive
blending, so its colour on the dark earth is the mark colour × 0.745 + the map: bus worst 7.84:1, BRT
worst 4.67:1 (on parks), both ≥ 3:1. The first version drew at alpha 1 and every dense corridor
saturated to white, which hid the shapes and the colour.

### Verification

- **D1.1 the numbers ✅.** `jq` on the full snapshot (`curl /v1/vehicles/latest`, 2,627 vehicles),
  independent of the app: bus 2,350, BRT 277, with speed 2,627, moving (≥ 3 km/h) 1,120, no speed 0,
  share 42.63 %, median speed of the moving 29 km/h, top 8 `GARAGEM·47 483·28 ESPECIAL·23 10·21 639·20
  0·19 17·19 328·19`. The Agora panel reads `2.350`, `277`, `43%` with `sem velocidade: 0`, `29,0 km/h`
  and the same eight buttons in the same order (`verify.mjs`, printed PASS).
- **D1.2 the line flow ✅.**
  - "47" lists 472, 473, 474, 476 (`jq`: the same four).
  - Selecting 483 gives the URL `?linha=483`, the sign `483 · 28 ÔNIBUS` (`jq`: 28), the other vehicles
    at 15 %, and the map fitted to the line (`docs/tasks/assets/dashboard-linha-*`).
  - After a drag, two refetches (65 s) left the screenshot byte-identical: the map isn't pulled back.
  - "Limpar linha" returns to `/` and `13:24 · 2.627 ÔNIBUS NA RUA`.
  - Click to sign update: 16 ms (the URL goes through `router.replace`; no lag, so no optimistic copy).
- **D1.3 the bus flow ✅.**
  - A click on the map selects a bus (deck.gl `onClick`: `/?onibus=B31091`); the panel's bus list does
    the same.
  - The card for B31029 reads line 483, `0 km/h`, `0°, norte`, `visto há 10 s`, operator `Ônibus
    municipais (SPPO)`. Its row in `/tabela` is `483 · B31029 · Ônibus · Ônibus municipais (SPPO) · 10 s ·
    0 km/h · 0°`, and `jq` gives `speed_kmh 0, heading_deg 0, event_time 16:24:50Z` (end of the minute
    16:25:00 − 16:24:50 = 10 s).
  - **Seguir:** a drag turns it off (`aria-pressed` true → false). Recentring: with Seguir on, the bus's
    ring sits at (548, 462), the centre of the free part of the map (desktop padding left 64, right
    408, top 96, bottom 72), 1.5 s after the click, and again after a refetch. **That refetch was
    served with one bus moved +0.04° lon / +0.02° lat by a Playwright route in the test browser only**,
    because the recorded data never changes between refetches. The app never shows that position;
    nothing synthetic is stored or drawn outside the test.
  - `/?linha=ZZZ&onibus=ZZ9`: the sign reads `ZZ9 · NÃO APARECE`, the card says "Não aparece no último
    minuto gravado (13:24).", Seguir is disabled, and the Linhas tab says "A linha ZZZ não está no último
    minuto gravado." No position is shown.
- **D1.4 URL ✅.** Loading `/?linha=483&onibus=B31029&cor=velocidade` restores the line, the Ônibus tab,
  the card and the "velocidade" toggle. The "Ver como tabela" link goes to `/tabela?linha=483`, whose
  caption is "28 veículos da linha 483 no último minuto gravado" with every row on line 483.
- **D1.5 screens ✅.** 16 screenshots (2 viewports × 2 schemes × default, line, bus, speed) are in
  `docs/tasks/assets/dashboard-<state>-<viewport>-<scheme>.png`, from the production build. No
  horizontal scroll in any of them at 390 px, nor on `/tabela` at 390 px.
- **D1.6 accessibility ✅.**
  - **axe-core 4.14.0**, tags `wcag2a wcag2aa wcag21aa wcag22aa`: **0 violations in all 24 runs** =
    2 viewports (1440 × 900, 390 × 844) × 2 schemes (Playwright `colorScheme`) × 6 states (`/` Agora,
    `/` Linhas, `/` with a bus and speed colours, `/` with an unknown bus, `/tabela`, `/tabela?linha=483`).
  - Each run reports 1 "incomplete" rule, `color-contrast`, which axe couldn't decide. Their causes,
    listed by axe: the letreiro's text over its dot-grid gradient (measured above: 7.96:1 on a lit
    dot), the `◐` status glyph (`aria-hidden`, decorative), and, only on the phone `/tabela`, table
    headers scrolled out of the scroll area ("partially obscured").
  - **Keyboard only** (Tab, dark and light): letreiro → "Ver como tabela" → the tabs → the panel (every
    top-line button; on the Linhas tab "Limpar linha", every bus button, the search, every line button;
    on the Ônibus tab Seguir and "Limpar ônibus") → the colour toggle → the map → zoom in → zoom out →
    attribution. Every stop has a 2 px solid ring (`#ffb341` dark, `#0a5c8c` light). The map is last in the
    source, so the page's own controls come first. The group of tabs and the colour toggle are roving
    (arrow keys), as Radix does it.
  - **Reduced motion:** with `prefers-reduced-motion: reduce` no outgoing aria-hidden copy appears in the
    sign and no animation runs on it (normal motion: the copy appears and `animate-roll-*` runs for
    300 ms); Seguir lands on the bus within the first screenshot (578 ms, ring at (548, 462)) where
    normal motion is still mid-ease at 721 ms ((669, 397)): `jumpTo` vs `easeTo`.
  - **Status announcement exactly once per change:** a `MutationObserver` on the `role="status"`
    region saw `["Não está ao vivo."]` after load and two refetches (0 further changes); one refetch
    served as a failed feed (a test-only route that rewrote `feeds_by_status`) added exactly one
    (`Atenção: falha…`) and the recovery exactly one more.
- **D1.7 performance ✅.** All 2,627 vehicles, 1440 × 900, panning by a continuous mouse drag for 10 s,
  frames counted with `requestAnimationFrame`, median of 3 runs, real GPU (Apple M2, Chrome headless):

  | Configuration | Runs (fps) | Median |
  |---|---|---|
  | dark, modo, glow on | 59.9, 59.9, 59.8 | **59.9** |
  | dark, velocidade (no glow) | — | 59.6 |
  | light, modo | — | 59.9 |
  | dark, modo, line 483 selected (dimming) | — | 60.0 |

  60 fps is the display's cap, so the glow costs nothing measurable and stays.
- `make lint`, `make test` (all Go packages and **51 Vitest tests in 3 files**) and `make web-build`
  pass.

### Where this differs from the plan

1. **No white halo around the marks.** The old layer needed it against dark map features. Every mark is now
   ≥ 3:1 against the land and water on its own (table above), so the atlas holds five white shapes tinted
   per vehicle (mask icons): filled circle, filled square, their hollow rings, and the selected bus's large
   ring.
2. **The glow is for "colorir por modo" only.** In "velocidade" the dense corridors turned yellow-white and
   the five bins stopped being tellable apart, so that mode uses normal blending in both schemes.
3. **The ≥ 50 bin is half-open** `[50, ∞)`, not "> 50", so the bins never overlap (`<3`, `[3,15)`,
   `[15,30)`, `[30,50)`, `≥50`). The legend says "50 ou mais".
4. **Colour control and legend** are the panel's footer on a phone (so they stay visible when it's
   collapsed) and float at the bottom left on a desktop. The collapsed phone panel is up to 33 % of the
   height, not 25 %, to fit them.
5. **Map controls moved:** the zoom buttons sit left of the panel on a desktop and above it on a phone
   (`--panel-h`), because the panel hid them. "Ver como tabela" sits above the attribution on a desktop so
   it doesn't cover OpenStreetMap's credit.
6. **`/tabela?linha=` is the exact line** the dashboard has selected, so the table shows the same
   vehicles; the "Filtrar por linha" box (prefix match, not in the URL) narrows it further. The sign
   "Mostrando só a linha 483" has a "Mostrar todas as linhas" button.
7. **Selecting a bus doesn't change the line.** The selected bus is always drawn at full opacity, even if
   its line is dimmed; choosing a line drops a selected bus.
8. **Only a drag turns Seguir off**, not a wheel zoom, the zoom buttons or the keyboard.
9. **Refetch interval is 30 s** (the task text says 15 s; the code already was 30 s and the data changes
   once a minute).
10. **Names:** the shared request is `snapshotQueryOptions` (no `select`); `latestVehiclesQueryOptions`
    keeps its `select` for the table. Every derived hook (`useFleetStats`, `useLineIndex`) reads
    `snapshotQueryOptions` through its own module-level `select`.
11. **The fixture has 254 vehicles on 19 lines plus 40 with no line** (the task said about 300 on ≥ 12
    lines); it is real rows from the 16:24Z snapshot, cut with `jq` (`hooks/testdata/snapshot-20261012.json`,
    44 KB).
12. **Extra files:** `hooks/map-padding.ts`, `media-query.ts`, `use-is-desktop.ts`, `use-reduced-motion.ts`,
    `heading.ts`, `letreiro-text.ts`, `speed-bins.ts`, `dashboard-url.ts`, `components/basemap.ts`,
    `components/tabela-view.tsx`; `vitest.config.mts` also resolves `@/` and includes
    `components/**/*.test.ts` (the test that fails if `vehicle-style.ts` and `globals.css` drift apart).

### Facts about the data this surfaced

- **No `speed_kmh` is null in the recorded run** (0 of 1,099,188 rows). The "sem velocidade" branch (a
  hollow ring, `sem velocidade: N`, outside the moving share) is real code the data never exercised, so
  its unit tests use real rows with the speed removed, and say so.
- **20 % of the newest minute has `line: null`** (524 of 2,627). They're a sentence at the end of the
  Linhas list, not a line.
- **`GARAGEM` (47) and `ESPECIAL` (23) are values of the provider's `line` field** and show up as the
  busiest "lines" in Agora. The screen shows them as the data says; whether to treat them as lines is a
  product call for later.
- **A line is one mode** in the whole snapshot (checked by a test), and vehicle IDs are unique across
  operators in it, so `?onibus=` takes the bare ID.
- **`next dev`'s "N" badge** overlaps the legend at the bottom left on a desktop. It's dev-only; the
  production build doesn't have it, and the screenshots come from the production build.

### Commands

```
pnpm dlx shadcn@latest init -t next -b radix -p nova -y --no-monorepo --no-rtl
pnpm dlx shadcn@latest add tabs toggle-group
pnpm --dir apps/web build && pnpm --dir apps/web start     # then Playwright + axe on :3000
make lint && make test && make web-build
```
