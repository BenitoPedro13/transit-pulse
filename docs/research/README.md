# Research — sources

Verified facts about every data source, each backed by a real captured response in
`samples/`. **Nothing here may come from memory.** A cell is filled in only after it has been
checked against the live source, with the URL linked and the capture date stated.

**Status (2026-10-12): verified** by `docs/tasks/TASK-source-inventory.md`. All captures,
probes and polls ran on 2026-10-12 between 13:18 and 13:41 UTC (10:18–10:41 in Rio), a
Monday. That Monday was a public holiday: the GTFS `calendar_dates.txt` marks 2026-10-12 as a
Sunday-service day (`D_REG` added, `U_REG` removed). The measured volumes are therefore a
holiday mid-morning, not a weekday peak (see "Raw volume"). Anything not confirmed stays
`[VERIFY: …]`, with what was tried.

## Source inventory

Tiers: `live` = vehicle positions we can poll; `scheduled` = a machine-readable static
timetable (GTFS) only; `none` = no public machine-readable data found.

| Operator | Mode | Run by | Tier (verified) | Source URL | Auth | Update interval (measured) | Format | Verified? |
|---|---|---|---|---|---|---|---|---|
| Rio city buses (SPPO) | bus | city (SMTR) | **live** | [ITS API](#1-smtr-live-vehicle-positions-sppo-sistema-rio-brt), [GTFS-RT](#12-gtfs-realtime-vehicle-positions-beta), [legacy](#13-legacy-endpoints-dadosmobilidaderiogps) | none | a ping every ~30 s per bus (p50 30 s) | JSON / GTFS-RT protobuf | **yes, 2026-10-12** |
| Rio city buses ("Sistema RIO") — *new row* | bus | city (SMTR) | **live** | ITS API (`sistema=rio`), GTFS-RT | none | a ping every ~5 s per bus (p50 5 s) | JSON / GTFS-RT protobuf | **yes, 2026-10-12** |
| BRT Rio (MOBI-Rio) | brt | city (agency `20001` MOBI-Rio in SMTR's GTFS) | **live** | ITS API (`modo=brt`), GTFS-RT, legacy `/gps/brt` | none | a new fix every 15–30 s per moving bus | JSON / GTFS-RT protobuf | **yes, 2026-10-12** |
| SuperVia → **TrensRJ** | rail | state (AGETRANSP concession) | **none** | operator site only: <https://www.trensrj.com.br/pt> | — | — | HTML timetables | **yes, 2026-10-12** (absence) |
| MetrôRio | metro | state (AGETRANSP concession) | **none** | operator site only: <https://www.metrorio.com.br/> | — | — | HTML | **yes, 2026-10-12** (absence) |
| VLT Carioca | tram | `[VERIFY: concession authority — AGETRANSP doesn't list it]` | **none** (city GIS has VLT stops only) | <https://trilhos.motiva.com.br/vltrio/> | — | — | HTML | **yes, 2026-10-12** (absence) |
| Barcas → **Barcas Rio** | ferry | state (AGETRANSP concession) | **none** | <https://barcasrio.com.br/linhas-horarios-e-tarifas/> | — | — | HTML timetables | **yes, 2026-10-12** (absence) |
| Intercity buses (DETRO-RJ) | bus | state (DETRO-RJ) | **none** | <http://www.detro.rj.gov.br/regulares-tarifas-itinerario/> (an HTML lookup form) | — | — | HTML | **yes, 2026-10-12** (absence) |
| Niterói city buses | bus | city (Niterói) | **none** (route geometry only) | ArcGIS layer "Linhas de Ônibus Transnit" | none | — | ArcGIS Feature Service | **yes, 2026-10-12** |
| Rio static GTFS (city network) | bus + brt | city (SMTR) | **scheduled** | <https://dados.mobilidade.rio/gtfs/schedule> | none | file dated 2026-06-10, unchanged since | GTFS zip | **yes, 2026-10-12** |
| data.rio historical (BigQuery) | bus + brt | city (SMTR) | historical (exists) | `rj-smtr.monitoramento.*`, `rj-smtr.br_rj_riodejaneiro_veiculos.*`, `rj-smtr.datario.*` | **a Google Cloud account is needed to query; not tested** | daily partitions | BigQuery tables | partly: schema and size from the city's own dbt catalog; access `[VERIFY]` |

"Verified (absence)" means every official channel listed under that source was searched and
none publishes machine-readable data. It doesn't prove the data doesn't exist privately.

## ⚠ Findings that contradict or change the plan (for the user to decide)

1. **No official static GTFS for rail, metro, VLT or ferries.** Phase 2 assumes "static GTFS
   for rail and ferries". I searched the Mobility Database catalogue, the Transitland atlas, the
   state open-data portal (CKAN `dadosabertos.rj.gov.br`), AGETRANSP, data.rio/ArcGIS and every
   operator site. Not one of them publishes GTFS for SuperVia/TrensRJ, MetrôRio, VLT or Barcas.
   They publish HTML timetables only. Phase 2 needs a decision: show them as `none`, or
   hand-build a GTFS from the operators' published timetables (it would then be labelled as
   derived by us).
2. **Two suppliers report the same SPPO bus.** In the ITS API, 2,072 vehicle IDs are reported
   by both `conecta` and `zirix`. They are two independent AVL devices with different
   timestamps and positions. 3,114 `(vehicle, datetime)` pairs exist in both suppliers' data,
   and only 1 of them has the same position in both. The identity in architecture §10 #7,
   `(operator, vehicle, event_time)`, would silently drop one of two real observations. The
   options are adding the supplier to the identity, or choosing one supplier per vehicle.
3. **The live system uses a newer GTFS than the public one.** The public static GTFS hasn't
   changed since 2026-06-10, and its `feed_end_date` is **2026-12-31**, inside a 4-month live
   run and on the Réveillon replay day. Live `trip_id`s join to it for only about 44% of buses;
   `route_id` (≈99%) and `shape_id` (≈98%) still join. SMTR's own BigQuery holds 101
   `feed_info` versions, so newer feeds exist but aren't published at that URL.
4. **"Delays by line" can't be measured against a timetable.** 17,275 of the 17,361 GTFS trips
   (99.5%) are defined by `frequencies.txt` with `exact_times=0`, so there are no scheduled
   arrival times. Phase 1's "delays by line" has to be based on headways (observed against
   scheduled intervals).
