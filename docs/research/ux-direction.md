# UX direction: a dashboard people want to play with

> Written 2026-10-12, after the first screen (`TASK-live-map.md`) shipped. **Revised the same day**
> after the user said what they want: *"this is a learning project, idc about the current gtfs, i
> just wanted to have a great beautiful dashboard people could play"*.
>
> The first version of this document proposed a rider tool (a Moovit-like line page) plus an
> accountability layer (promised versus real headways). Both are dropped as the goal:
> `TASK-line-page.md` is deferred and `TASK-ui-lines.md` is superseded. The research in §2 stays
> because it still explains why we don't compete with Moovit.
>
> **The goal now:** a beautiful, explorable view of Rio's buses moving, built on real data. A
> visitor should want to click around, scrub through time and press play.

## 1. What the current screen gets wrong

- **It's a debugging view, not a dashboard.** A small framed map, a 2,627-row table and a status
  paragraph. The map, which is the interesting part, takes a third of the screen.
- **It's broken in dark mode.** The grey secondary text assumes a white background, so in the
  user's screenshot it is nearly invisible. That's a WCAG 1.4.3 failure that the axe run missed
  because it only ran in light mode.
- **There's nothing to play with.** It has one filter box and a hover tooltip, and it only ever
  shows the current minute, even though we hold every minute of the run.

## 2. Background research (riders' apps), kept for context

