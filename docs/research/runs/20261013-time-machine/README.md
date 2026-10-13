# Time machine browser verification, 2026-10-13

Completed T6 and T8–T11 from `docs/tasks/TASK-time-machine.md` on the real
`transit_pulse_run` database (2026-10-12 14:27–16:24 UTC). The API, local Rio
PMTiles extract and Next dev server were running. Chromium used the Apple M2
Metal renderer, not a software GPU. Playwright 1.62.1 and axe-core 4.13.0 were
available locally; they are verification tools, not application dependencies.

Results and screenshots are in `docs/tasks/assets/time-machine-*`:

- `time-machine-browser-results.json`: layouts, accessibility, keyboard,
  reduced motion, FPS, cached seeking and full playback timing.
- `time-machine-honesty-results.json`: actual rendered layer props, real
  30-minute gap in BRT vehicle 7001, no interpolated heads in real-position or
  reduced-motion mode, and empty replay layers while history loads or fails.
- `time-machine-regression-results.json`: the shared minute survives delayed
  hour responses; mobile touch can collapse Linhas after scrolling and selection.

The browser measurements used the development server. The production build and
all repository checks passed separately. Frame rate is the browser's
`requestAnimationFrame` cadence while the real deck.gl map and trails render.

## Rerun

Start the existing recording with the commands from README (or `make dev DB=...`
pointing to `transit_pulse_run`). Do not start the adapter against this recording.
The scripts expect its exact minutes and counts, and `honesty-check.mjs` inspects
React's current fiber tree in the development build. Loading/failure checks
intercept history requests only; they do not fabricate provider positions or
change the database.

Use an existing Playwright/axe installation, or install a separate tooling folder:

```sh
mkdir -p /private/tmp/transit-browser-tools
pnpm --dir /private/tmp/transit-browser-tools add playwright@1.62.1 axe-core@4.13.0
pnpm --dir /private/tmp/transit-browser-tools exec playwright install chromium
export PLAYWRIGHT_MODULE=/private/tmp/transit-browser-tools/node_modules/playwright/index.mjs
export AXE_SCRIPT=/private/tmp/transit-browser-tools/node_modules/axe-core/axe.min.js
export TRANSIT_URL=http://127.0.0.1:3000
node docs/research/runs/20261013-time-machine/browser-check.mjs
node docs/research/runs/20261013-time-machine/honesty-check.mjs
node docs/research/runs/20261013-time-machine/regression-check.mjs
```

Each script exits nonzero on a failed assertion and writes its results even on
failure. `browser-check.mjs` takes about three minutes, including the 117-second
real-time playback. The browser must remain visible to its frame clock: switching
tabs deliberately pauses playback.