5. **Every live API is labelled beta.** "May change stability and format without notice"
   (SMTR's own wording). One extra city bus system exists, "Sistema RIO" (supplier
   `maxtrack`), which wasn't in the plan's inventory.
6. **Operator names changed:** SuperVia's site returns 502 and the regulator lists **TrensRJ**;
   the ferries are now **Barcas Rio**. Both are on AGETRANSP's operator list.

---

## 1. SMTR live vehicle positions (SPPO, Sistema RIO, BRT)

The city's transport secretariat (SMTR) publishes **three public, unauthenticated paths** to
the same underlying AVL data, all behind its Kong API gateway. Each one was polled for
about 10 minutes on 2026-10-12 with the User-Agent
`transit-pulse-research/0.1 (one-off source inventory; contact: …)`. All three are
CC-BY-4.0 and **attribution is required**: "Secretaria Municipal de Transportes - SMTR,
Prefeitura da Cidade do Rio de Janeiro - PCRJ". This comes from the ArcGIS items'
`licenseInfo` / `accessInformation`, and Transitland records `use_without_attribution: no`.

Official documentation (ArcGIS Online items owned by `PrefeituraRio`, all public):

- ITS API: item `bc16ed45dda5434999ed3e4e9dbee56f`, a PDF published 2026-09-23. Its
  Swagger page is <https://its.mobilidade.rio/docs>, and its spec is
  <https://its.mobilidade.rio/openapi.json> (OpenAPI 3.1.0, `info.version` **0.1.5-beta**,
  fetched 2026-10-12). It has one data endpoint, `GET /v1/geolocalizacao/veiculos`, with the
  8 optional parameters below and **no security scheme**, plus `/healthz` and `/readyz`.
- GTFS-RT, buses: item `997bc16cc4344a5cb0e48ab06cbdfd1f`, modified 2026-09-24.
- GTFS-RT, BRT: item `8bb196cc02174d25b77437bb2f6443d4`, modified 2026-10-07.

The documentation is readable at
`https://www.arcgis.com/sharing/rest/content/items/<id>?f=json`.

### 1.1 ITS API (beta) — every supplier, per-minute windows

- **URL:** `https://its.mobilidade.rio/v1/geolocalizacao/veiculos`
- **Parameters (documented, and the ones marked † were exercised):**
  - `minuto_utc`† (ISO 8601 with `Z`): returns the positions *received by the server* in
    that UTC minute, filtered on `datetime_servidor`. Confirmed: for minute 13:19 the
    `datetime_servidor` values ran from 13:19:00Z to 13:20:00Z.
  - `limit`† (default 500, max 5000) and `cursor`† (`next_cursor` in the body);
  - `fornecedor`†, `sistema`, `modo`, `id_veiculo`, `servico`.
- **Auth:** none.
- **Documented gateway limits:** 5 requests/s with a burst multiplier of 5; a 15–30 s
  timeout; up to 60 s from reception to availability.
