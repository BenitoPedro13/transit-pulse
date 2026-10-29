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
- Full build and Git preview verification pending.
