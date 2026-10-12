# UX direction: from "every bus at once" to something a rider uses

> Written 2026-10-12, after the first screen (`TASK-live-map.md`) shipped. This is research and a
> proposal. **Nothing here is decided until the user picks the options in §7.** The chosen
> direction becomes task documents. It replaces the phase 1 deliverable "delays by line"
> (architecture §4), which was never specified as a screen.

## 1. What the current screen answers

It's 2,627 dots, a table sorted by line, and a feed status. That answers an **operator's**
question ("where is the fleet right now?"). It answers none of the questions riders actually
ask:

- **No entry point a rider would use.** People in Rio name a line ("o 474") or a place ("perto de
  casa"), not "the fleet". The filter is a text box under a 2,627-row table.
- **No route, no stop, no direction.** A dot on a street doesn't say which way it's going, how far
  it is from *my* stop, or whether there's another one behind it.
- **No notion of "good" or "bad".** The data can show that line X promised a bus every 10 min
  and delivered a 35-min gap. The screen doesn't show it.
- **Desktop-only layout.** A rider checks a bus on a phone, at a stop.

The pipeline underneath is the valuable part, and it's fine. What's missing is the product layer.

## 2. What riders' apps do (researched 2026-10-12)

| App | Home screen leads with | Core features |
|---|---|---|
| **Moovit** (covers Rio) | trip search | live arrival estimates from vehicle GPS; trip planner with total time and transfers; step-by-step navigation with a "get off" alert; service alerts; crowdsourced reports; favourite lines; ticketing in some cities ([Moovit blog](https://moovit.com/blog/navigating-with-moovit/amp/), [app listing](https://mwm.ai/>/apps/moovit-bus-transit-tracker/498477945)) |
| **Transit** | **nearby departures**: every line around you with its wait time, in the line's own colour | "where to?" search; nearby cards ([MacStories](https://www.macstories.net/reviews/transit-is-still-the-best-designed-transit-app-on-the-iphone-in-2024/), [Pratt critique](https://ixd.prattsi.org/2026/02/design-critique-transit/)) |
| **Citymapper** | **"Get me somewhere"**, plus Home and Work shortcuts | routing compared by speed, cost and transfers; departures are a tap away ([Pratt critique](https://ixd.prattsi.org/2026/09/design-critique-citymapper/)) |

**Rio has its own crowd** (news sources of mixed age; `[VERIFY: current app-store listings before
quoting any of these publicly]`):

- **Cittamobi:** GPS tracking and arrival forecasts at a stop, launched in Rio with Rio Ônibus and
  the city ([Portal do Trânsito](https://www.portaldotransito.com.br/noticias/mobilidade-e-tecnologia/novo-app-no-rio-de-janeiro-ajuda-os-mais-de-67-milhoes-de-moradores-e-turistas-no-transporte-publico/amp/)).
- **Quicko:** SMTR alerts, positions, arrivals and rider reports.
- **Moovit:** described above.

**Conclusion: arrival predictions and trip planning are already covered in Rio, by funded teams
with official partnerships.** A portfolio project that rebuilds a worse Moovit is a weak case
study. Trip planning in particular needs a routing engine and a current GTFS, and ours is stale
(research finding 3).

## 3. What nobody shows riders, and we can

Accountability tools exist elsewhere:

- [TransitMatters](https://transitmatters.org/s/TransitMatters-Headwaymanagement.pdf) (Boston)
  publishes bunching rates per stop. One example: outbound buses on the 66 bunched over 35% of
  the time at one stop.
- [TransSee](https://www.masstransitmag.com/technology/miscellaneous/press-release/21209270/transsee-transsee-adds-new-charts-that-show-transit-operations)
  turns public vehicle APIs into headway and wait-time charts.
- Human Transit [argues](https://humantransit.org/?p=28275) that bunching is invisible on a
  normal map, and that a route should be drawn as a line with its buses placed along it.

**I found no rider-facing equivalent for Rio.** `[VERIFY: a quick search for "intervalo real
ônibus Rio" dashboards, e.g. from SMTR's own data portal or Casa Fluminense, before claiming
"first"]`

We hold exactly what that needs:

- **The promise.** 99.5% of SMTR's GTFS trips are `frequencies.txt` headways (research finding
  4), e.g. "every 12 min, 06:00–09:00". That's a published promise in minutes.
- **The reality.** Every vehicle, every minute, from every supplier, kept forever in the raw log.
- **The honesty layer.** Live, stale, failed and no-data are already distinct (invariant 6).

**So the angle is: "o ônibus prometido × o ônibus real" (the promised bus × the real bus).** The
site is useful today ("where's my bus, and is another one behind it?") and has something to say
over time ("this line promises 10 min and delivers 24").

## 4. The proposed product

Three screens, mobile-first. The line is the hub, because that's how riders think.

### 4.1 Home: search, and what's near me

```
┌──────────────────────────────┐
│ Transit Pulse                │
│ ● Ao vivo · atualizado 13:24 │
│ ┌──────────────────────────┐ │
│ │ 🔍 Linha ou lugar (ex. 474)│ │
│ └──────────────────────────┘ │
│ [ Perto de mim ]             │  ← geolocation only when tapped
│                              │
│ Suas linhas                  │  ← favourites in localStorage
│  474 Jacaré–Copacabana  ▸    │
│  ↳ 6 ônibus na rua · maior   │
│    intervalo agora 28 min ⚠  │
│                              │
│ Piores intervalos agora      │  ← the accountability hook
│  1. 393  prometido 10 · real 41 min
│  2. 565  …                   │
└──────────────────────────────┘
```

### 4.2 The line page (the core)

```
┌──────────────────────────────┐
│ ◂ 474  Jacaré → Copacabana   │
│ [ Ida ] [ Volta ]            │
│                              │
│ Agora: 6 ônibus · prometido  │
│ a cada 12 min (06–09h)       │
│                              │
│ Jacaré                       │  ← the route as a vertical strip (stops
│  ●  Pç. Jacaré               │     from GTFS), each bus placed by its
│  │                           │     distance along the shape
│  🚌 A29001 · há 40 s         │
│  │ ┄┄ 4 min ┄┄               │  ← gap between buses, in time
│  🚌 A29014 · há 1 min        │
│  │                           │
│  │ ┄┄ 28 min ┄┄  ⚠ buraco    │  ← a gap > 2× the promise, shown with text, not only colour
│  │                           │
│  ●  Central                  │
│  🚌 A29120 · há 20 s         │
│  ●  Copacabana               │
│                              │
│ [ Ver no mapa ]              │  ← the map is secondary here, with this line only
│                              │
│ Hoje vs prometido            │
│  espera média real 11 min    │
│  (prometido ≤ 6)             │
│  ônibus colados: 18% das passagens
└──────────────────────────────┘
```

- The strip **is** the table alternative (invariant 7): it's an ordered list and reads well in a
  screen reader ("ônibus A29001, entre Pç. Jacaré e Central, visto há 40 segundos; próximo
  ônibus 4 minutos atrás").
- **Tap a stop** → "chegando aqui: 2 ônibus, a 1,2 km e 3,4 km pela rota, vistos há 40 s e 1
  min". Those are observed facts (distance along the shape, age of the fix), not predictions.

### 4.3 The reliability page (the case study's argument)

- Lines ranked by **excess wait**: the observed average wait minus the promised one, by hour and
  by weekday, with the region filter.
- For each line:
  - a headway chart over the day, promise against reality;
  - bunching %;
  - "buses on the street vs. buses the promise needs" (if the data can support it; see §5).
- **Replay** (architecture §7) fits here: "veja o 474 no dia 31/12", with the route strip
  animated (respecting reduced motion: a slider, not autoplay).

### 4.4 What stays

- **The all-vehicles map becomes "A rede agora"**, a secondary page. It's still a good overview
  and a good demo of deck.gl, but it isn't the home page.
- **The status strip, the coverage tiers and "last updated at" stay on every page.** They're the
  project's honesty layer.

## 5. What it takes underneath (feasibility)

| Need | Have | Gap |
|---|---|---|
| Routes, stops, shapes | SMTR static GTFS (494 routes, 7,694 stops, shapes 18.8 MB). Last updated 2026-06-10; `route_id` joins ≈99% and `shape_id` ≈98% | **a GTFS loader** into PostGIS (`routes`, `stops`, `shapes` as linestrings, stop order per shape). Stale `trip_id`s don't matter here. Each loaded feed is versioned and kept, since it's a projection of a raw file |
| Which line a bus is on | `line` (`servico`) and `route_id` in the canonical event | `ESPECIAL`, `LECD…` and `0` don't join (5–15%). They're shown as "sem linha identificada", never guessed |
| Direction (ida/volta) | the raw ITS has `sentido` (`ida`/`volta`/`circular`/`""`) and `shape_id`; **the canonical event v1 carries neither** | **Recommended:** derive direction provider-agnostically, by projecting consecutive fixes onto both shapes of the route and keeping the one the bus moves forward along. That works for every future provider, and "nothing downstream knows the provider" stays true. The alternative is optional `direction`/`shape_id` fields in event v2. Decided in the line-page task |
| Distance along route | lat/lon + shapes | PostGIS `ST_LineLocatePoint` per fix, in the consumer or a projection. That's ≈ 155 fixes/s, which `[VERIFY: measure]` before choosing where it runs |
| Observed headway | positions every minute | the time between consecutive buses passing reference stops. Passing times are interpolated between two observed fixes, which is a derived value, so it's labelled as such (invariant 1: derived ≠ observed). The aggregates (5-min, hourly, daily) are architecture §8's continuous aggregates |
| Promised headway | `frequencies.txt` per trip and time window | joined by `route_id` + direction + time of day. Stale since June `[VERIFY with SMTR: §10 #12]`, so the page says which GTFS version the promise comes from |
| Expected wait | headways | random-arrival wait = Σh² / (2·Σh); the promise's equivalent is h/2. It's the standard measure, and it's honest about bunching (two buses 1 min apart don't halve anyone's wait) |
| ETA "chega em 5 min" | — | **not proposed for now.** It's a prediction, and Cittamobi and Moovit already do it. If ever added, it's labelled "estimativa" and shown next to the observed facts |

## 6. Proposed order

Each item is its own task document. Every step puts something new on screen.

1. **U1: GTFS loader + the line page** (search, the route strip with live buses, the line on the
   map, gaps between buses, mobile-first layout). Usable on the recorded local data.
2. **U2: Home** (search, favourites, near me) **and the stop view** (buses approaching, by
   distance and age).
3. **U3: Reliability** (continuous aggregates, promise × reality, line ranking). It needs days
   of live data, so it comes **after `TASK-aws-infra.md`** has recorded some. This is phase 1's
   "delays by line", made concrete.
4. **U4: Replay of a line on a chosen day** (with the replay task, architecture §7).

**Rejected for this project:**

| Feature | Why |
|---|---|
| Trip planner | needs a routing engine and a current GTFS; Moovit already does it |
| Ticketing, crowdsourced reports, push notifications | outside a data case study, and they need accounts or a backend write path |
| ETA predictions as the headline | already covered in Rio, and against the "observed first" stance |

## 7. Decisions for the user

1. **Angle:**
   - (a) **both: a rider tool + accountability, with the line page as the hub** *(recommended)*;
   - (b) a rider tool only;
   - (c) accountability only (reliability pages, no stop view).
2. **ETA estimates:** (a) not now *(recommended)*; (b) yes, labelled as an estimate.
3. **Mobile-first** for every new screen, with the current desktop layout reworked in U1: yes or
   no.
4. **Order:**
   - (a) U1 runs **in parallel** with `TASK-aws-infra.md` (separate Sonnet sessions, different
     files) *(recommended)*;
   - (b) the AWS task first, then U1.
5. **Email SMTR** (`infra.smtr@prefeitura.rio`) for the current GTFS (architecture §10 #12). The
   promise side of U3 depends on it. Recommended: yes, now.