- **Caching headers:** none at all (no `ETag`, `Last-Modified` or `Cache-Control`), and no
  gzip even when `Accept-Encoding: gzip` is sent. Conditional requests are therefore
  impossible. A completed minute is immutable, so there's no need for them.
- **History:** the minutes 1 h, 6 h and 24 h back all returned full data (9,446, 6,044 and
  9,130 records). Re-fetching three minutes about 14 minutes later gave **byte-identical
  rows** (10,933 of 10,933): no late arrivals, no removals. So a missed minute can be
  backfilled for at least 24 h.
- **Body:** `{minuto_utc, data: [...], next_cursor, providers_status: {conecta, maxtrack,
  sonda, zirix: "ok"}}`.
- **Suppliers (median per minute, 11 minutes measured):**

  | `fornecedor` | `sistema` / `modo` | records/min | vehicles/min | ping interval p10/p50/p90 | `datetime_servidor − datetime` p50/p90/p99 |
  |---|---|---|---|---|---|
  | `conecta` | `sppo` / `onibus` | 3,749 | 1,574 | 6 / 30 / 31 s | 12 / 26 / 232 s |
  | `zirix` | `sppo` / `onibus` | 4,719 | 1,865 | 3 / 30 / 31 s | 2 / 25 / 145 s |
  | `maxtrack` | `rio` / `onibus` | 678 | 134 | 1 / 5 / 30 s | 2 / 2 / 36 s |
  | `sonda` | `null` / `brt` | 1,691 | 863 | 15 / 15 / 30 s | — (no `datetime_servidor`) |

- **Fields actually present differ by supplier** (the documentation lists them all):
  - `maxtrack` fills everything: `id_registro`, `id_equipamento`, `sequencial_equipamento`,
    `qualidade_sinal`, `hdop`, `direction_id` and the rest. It's **the only supplier with
    `id_registro`**.
  - `conecta` and `zirix` carry `id_veiculo`, `servico`, `sentido`, lat/lon, `velocidade`,
    `direcao`, `datetime`, `datetime_servidor`, `route_id`, `trip_id`, `shape_id` and
    `sistema`. In practice `zirix` almost never fills `route_id`, `trip_id` or `shape_id`.
  - `sonda` (BRT) carries only `id_veiculo`, `servico`, `sentido`, lat/lon, `velocidade`,
    `direcao`, `datetime` and `route_id` / `trip_id` / `shape_id`, all of which were null in
    the samples. It has no `sistema` and no `datetime_servidor`.
- **`sentido` is `ida` / `volta` / `circular` / `""`**, not the documented `I` / `V` / `C`.
- **`sonda` behaves as a snapshot**, re-served within each minute: 6,149 of 18,135 rows
  were exact duplicates. It includes vehicles that have been parked a long time: the age of
  `datetime` at fetch was p50 168 s, p90 200,101 s (2.3 days), and the maximum was about
  845 M s, an obviously bogus timestamp. **A freshness cut-off is needed.**
- **Volume per minute:** a median of 10,883 records over 3 pages, **6.64 MB raw, ~367 KB
  gzip-compressed** (summed page by page).
- **Request budget:** 3 page requests per minute. That's 0.6% of the documented 5 requests/s.

### 1.2 GTFS-Realtime Vehicle Positions (beta)

- **URLs:**
  - `https://dados.mobilidade.rio/gtfs/realtime/onibus/vehicle-positions` (SPPO and
    Sistema RIO);
  - `https://dados.mobilidade.rio/gtfs/realtime/brt/vehicle-positions`.
- **Auth:** none.
- **Documented limits:** 5 requests/s and 60/min; a 10 s in-memory cache TTL; a 60 s timeout
  with up to 5 internal retries.
- **Caching headers:** `Content-Type: application/x-protobuf`, `Cache-Control: public,
  max-age=30`, plus a strong `ETag` and `Last-Modified`. `X-Cache-Status` was always
  `Bypass`.
- **Conditional requests work:**
  - the BRT feed returned **304 four times** in 21 polls when nothing had changed;
  - the bus feed changed on every 30 s poll, so it never returned 304.
- **Updates:** `header.timestamp` advances in exact 30 s steps for buses. The feed is about
  34 s old when fetched.
