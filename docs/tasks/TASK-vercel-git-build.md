# Vercel Git build repair — 2026-10-29

## Problem
Git builds start at the repository root, which has no package.json. The app is in apps/web.
Generated replay and GTFS projections are ignored, so a successful compile alone is insufficient.

## Plan
1. Set the existing Vercel project's Root Directory to apps/web; retain Next.js and Node 24.
2. Publish only the compact, already-public replay and GTFS projections to a new content-addressed
   release in the existing public assets bucket. Keep all raw logs private in S3 and off the Mac.
3. Commit a pinned checksum inventory and a Node-only Vercel asset preparation step inside apps/web.
   Verify the entire downloaded bundle before installing public files; reject corruption and unsafe paths.
4. Validate lint, tests, typecheck, an asset-free preparation and a fresh Git preview build.
   Check the resulting replay map/table and files. Document the settings and release process.

## Sources
- https://vercel.com/docs/builds/configure-a-build
- https://vercel.com/docs/rest-api/projects/update-an-existing-project
- Installed Next.js public-folder documentation.

## Results
- Root Directory changed from null to apps/web on the existing Vercel project.
- Public API/tile origins stored for both preview and production (previous builds relied on shell env).
- Uploaded one immutable 6,264,515-byte compact asset release to the existing public tiles bucket.
  Bundle SHA-256: 0aba4d2803c349530bbc796e3598f4130d30e425e2b9b34059fa14a8791730c7.
- Empty-directory preparation downloaded and verified all 502 files (21,845,208 expanded bytes).
- 106 web tests and 4 bundle integrity/path tests passed, plus lint and typecheck.
- Local full build passed. Fresh Vercel Git deployment dpl_GWFJnfJM9ajue8JVywwD4mw9ceZe
  for commit 88eaa7a is READY: pnpm 11.21.0, Next.js 16.4.3, all 502 assets verified.
- Public preview: https://transit-pulse-emw2jwnfw-benito-3af53aa4.vercel.app/replay
- Chrome desktop/mobile map/playback/end-seek/restart/table checks passed while all API requests
  were blocked; no page errors or mobile overflow. All seven replay files plus GTFS index/one
  route file matched the pinned checksums on the deployed site.
- Live API separately returned 502. AWS reports Server.SpotInstanceShutdown at 15:58:09 UTC,
  with persistent Spot request open/capacity-not-available. This does not affect packaged replay.
- No new compute, storage volumes, bucket policies or full raw-log downloads were needed.
