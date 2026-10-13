# Line routes (D3), 2026-10-13

Implemented `docs/tasks/TASK-line-routes.md` against the real SMTR ZIP and the
existing `transit_pulse_run` recording. Generated assets stay in
`apps/web/public/gtfs/`; the raw ZIP is retained by `packages/raw` under source
`gtfs`, with its location/validators in `var/gtfs/state.json`.

Results:

- `export-results.json`: full feed provenance, payload sizes, unmatched snapshot
  lines and three isolated offline export timings/RSS measurements.
- `performance-initial-deck.json`: retained failed initial rendering measurements
  and one fresh-browser diagnostic comparison; the final renderer uses MapLibre
  static layers and clipped, cached real trail geometry.
- `docs/tasks/assets/line-routes-browser-results.json`: four layout/axe checks,
  keyboard, touch collapse, delayed selection, missing files, retry, reduced
  motion, latest mode, cached switches and three 10 s playback runs.
- `docs/tasks/assets/line-routes-history-honesty-results.json`: D2 honesty rerun
  after the trail optimization, with separate gap/loading/failure screenshots.
- `docs/tasks/assets/line-routes-*.png`: bus/BRT stop lists, phone compact views,
  both colour schemes, desktop/phone, and history regression states.

Browser checks used Chromium with Apple M2 Metal GPU, the Next development server
and the real local API. Production build and TypeScript pass separately. No runtime
dependency was added for validation. GPU/host load affects the individual frame
measurements; report the preserved runs and median together.

## Rerun

From the repo root, prepare the static files with `make gtfs` (one fetch attempt
per 24 h) or `make gtfs-export` (offline). Start the recorded database's API and
the web server as described in the D2 run's README. Do not ingest into this recording.
Browser checks use **http://localhost:3000**: this matches Next's default development
origin; using 127.0.0.1 against this dev server blocked development resources.

Use existing Playwright/axe tooling or install it separately from the app:

```sh
mkdir -p /private/tmp/transit-browser-tools
pnpm --dir /private/tmp/transit-browser-tools add playwright@1.62.1 axe-core@4.13.0
pnpm --dir /private/tmp/transit-browser-tools exec playwright install chromium
export PLAYWRIGHT_MODULE=/private/tmp/transit-browser-tools/node_modules/playwright/index.mjs
export AXE_SCRIPT=/private/tmp/transit-browser-tools/node_modules/axe-core/axe.min.js
export TRANSIT_URL=http://localhost:3000

# Run measurements separately from compiler/build/test workloads.
node docs/research/runs/20261013-line-routes/browser-check.mjs
node docs/research/runs/20261013-line-routes/history-check.mjs
sh docs/research/runs/20261013-line-routes/validate-export.sh
go build -o var/gtfs-export ./apps/gtfs
python3 docs/research/runs/20261013-line-routes/measure-export.py
```

`measure-export.py` uses macOS `/usr/bin/time -l` and the API on 8080. Each export
uses a fresh temporary output directory; generated provider data is never deleted.
`validate-export.sh` uses the existing Go schema helper and validates every current
file. Browser scripts save results on failure and exit nonzero. Delays/503 responses
are applied only to our file/history requests; no provider positions are fabricated.
The history script is the D2 check with a separate artifact prefix and localhost
default. Scripts inspect React's rendered layer props and require the development
build. The functional contexts are closed and a fresh browser is launched for FPS.

The real trimmed ZIP also supplies `hooks/testdata/line-routes-22.json`; every geometry
and stop is from that capture. Go's independent CSV oracle verifies the projection.
Use `make test`, `make lint`, and `make web-build` for repository checks. Include
the generated `public/gtfs/` directory when deploying the web build.