- **Format:** GTFS-RT 2.0, `FULL_DATASET`, one `FeedEntity` per vehicle with
  `entity.id == vehicle.id`.
  - Each entity has `trip{trip_id, start_time, start_date, schedule_relationship, route_id,
    direction_id}`, `position{latitude, longitude, bearing, speed}`, `timestamp` (POSIX
    seconds) and `vehicle{id}`.
  - There's no `stop_id`, `current_status` or `occupancy`.
  - Decoded with the field numbers in
    [google/transit `gtfs-realtime.proto`](https://github.com/google/transit/blob/master/gtfs-realtime/proto/gtfs-realtime.proto).
- **Size per poll (median):**
  - buses: 116.7 KB raw / **39.8 KB gzip**, with 862–1,021 entities;
  - BRT: 16.5 KB raw / **4.6 KB gzip**, with 127–135 entities.
- **Coverage is partial: only trip-matched vehicles appear.**
  - Buses: 1,008 entities against 2,202 buses seen by the ITS API in the same minute (46%).
  - BRT: 131 entities against 863 vehicles in the BRT snapshot.
- **Data quality:**
  - 398 of 2,224 BRT `bearing` values are **greater than 360** (e.g. `906.0`); the bus feed
    had none;
  - one bus vehicle in about 10 minutes moved without its `timestamp` changing.
- **GTFS joins (bus feed):** `route_id` 858/862 (99.5%) and `trip_id` 393/862 (45.6%). BRT:
  131/131 for both.

### 1.3 Legacy endpoints `dados.mobilidade.rio/gps/*`

These aren't described in the 2026-09 documentation. They pre-date the ITS API, and the
city's own archived pipeline repo `prefeitura-rio/pipelines_rj_smtr` (archived 2026-08-14)
captured from vendor URLs with credentials, not from these endpoints.

- **`GET https://dados.mobilidade.rio/gps/brt`**: a snapshot,
  `{"veiculos":[{codigo, placa, linha, latitude, longitude, dataHora, velocidade,
  id_migracao_trajeto, sentido, trajeto, hodometro, direcao, ignicao,
  capacidadePeVeiculo, capacidadeSentadoVeiculo}]}`.
  - The body is always gzip-encoded.
  - Rate limits from the headers: 2 requests/s and 60/min.
  - It sends a weak `ETag`, which **changed on all 20 of 20 polls**, so a 304 never happened.
  - `X-Cache-Status` was a Hit 13 times and a Miss 7 times, with an `Age` of 1–11 s.
- **Every one of the 863 registered vehicles appears in every poll**, including stale ones:
  - 4,379 of 17,260 rows (25%) had a `dataHora` more than 1 day old;
  - 183 vehicles had `linha` `"0"` or empty, and 454 had `ignicao: 0`;
  - `dataHora` is epoch **milliseconds**, UTC (`1791811115000` = 13:18:35Z, fetched at
    13:18:53Z).
- **`GET https://dados.mobilidade.rio/gps/sppo?dataInicial=…&dataFinal=…`**: a time-window
  query.
  - Without the parameters it returns HTTP 500, with a .NET error naming
    `ConectaWEB…WSConectaController.EnvioSMTR(…, DateTime dataInicial, DateTime dataFinal, …)`.
  - The format that worked is `YYYY-MM-DDTHH:MM:SS` in **Rio local time**.
  - The window filters on `datetime_servidor`.
  - It returns a JSON array of `{id_veiculo, servico, sentido (I/V/C/""), latitude,
    longitude, velocidade, direcao, route_id, trip_id, shape_id, datetime, datetime_envio,
    datetime_servidor}`.
  - Headers: `Cache-Control: private`, no `ETag`, rate limits of 5 requests/s and 60/min,
    gzip.
- **The SPPO timestamps carry a `Z` but are Rio local time (UTC−3).**
  - A request at 13:19 UTC returned `datetime: "2026-10-12T10:17:44Z"`.
  - 1,412 legacy rows match ITS `conecta` rows exactly, with identical coordinates, at
    **+3 h**, and none match at +0 h.
- **Per 1-minute window:** 3,564–3,992 records and about 1,550 vehicles, **1.26 MB raw /
  159 KB gzip**. About 1% of rows are exact duplicates, and 12 more `(vehicle, datetime)`
  pairs differ only in `datetime_servidor`.
- **Late arrivals:** re-querying three windows about 14 minutes later returned identical sets.
- **Reliability:**
  - One of 10 SPPO requests (about 13:29:57 UTC) failed mid-body with a truncated chunked
    response (`IncompleteRead`). This is a real "fetch failed" case.
  - The `/gps/brt` route returns 404 to `HEAD` requests and works for `GET`.

### 1.4 Behaviour that event identity depends on (architecture §10 #7)

- **Vehicle IDs are stable across polls** in all three paths:
  - BRT: 863 of 863 identical across 20 polls;
  - ITS: 3,853 vehicles in all 11 minutes, out of 5,792 seen;
  - GTFS-RT: 723 of 1,188 buses and 94 of 172 BRT vehicles in every poll (the rest
    started or ended trips).
- **The same IDs are used across paths:**
  - legacy SPPO `id_veiculo` = ITS `conecta`/`zirix` `id_veiculo` = GTFS-RT `vehicle.id`
    (1,006 of 1,008);
  - legacy BRT `codigo` = ITS `sonda` `id_veiculo` = GTFS-RT BRT `vehicle.id` (863/863 and
    131/131).
  - The ID formats differ by system: SPPO `A29001`, `C30129`; Sistema RIO `A2-003`;
    BRT `901008`, `E8…`.
- **A parked vehicle repeats the same `event_time` in snapshot feeds**, so `(vehicle,
  event_time)` collapses the repeats correctly:
  - legacy BRT: 10,482 of 12,751 stationary consecutive pairs kept the same `dataHora`, and
    0 moved without a new `dataHora`;
  - GTFS-RT BRT: 125 of 205 stationary pairs.
  - In the window-based paths (ITS, legacy SPPO) each ping appears once per window, apart
    from the duplicates noted above.
- **But identity collides across suppliers** (finding 2 above): `conecta` and `zirix` report
  the same `id_veiculo` from two devices with different positions.

### 1.5 Mapping to the canonical event (architecture §3)

| Canonical | ITS API | GTFS-RT | legacy SPPO | legacy BRT |
|---|---|---|---|---|
| `vehicle` | `id_veiculo` | `vehicle.id` | `id_veiculo` | `codigo` |
| `line` | `servico` (= GTFS `route_short_name`; 91.6% join for conecta, 94.9% zirix, 100% maxtrack, 84.5% sonda — misses are `ESPECIAL`, `LECD…`, `0`) and `route_id` (99.2% / 100% join where present) | `trip.route_id` (99.5% / 100%) | `servico` (90.5%), `route_id` (98.8%) | `linha` (75.7%; `"0"` = no line) |
| `lat`, `lon` | float WGS84, 6–7 decimals | float32 | float, 7 decimals | float, 6 decimals |
| `event_time` | `datetime`, ISO 8601, real UTC | `vehicle.timestamp`, POSIX s, UTC | `datetime`, ISO with **false `Z`: it's UTC−3** | `dataHora`, epoch ms, UTC |
| speed / heading | `velocidade` km/h, `direcao` ° | `position.speed`, `bearing` (BRT > 360 seen) | `velocidade`, `direcao` | `velocidade`, `direcao` (sometimes `" "`) |
| extra | `trip_id`, `shape_id`, `sentido`, `datetime_servidor`, `fornecedor` | `trip_id`, `direction_id`, `start_time` | `trip_id`, `shape_id`, `datetime_servidor` | `ignicao`, `placa`, `trajeto` |

## 2. Rio static GTFS (SMTR)

- **URL:** `https://dados.mobilidade.rio/gtfs/schedule`, served as an attachment named
  `gtfs.zip`.
  - This is what data.rio's ArcGIS item `b577e4c4c0924888823b630bbdb2c6fd` ("GTFS do Rio de
    Janeiro") links to.
  - The Mobility Database (`mdb-1791`) and Transitland's historic URL still point to ArcGIS
    item `8ffe62ad…`, which now returns **403**.
- **Auth:** none.
- **License:** CC-BY-4.0. Attribution: "Secretaria Municipal de Transportes - SMTR, Prefeitura
  da Cidade do Rio de Janeiro - PCRJ".
- **Caching:**
  - `Cache-Control: public, max-age=3600`, `Last-Modified: Wed, 10 Jun 2026 17:51:42 GMT`,
    strong `ETag "df5bbd7ba0535fa5b9ef9b0b08854409"`.
  - Both `If-None-Match` and `If-Modified-Since` return **304**.
  - `HEAD` returns 404, while ranged `GET` works.
- **Full feed (captured 2026-10-12 13:22 UTC):** 25,321,196 bytes, SHA-256
  `a99f925460e7628b6eeecb9952430542c06b3e2800afa8ba7f9765fd2e6f26f1`. It isn't committed.
- **Version and freshness:**
  - `feed_info` publisher "Secretaria Municipal de Transportes - SMTR", `feed_id`
    `SMTR-PCRJ`, `feed_start_date` 20240101, **`feed_end_date` 20261231**, `feed_version`
    empty.
  - The files inside are dated 2026-06-03.
  - The ArcGIS snippet says "Atualizado mensalmente" (updated monthly), but the file has
    been unchanged since June.
- **Files:**
  - `agency`, `calendar`, `calendar_dates`, `fare_attributes`, `fare_rules`, `feed_info`,
    `frequencies`, `routes`, **`shapes`** (18.8 MB), `stop_times` (85.6 MB), `stops` and
    `trips`;
  - 107.5 MB unzipped.
- **Contents:**
  - **Agencies:** Internorte `22003`, Intersul `22002`, Santa Cruz `22005`, Transcarioca
    `22004` and MOBI-Rio `20001`, all in `America/Sao_Paulo`.
  - **Counts:** 494 routes (`route_type` 700: 436, 702: 30, 200: 28), 17,361 trips and
    7,694 stops.
  - **Timetabling:** 17,275 trips (99.5%) use `frequencies.txt` with `exact_times=0`.
  - **No rail, metro, VLT or ferry routes.**
- **Sample:** `samples/smtr-20261012-gtfs-trimmed.zip` holds 3 routes (BRT `22` /
  `20000221110`, bus `565`, bus `343`), at most 6 trips per direction, and the stops,
  stop_times, shapes, frequencies and fares they reference. Every file is kept consistent.

## 3. data.rio historical data on BigQuery

- **Project:** `rj-smtr`. Facts below come from the city's published dbt docs at
  <https://docs.mobilidade.rio/>, whose embedded `catalog.json` was generated
  2026-10-10T16:48Z, and from `prefeitura-rio/queries-rj-smtr`.
- **Access:**
  - Querying needs a Google Cloud account, even for public data. Per the task rules I didn't
    sign up, and **no query or dry run ran**.
  - `[VERIFY: whether rj-smtr.datario.* / rj-smtr.monitoramento.* grant public read — needs
    a GCP account; the user decides]`
  - The ITS documentation names `rj-smtr.monitoramento` as "o data lake" for history.
- **Tables (rows and bytes from the catalog):**

  | Table | Rows | Bytes | Bytes/row |
  |---|---|---|---|
  | `br_rj_riodejaneiro_veiculos.gps_sppo` (treated, legacy conecta) | 13,857,185,037 | 2.08 TB | 150 |
  | `br_rj_riodejaneiro_veiculos.gps_brt` | 656,895,171 | 97.3 GB | 148 |
  | `monitoramento.gps_onibus_conecta` | 3,696,655,562 | 504.3 GB | 136 |
  | `monitoramento.gps_onibus_zirix` | 3,931,484,132 | 535.6 GB | 136 |
  | `monitoramento.gps_v2_onibus_maxtrack` | 42,904,604 | 14.5 GB | 337 |
  | `monitoramento.gps_brt_sonda` | 19,862,082 | 2.7 GB | 138 |
  | `datario.gps_onibus`, `datario.gps_brt` | views | — | — |
  | `monitoramento.view_gps_onibus` | a view over every supplier | — | — |

  The view's columns include `datetime_gps` ("[GMT-3]"), `id_registro`, `route_id`,
  `trip_id`, `shape_id` and `status`.
- **The date range isn't confirmed.**
  `[VERIFY: min/max of the partition column "data" — needs a query]`. Dividing rows by
  today's measured rate gives estimates, not facts:
  - `gps_onibus_conecta`: about 685 days at 5.4 M rows/day;
  - `gps_onibus_zirix`: about 580 days;
  - `gps_v2_onibus_maxtrack`: about 44 days, consistent with Sistema RIO being new.
- **Bytes scanned per day of positions (estimate, no dry run):**
  - At today's conecta rate (3,749 rows/min ≈ 5.4 M rows/day) × 136–150 bytes/row, that's
    **≈ 0.73–0.81 GB per day of all columns**, and less when selecting a few.
  - All suppliers together (≈ 10.9 k rows/min ≈ 15.7 M rows/day): **≈ 2.1–2.4 GB/day**.
  - BigQuery on-demand price (checked on <https://cloud.google.com/bigquery/pricing>,
    2026-10-12): the first 1 TiB per month is free, then **USD 6.25 per TiB**. A year of
    every supplier is about 0.8 TiB.

## 4. SuperVia → TrensRJ (rail)

- **Not found** in the Mobility Database catalogue (`feeds_v2.csv`, downloaded 2026-10-12;
  its only Rio-state feeds are `mdb-1791` and an inactive Angra dos Reis feed).
- **Not found** in the Transitland atlas (the repo at its 2026-10-09 commit; the only Rio
  file is `dados.mobilidade.rio.dmfr.json`).
- **Not found** on the state CKAN portal (`dadosabertos.rj.gov.br`, searches for
  `gtfs`/`trem`: only monthly ridership).
- **Not found** on AGETRANSP (its open-data page links only to the state portal).
- `https://www.supervia.com.br/` returned **502**. AGETRANSP lists `trensrj` as an operator,
  and `https://www.trensrj.com.br/pt` is live, with timetables as web pages and an app. The
  site uses an undocumented backend (`api.trensrj.com.br`) that isn't open data and **must
  not be used** (invariant 4, terms unknown).
- **Tier: none.** `[VERIFY: whether the TrensRJ concession contract requires publishing
  GTFS — not checked]`

## 5. MetrôRio

- Same searches as §4: nothing in the Mobility Database, Transitland, the state portal or
  AGETRANSP.
- The state portal has only ridership tables (`concessionaria-metrorio`,
  `passageiros-transportados-por-mes-e-por-linha-sistema-metroviario-rj`).
- The operator homepage has a line-status widget and no open-data or GTFS link.
- **Tier: none.**

## 6. VLT Carioca

- No GTFS in the Mobility Database, Transitland or the state portal (a search for `vlt`
  returned 0).
- The operator site <https://trilhos.motiva.com.br/vltrio/> has no data links.
- The city's GIS has a VLT stops layer, ArcGIS item `29cc383d2d344e8387dda153ec0d545d` on
  `pgeo3.rio.rj.gov.br` (stops, not a timetable).
- SMTR's dbt catalog has `dashboard_bilhetagem_implantacao_jae.gps_agregado_vlt`
  (860 rows), an internal ticketing aggregate.
- **Tier: none.**

## 7. Barcas → Barcas Rio (ferries)

- No GTFS in the Mobility Database, Transitland or the state portal (only
  `concessionaria-ccr-barcas`, a 2025-11-04 dataset of operating figures).
- <https://barcasrio.com.br/> publishes "Linhas, horários e tarifas" as HTML. Its homepage
  has a "Estações agora / Tempo real" banner, but no data link was found.
  `[VERIFY: what "Tempo real" shows and whether it's machine-readable]`
- **Tier: none.**

## 8. DETRO-RJ intercity buses

- No GTFS in the Mobility Database, Transitland or the state portal (searches for `onibus`
  and `detro` returned 0).
- <http://www.detro.rj.gov.br/regulares-tarifas-itinerario/> is an HTML lookup form for
  routes, fares and itineraries. It offers no download or API. HTTPS didn't connect; HTTP
  worked.
- **Tier: none.**

## 9. Niterói city buses

- There's no GTFS in the Mobility Database or Transitland, and no open real-time feed.
- The city's ArcGIS org publishes the layer "Linhas de Ônibus Transnit" (item
  `40cf3890d24644bfbaf3d69b0c2b94c1`):
  - **43 polylines** with the fields `OBJECTID`, `tx_nome`, `db_extensao` and
    `Shape__Length`;
  - data last edited **2023-12-05**;
  - **no license stated**.
