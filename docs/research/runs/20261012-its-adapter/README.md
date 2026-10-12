# ITS adapter: lag measurement and 60-minute live run (2026-10-12)

The evidence behind `docs/tasks/TASK-its-adapter.md` §7 and `docs/research/README.md` §1.1.
These are the throwaway scripts and logs as they ran, kept so the numbers can be checked.
The scripts have the session's scratchpad paths in them (`S=`); run them from a directory
holding the files, with `python3 -I`.

| File | What it is |
|---|---|
| `lag_measure.py`, `lag.jsonl` | the lag measurement: 30 minutes × 4 fetch times, one JSON line per fetch (record count, SHA-256 of the sorted rows, pages, per-supplier counts) |
| `e7_driver.sh`, `early_stop.sh` | the choreography: start, E8 (stop 5 min), E9 (Redpanda down 3 min), and the stop at 60 min |
| `e7-events.log` | when each step happened (UTC) |
| `e7.log` | the adapter's JSON log for the whole run, both processes |
| `e7-rss.log` | `ps` RSS in kB, once a minute |
| `peak.time` | `/usr/bin/time -l` of a 150 s backfill: maximum resident set 64,258,048 B |
| `e7_analyze.py`, `rawread.py` | the analysis: reads `e7.log` and the raw log, prints every number in §7 |

**The raw log of the run is not in git** (41 MB, 118 minutes, 1,153,274 records). It's at
`var/runs/20261012-its-adapter-raw/` (git-ignored, on the author's disk), with SHA-256s in
`var/runs/20261012-its-adapter-raw.sha256`. It's real provider data, stored as received. The
topic dump used for the "replay = live" check (474 MB) wasn't kept: it's rebuilt from the raw
log with `MinuteEvents`, and the check that compared them was a temporary test.
