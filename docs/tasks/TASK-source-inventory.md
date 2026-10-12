# TASK: Source inventory — verify every Rio source live

**Status (2026-10-12): done.** Research only: no application code, no scaffold, no
infrastructure. The results are in `docs/research/README.md`; the questions this task raised
for the user are `docs/architecture.md` §10 #9–#12.

## 1. Current scenario

- `docs/research/README.md` lists ten rows (SPPO buses, BRT, SuperVia, MetrôRio, VLT
  Carioca, Barcas, DETRO-RJ intercity buses, Niterói city buses, Rio static GTFS, data.rio
  on BigQuery). **Every cell is `[VERIFY]`**; everything known came from memory in the
  planning conversation.
- `docs/research/samples/` is empty, so no parser can be tested and no number in
  `docs/architecture.md` is backed by a measurement.
- Blocked on this:
  - the canonical event (§3): field formats, time zone of `event_time`, whether `line` joins
    to GTFS;
  - event identity (§10 #7): `(operator, vehicle, event_time)` is only sound if vehicle IDs
    are stable across polls and a parked vehicle repeats its `event_time`;
  - the tiers in §4 and the phase plan built on them;
  - raw volume (§6) and therefore retention (§8) and the account-move size (§7.1).

## 2. Planned changes

### 2.1 Research process (per row)

1. Find the official source, starting with the operator or authority (Prefeitura do Rio /
   SMTR, data.rio, the state government, operator sites), then the GTFS aggregators
   (Mobility Database, Transitland) and official GitHub repos (e.g. the city's data office).
2. Record: exact URL; terms of use and attribution; auth method; format and the fields that
   map to the canonical event; `ETag` / `Last-Modified` / `Cache-Control`; whether a
   conditional request returns `304`; any published rate limit; the actual tier.
3. **If a source needs an API key or registration: stop and report it.** No sign-ups.
4. **Live sources are measured, not trusted:**
   - poll every 30 s (never faster) for ~10 minutes with a descriptive User-Agent
     (`transit-pulse-research/0.1 (+contact: repo owner; one-off source inventory)`);
   - record the observed update interval (content hash and max `event_time` per poll);
   - bytes per poll, raw and gzip-compressed; vehicles per poll;
   - whether a vehicle ID persists across polls; whether a stationary vehicle repeats the
     same `event_time`; the time format and time zone;
   - whether `line` joins to GTFS `route_id` / `route_short_name`.
   - Polling scripts live in the session scratchpad, **not** the repo.
5. **GTFS feeds:** download, record version/date, file list (is there `shapes.txt`?), full
   size and SHA-256. Commit only a trimmed sample (a few routes, their trips/stop_times/
   shapes), force-added past `.gitignore`.
6. **data.rio / BigQuery:** confirm dataset and table names, schema, date range and whether
   it's public. Estimate bytes scanned per day with a dry run only if credentials already
   exist locally. **No paid query without asking.**
7. Samples are named `<operator>-<YYYYMMDD>-<what>.<ext>` and kept small; large captures
   stay local with their URL and capture time noted.

### 2.2 Documentation

- `docs/research/README.md`: fill the inventory table ("Verified? yes" + date only where a
  sample backs it), and one section per source with the details from §2.1. Unconfirmed
  facts stay `[VERIFY: …]` with what was tried.
- Volume estimate: bytes/day per live source (compressed), S3 cost for the live run, and the
  **total raw-log size at the end of a ~4-month live run** (the local backup and re-upload
  size for the account move, architecture §7.1).
- `docs/architecture.md`: resolve what this task answers in §3 (field notes), §4 (tiers),
  §6 (raw volume), §8 (retention) and §10 #7 (vehicle-ID stability).
- `CLAUDE.md`: only if an invariant or decision is affected (e.g. a tier assumption is
  wrong). Contradictions with the plan are **flagged to the user**, not worked around.
- `README.md`: status line.

### 2.3 Alternatives rejected

- **Filling cells from aggregator listings alone** (Mobility Database / Transitland
  metadata). Rejected: listings go stale; the operator's own live response is the source of
  truth, the aggregator is a pointer to it.
- **Polling faster than 30 s to pin down the update interval precisely.** Rejected:
  invariant 4. A 30 s cadence over ~10 min bounds the interval well enough to choose a poll
  rate; finer measurement can happen in phase 1 once the provider's own interval is known.
- **Committing full GTFS zips or multi-poll dumps.** Rejected: `.gitignore` policy; SHA-256
  and URL make the full file reproducible enough.

## 3. Why

- Unblocks the canonical event, the event identity, the phase plan and the scaffold task
  (`TASK-scaffold.md`): adapters can't be written against fields nobody has seen.
- Gives parsers real fixtures (CLAUDE.md "Tests").
- Turns retention and the account-move size into numbers instead of guesses.
- **Cost:** a few hours of polite polling and reading; no money. BigQuery dry runs are free;
  any paid query is asked first.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `docs/tasks/TASK-source-inventory.md` | new | this document |
| `docs/research/README.md` | edit | inventory filled in, one section per source, volume estimate |
| `docs/research/samples/*` | new | small real captures, `<operator>-<YYYYMMDD>-<what>.<ext>` |
| `docs/architecture.md` | edit | §3, §4, §6, §7.1, §8, §10 #7 as resolved |
| `CLAUDE.md` | edit (if needed) | only if a decision or invariant is affected |
| `README.md` | edit | status line |

## 5. Outcome (2026-10-12)

- **Live:** SPPO buses, the new "Sistema RIO" buses and BRT. They come through three
  public, keyless SMTR paths (the ITS JSON API, GTFS-RT and the legacy JSON endpoints), all
  labelled beta.
- **Scheduled:** SMTR's static GTFS (buses and BRT only). It hasn't changed since
  2026-06-10, and `feed_end_date` is 2026-12-31.
- **None:** TrensRJ (formerly SuperVia), MetrôRio, VLT, Barcas Rio, DETRO-RJ and Niterói.
- **Historical:** `rj-smtr` on BigQuery exists. Access wasn't tested, because it needs a
  GCP account.
- **Deviations from §2:**
  - No BigQuery dry run: there were no credentials, and none were created.
  - One legacy SPPO request failed mid-body, so the late-arrival recheck ran as a separate
    3-request step.
  - `pip` didn't work in the sandbox, so GTFS-RT was decoded with a stdlib decoder.
- **Added on request:** an estimate of the total raw log after a ~4-month run, for
  architecture §7.1.