- Sibling layers exist for "Transoceânica" lines and bus shelters.
- The hub's search API (`dados-geoniteroi.opendata.arcgis.com`) returned **401**.
- **Tier: none** (route geometry only, unlicensed).

---

## Raw volume, S3 cost and the 4-month raw log

Measured medians from 2026-10-12, 10:19–10:41 Rio time: a public holiday on Sunday service,
mid-morning. Daily figures multiply that median by the polls per day. Overnight load is lower
(at 04:34 Rio time the ITS API had 6,044 records/min against about 10,900 at 10:20). A
weekday peak may be higher than the holiday median. These are estimates to re-measure over
full weekdays and weekends in phase 1. The raw log stores each response as received,
gzip-compressed.

| Source (cadence) | gzip per poll | Polls/day | **Per day** | 122 days (~4 months) |
|---|---|---|---|---|
| ITS API, every supplier (1 min, 3 pages) | 367 KB | 1,440 | **≈ 529 MB** | **≈ 64.5 GB** |
| GTFS-RT buses (30 s) | 39.8 KB | 2,880 | **≈ 115 MB** | ≈ 14.0 GB |
| GTFS-RT BRT (30 s) | 4.6 KB | 2,880 | **≈ 13 MB** | ≈ 1.6 GB |
| legacy SPPO window (1 min) | 159 KB | 1,440 | ≈ 229 MB | ≈ 27.9 GB |
| legacy BRT snapshot (30 s) | 27.9 KB | 2,880 | ≈ 80 MB | ≈ 9.8 GB |
| static GTFS (only on change; 304 otherwise) | 25.3 MB per version | ~0–1 per month | — | ≈ 0.1 GB |