| App | Leads with | Notes |
|---|---|---|
| **Moovit** (covers Rio) | trip search | arrivals, trip planner, navigation, alerts, crowdsourcing ([Moovit blog](https://moovit.com/blog/navigating-with-moovit/amp/)) |
| **Transit** | nearby departures in each line's own colour | ([MacStories](https://www.macstories.net/reviews/transit-is-still-the-best-designed-transit-app-on-the-iphone-in-2024/), [Pratt](https://ixd.prattsi.org/2026/02/design-critique-transit/)) |
| **Citymapper** | "Get me somewhere" | ([Pratt](https://ixd.prattsi.org/2026/09/design-critique-citymapper/)) |

Rio also has Cittamobi and Quicko, both with official partnerships
([Portal do Trânsito](https://www.portaldotransito.com.br/noticias/mobilidade-e-tecnologia/novo-app-no-rio-de-janeiro-ajuda-os-mais-de-67-milhoes-de-moradores-e-turistas-no-transporte-publico/amp/)).
Rider utility is a solved, crowded space, which is one more reason to build something people
*explore* rather than something they commute with.

## 3. The direction: "Rio em movimento"

**The subject:** 2,600 buses moving through one city, recorded minute by minute.
**The audience:** someone opening a portfolio link.
**The page's one job:** make them watch the city move, then let them dig in.

### 3.1 Visual identity

- **The signature is the *letreiro*,** the amber LED dot-matrix destination sign on the front of
  every Rio bus. Line numbers, the clock and the headline count are set in a dot-matrix face,
  amber on a dark panel, as on a real sign.
  - **Letreiro panels are dark in both colour schemes,** because real signs are.
  - **This is the one aesthetic risk,** and it's spent in that one place only.
  - Everything else stays quiet.
- **The map is the hero.** It's full-bleed, under floating panels. In dark mode the basemap is a
  dark, blue-green "Guanabara at night" tint, and the buses glow on it like city lights
  (additive blending).
- **Type:**
  - **Doto** (Google Fonts, dot-matrix) is used only for letreiro text.
  - **Archivo**, from Omnibus-Type (a foundry literally named after the bus), is used for
    everything else. Its width axis gives a condensed cut for labels, and tabular figures are
    used for numbers.
  - `[VERIFY: both are on Google Fonts and load through next/font/google; Archivo's wdth axis
    range]`
- **Palette** (named, exact values set and contrast-checked in `TASK-dashboard.md`):

| Name | Role | Dark | Light |
|---|---|---|---|
| Baía | page and map base | `#0B1F26` | `#EEF2F1` |
| Morro | panels | `#12303A` | `#FFFFFF` |
| Calçadão | text | `#E8F0EE` | `#0B1F26` |
| Neblina | secondary text | `#8FA9AE` | `#4A6066` |
| Letreiro | LED amber, letreiro panels only | `#FFB341` on `#07161B` | the same: the panel stays dark |
| Céu / Coral | bus / BRT marks | `#7FD4FF` / `#FF6B5A` | `#0A6FA8` / `#C2412F` |

- **Final values:** the text colours above passed as written. The rest (panel edge, focus ring, the five
  speed bins, status markers, the dark basemap tint) were set and measured in `TASK-dashboard.md` §6.
- **Why this isn't a default look:** it's neither cream-and-serif nor black-with-one-neon. The
  base is the bay's blue-green, and the amber comes from an object every carioca sees every day.

### 3.2 What there is to play with, in order of delivery

1. **D1 `TASK-dashboard.md`: the dashboard.** Web only, on the data the API already serves.
   - A full-bleed map with a letreiro header: "13:24 · 2.627 ÔNIBUS NA RUA".
   - An **Agora** panel: counts by mode, the share moving, the median speed, the busiest lines.
     Each line can be clicked.
   - **Pick a line:** the letreiro rolls to "474", the other buses dim, and the map fits the
     line's buses.
   - **Click a bus:** a card with its line, speed, heading and age, plus "Seguir", which follows
     it on each refresh.
   - **Colour the buses by** mode or by speed: real `velocidade`, with null kept distinct.
   - **On a phone,** a bottom sheet.
   - **State in the URL** (`?linha=474&onibus=A29001&cor=velocidade`), so a view can be shared.
   - The table stays, as "Ver como tabela" (invariant 7).
2. **D2 `TASK-time-machine.md`: scrub and play the last hours.**
   - A timeline along the bottom, whose track is a chart of vehicles per minute. Failed minutes
     are drawn as visible gaps, never smoothed (invariant 6).
   - Play at 1×, 10× or 60×, with fading trails.
   - "AO VIVO" jumps back to now.
   - The history format is the same one the static replay (architecture §7) will use, so the
     replay days later reuse the screen unchanged.
3. **D3 `TASK-line-routes.md`: line routes on the map.** Static GTFS shapes and stops are drawn for the
   selected line. A stale GTFS is fine for that (user, 2026-10-12). Nothing measures headways.
   User decision (2026-10-13): export static per-line files. Destination variants and ordered
   stop lists stay available in replay without querying the live backend.
4. **D4 (later): the speed map.** A hexagon layer of median speed over the last 15 minutes, or
   over the played window: "where Rio is stuck right now".

### 3.3 Honesty, kept (the invariants, not the old product)

- **Smoothed motion is labelled.** Trails and motion in D2 are smoothed between real fixes about
  1 minute apart. The screen says so ("movimento suavizado entre posições reais"), and "Só
  posições reais" switches smoothing off (invariant 1).
- **Gaps stay visible.** A vehicle that goes unseen for more than 5 minutes breaks its trail and
  is never bridged.
- **Replay never looks live.** The letreiro reads "REPLAY 12:47" whenever the view isn't live,
  and the status strip says it.
- **Colour never works alone.** Mode is also a shape. Speed has a legend with numbers, and "sem
  velocidade" is drawn hollow.
- **Accessibility holds.** Keyboard reaches everything, the table alternative stays, nothing
  autoplays, and reduced motion turns trails and camera animation off.

## 4. Dropped or deferred

| Item | Status |
|---|---|
| Rider line page with direction inference and gaps (`TASK-line-page.md`) | deferred; not planned |
| Promised × real headways, line rankings | dropped as a goal; the GTFS's freshness doesn't matter (§10 #12 closed as "not needed") |
| ETAs, trip planner, alerts | dropped |
| Emailing SMTR for a current GTFS | not needed |
