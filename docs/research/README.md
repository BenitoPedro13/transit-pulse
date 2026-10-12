# Research — sources

Verified facts about every data source, each backed by a real captured response in
`samples/`. **Nothing here may come from memory.** A cell is filled in only after it has been
checked against the live source, with the URL linked and the capture date stated.

**Status (2026-10-12): unverified.** Every cell below is `[VERIFY]`. Filling this table is
`docs/tasks/TASK-source-inventory.md`.

## Source inventory

"Expected tier" is the planning conversation's guess and is the first thing to verify:
`live` = vehicle positions, `scheduled` = static GTFS only, `none` = no public data.

| Operator | Mode | Run by | Expected tier | Source URL | Auth | Update interval | Format | Verified? |
|---|---|---|---|---|---|---|---|---|
| Rio city buses (SPPO) | bus | city `[VERIFY]` | live `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| BRT Rio | brt | city `[VERIFY]` | live `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| SuperVia | rail | state `[VERIFY]` | scheduled `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| MetrôRio | metro | state `[VERIFY]` | scheduled `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| VLT Carioca | tram | `[VERIFY]` | scheduled `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| Barcas (ferries) | ferry | state `[VERIFY]` | scheduled `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| Intercity buses (DETRO-RJ) | bus | state `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| Niterói city buses | bus | city (Niterói) `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| Rio static GTFS (city network) | — | city `[VERIFY]` | scheduled `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |
| data.rio historical (BigQuery) | — | city `[VERIFY]` | historical `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | `[VERIFY]` | no |

For each row, `TASK-source-inventory.md` should record:

- the exact URL, plus any terms of use or attribution requirement;
- the auth method (none, API key, registration);
- the observed update interval, measured by polling and not taken from the docs alone;
- the response format and the fields that map to the canonical event
  (`docs/architecture.md` §3);
- caching headers and conditional-request support (invariant 4), plus any published rate
  limit;
- a captured sample in `samples/`, named `<operator>-<YYYYMMDD>-<what>.<ext>`.

## Samples

`samples/` holds real captured responses, the only fixtures parsers are tested against.
Keep each one small and representative: a single poll, or a trimmed GTFS. Large captures
stay out of git (see `.gitignore`); note their source URL and capture time here instead.