**Total raw log at the end of a ~4-month live run, compressed:**

- **ITS API only:** about **65 GB**.
- **ITS + GTFS-RT:** about **80 GB** (both, if GTFS-RT is kept as a cross-check).
- **GTFS-RT only:** about **16 GB**, but it misses about half the buses (§1.2).

That's the local backup and the re-upload size for the account move (architecture §7.1).
It fits on the Mac plus an external drive.

**S3 cost** (prices from the AWS Price List API for `us-east-1`, `publicationDate`
2026-09-28):

- **Rates:**
  - Standard $0.023/GB-month (first 50 TB);
  - Standard-IA $0.0125/GB-month plus a $0.01/GB retrieval fee;
  - PUT $0.005 per 1,000 (Standard) or $0.01 per 1,000 (IA).
  - The S3 pricing page states IA's 128 KB minimum billable object size and 30-day minimum
    duration, and that the first 100 GB/month of transfer out to the internet is free.
- **Storage** for ITS + GTFS-RT (≈ 0.66 GB/day), 30 days in Standard and then IA: about
  $0.23 for month 1, then about $0.57, $0.82 and $1.07. **About $3 in total for the run.**
- **Requests are the real cost if each poll becomes its own object:**
  - about 10,000 PUTs/day (ITS pages plus 2 GTFS-RT feeds) → about 1.2 M PUTs over 4 months
    ≈ $6;
  - about $12 more if each object later transitions to IA, assuming lifecycle transitions
    are billed like IA PUTs at $0.01 per 1,000
    `[VERIFY: the lifecycle-transition request price]`;
  - and most objects are under IA's 128 KB minimum, so they'd be billed as 128 KB each.
  - **Batching to one object per source per hour** (24/day) makes requests negligible, at
    under $0.10 for the run, and every object is above 128 KB. That's a design point for the
    phase 1 task.
- **The account move:** downloading 65–80 GB fits within the 100 GB/month free egress if it's
  done in a single month. `[VERIFY: S3 PUT/ingress cost of the re-upload into the new account
  — only the replay days' prefixes need re-uploading, ≈ 0.5 GB per ITS day]`

**Database load:**

- **Write rate:** the ITS API is about 10.9 k positions/min, ≈ 180 events/s at the measured
  median, or about **15.7 M rows/day** before de-duplication. 3 days of full detail
  (architecture §8) is about **47 M rows**. GTFS-RT alone is about 1,150 entities per 30 s,
  ≈ 38 events/s.
- `[VERIFY in phase 1: TimescaleDB bytes/row with compression on the t4g.small]`

## Samples

All captured 2026-10-12. Bodies are stored decoded (as JSON or protobuf). The gzip
`Content-Encoding` was only on the wire.

| File | Source | Captured (UTC) | Notes |
|---|---|---|---|
| `brt-20261012-gps-legacy-snapshot.json` | `https://dados.mobilidade.rio/gps/brt` | 13:29:25 | full response; 863 vehicles, including stale ones |
| `sppo-20261012-gps-legacy-window-trimmed.json` | `https://dados.mobilidade.rio/gps/sppo?dataInicial=2026-10-12T10:25:00&dataFinal=2026-10-12T10:26:00` | 13:28:59 | **trimmed to the first 400 of 3,729 records**; the full window was 1.27 MB |
| `smtr-its-20261012-veiculos-{conecta,zirix,maxtrack,sonda}.json` | `https://its.mobilidade.rio/v1/geolocalizacao/veiculos?minuto_utc=2026-10-12T13:38:00Z&fornecedor=<f>&limit=40` | 13:40:07 | full responses, 40 records each, with a `next_cursor` |
| `sppo-20261012-gtfsrt-vehicle-positions.pb` | `https://dados.mobilidade.rio/gtfs/realtime/onibus/vehicle-positions` | 13:34:58 | full response; covers SPPO and Sistema RIO |
| `brt-20261012-gtfsrt-vehicle-positions.pb` | `https://dados.mobilidade.rio/gtfs/realtime/brt/vehicle-positions` | 13:35:01 | full response |
| `smtr-20261012-gtfs-trimmed.zip` | trimmed from `https://dados.mobilidade.rio/gtfs/schedule` | 13:22:07 | 3 routes; the full feed's size and SHA-256 are in §2 |

Not committed (kept locally in the session scratchpad, so they aren't reproducible from the
repo):

- every poll from the three ~10-minute runs: 20 legacy BRT, 10 legacy SPPO, 11 ITS minutes
  (33 pages) plus 3 rechecks and 3 history probes, and 21 + 21 GTFS-RT polls;
- the full GTFS zip.

The figures above were computed from them.

## Method notes

- The polling scripts lived in the session scratchpad, not the repo:
  - legacy BRT every 30 s and SPPO every 60 s;
  - ITS one minute every 60 s;
  - GTFS-RT both feeds every 30 s.
- Every request sent the descriptive User-Agent above.
- Peak combined load per host: `dados.mobilidade.rio` about 7 requests/min (limit 60/min);
  `its.mobilidade.rio` about 3 requests/min.
- GTFS-RT was decoded with a small stdlib decoder, because `pip` failed inside the sandbox.
- Not done:
  - no sign-ups, no BigQuery access and no paid queries;
  - undocumented site backends (TrensRJ) weren't touched.
