# TASK-aws-infra

> Written 2026-10-12. Puts the phase 1 stack on AWS, ships the raw log to S3 and puts the web on a
> public URL. It copies `renewable-pulse/docs/tasks/TASK-aws-infra.md` (deployed in the same account
> on 2026-09-27) and changes what this project needs differently. **Prerequisite:
> `TASK-replay-flake.md` is done** (green `make test`).
>
> Rules for the executing session: every `[VERIFY: …]` is resolved against the live docs, the
> console or the CLI before the code that depends on it is written, and the answer is recorded in
> §6. Phases run in order. Each phase is committed when its checks in §5 pass.
>
> **Refreshed 2026-10-13** for what was built after it was written: the time machine (D2,
> `GET /v1/history*`, `TASK-time-machine.md`) and the static line routes (D3, `public/gtfs/`,
> `TASK-line-routes.md`). The changes are in §1, §2.0, §2.1, §2.4, §2.5, §2.7, §2.8, §4 and §5.

## 1. Current scenario

- **Everything runs on the Mac.** `make up` (Redpanda + TimescaleDB), `make adapter-its`,
  `make consumer`, `make api`, `make web-dev`. The raw log is `./var/raw` on the Mac's disk and
  nowhere else, so invariant 2 ("raw positions on S3, kept forever") **isn't met yet**.
- **Recording time is limited.** The Free-plan account `860897618882` closes around 2027-03-27,
  or earlier if the shared credits run out (architecture §7.1). The three replay days
  (architecture §9) can only be recorded while the stack is live. Réveillon is 2026-12-31.
- **Measurements that were deferred to this task:**
  - the consumer's 30-minute sustained live run: lag, RSS, TimescaleDB and Redpanda memory
    (`TASK-positions-consumer.md` E8);
  - box memory under load, and whether `t4g.small` holds (architecture §6);
  - bytes per row in `positions` with live arrival order (architecture §8);
  - the snapshot query on a full-day chunk: 430 ms median on 1.1 M rows locally, and a full day is
    about 12× larger (`TASK-live-map.md` §7);
  - self-hosted glyphs and sprites (architecture §10 #8).
- **What already exists in the account** (renewable-pulse, verified 2026-09-27 in its task doc):
  - IAM user `benito-admin`, with CLI access through `aws login` (aws-cli 2.37.4 on this Mac;
    Terraform 1.16.4 is installed too);
  - Budget `renewable-pulse-monthly` (USD 20);
  - a `t4g.small` Spot instance in `us-east-1c` with its own VPC, EIP `52.5.78.81`, and
    `renewable-pulse.duckdns.org`;
  - Spot vCPU quota 5 (`L-34B43A08`), of which renewable-pulse uses 2.
- **The remote exists** (`github.com/BenitoPedro13/transit-pulse`, public; found 2026-10-13) but
  there's no CI yet. This task builds the Mac deploy scripts; `TASK-ci-cd.md` Part B runs the same
  scripts from GitHub Actions with OIDC once this task's Phases 1, 2 and 4 are done.
- **The web assumes it's served next to the API.** `next.config.ts` rewrites `/api/*` →
  `${API_ORIGIN ?? "http://127.0.0.1:8080"}` (`API_ORIGIN` already exists; its comment still says
  Caddy serves the web). `components/basemap.ts:72–77` reads `${origin}/tiles/rio.pmtiles`
  (git-ignored, from `make tiles`, 43 MB) and Protomaps' hosted glyphs and sprites.
- **The time machine (D2) reads the API too.** `hooks/use-history.ts` fetches `/api/v1/history`
  (the index; `public, max-age=10, s-maxage=10`) and `/api/v1/history/{hour}` (one
  `history-hour.v1` file per UTC hour; a closed hour older than 24 h is
  `public, max-age=31536000, immutable`, the others `public, max-age=30, s-maxage=30`; ETag on
  both; `apps/api/history.go:682`). `HISTORY_HOURS` (default 3) sets the window. Measured locally
  (`TASK-time-machine.md` §6): peak API RSS 79 MB during the load of a 118-minute window, and an hour
  file of 33 minutes is 427,591 B gzip, so a full hour is about 0.8–1.5 MB gzip. **A full 3 h
  window on full traffic hasn't been measured**, and the api's limit below is 128m.
- **The line routes (D3) are static files, not an API.** `make gtfs` (Mac, network, conditional
  request) captures the SMTR ZIP into `var/raw/gtfs/v1/…` (one 25 MB hour file) and exports
  `apps/web/public/gtfs/`: `index.json` plus 494 content-addressed files
  `<feed-sha256>/<sha256>.json`, 14 MB in all. `hooks/use-line-routes.ts:27` fetches
  `/gtfs/index.json`. The folder is git-ignored, Next serves `public/` with `max-age=0` by
  default, and the box never runs `apps/gtfs` (user decision 2026-10-13: static files, no tables,
  no endpoints).

## 2. Planned changes

### 2.0 Shape of the target

```
 Browser ──HTTPS──▶ Vercel (apps/web, Hobby)
                     ├─ /api/*   rewrite ──HTTPS──▶ Caddy on EC2 ──▶ api (in-memory snapshot + history hours)
                     ├─ /gtfs/*  static files in the Vercel build (D3; built on the Mac)
                     └─ tiles, glyphs, sprites ◀── CloudFront ◀── S3 (tiles bucket, private, OAC)

 EC2 t4g.small Spot, us-east-1, its own VPC, EIP, no SSH (SSM only)
   compose: caddy · api · consumer · adapter-its · redpanda · timescaledb
   /data (30 GiB gp3, separate volume): pg data, redpanda data, raw log, adapter state
   raw-sync timer (every 10 min) ──▶ S3 raw bucket (versioned, no delete for the instance)
   CloudWatch agent + custom raw metrics ──▶ alarms ──▶ SNS email
```

- **One instance for this project, separate from renewable-pulse.** Its own Terraform state, VPC,
  security group and EIP. Two separate boxes means a memory problem in one project can't take
  down the other. The Spot quota allows it (2 + 2 of 5 vCPU).
- **Web on Vercel, not on the box.** This replaces the "Caddy serves the web" assumption in
  `next.config.ts`'s comment and in CLAUDE.md. Reasons:
  - the 2 GiB box has no room for a Next.js server;
  - the public link has to outlive the AWS account (architecture §7.1);
  - renewable-pulse already works this way.
- **The browser only talks to Vercel.** Vercel proxies `/api/*` to the box, so the API needs no
  CORS, and an ad blocker that flags `duckdns.org` (seen with renewable-pulse) never sees that
  name.
- **No database backups.** The database is a projection (invariant 2). Recovery means rebuilding
  from the raw log, and §2.7 drills it. The raw log is the only thing that's backed up, and it's
  the most important data this task protects.
- **Not used:** NAT Gateway, ALB, RDS, MSK, EKS, GitHub Actions, DLM snapshots, `pg_dump`.

### 2.1 Cost (re-check every price, `[VERIFY: AWS pricing pages + current Spot price in us-east-1]`)

| Item | Basis | Monthly |
|---|---|---|
| `t4g.small` Spot | ~USD 0.006–0.009/h (renewable-pulse, 2026-09-27) | ~USD 5 |
| Elastic IP | USD 0.005/h | USD 3.65 |
| EBS gp3: 16 GiB root + 30 GiB data | USD 0.08/GB-mo | USD 3.68 |
| S3 raw: ≈ 15.5 GB/month added (517 MiB/day), Standard 30 days, then IA | architecture §8 | < USD 1 in month 1, ≈ USD 1 by month 4 |
| S3 PUTs: raw-sync | ≈ 24 hour-files × 6 syncs/h ≈ 4.3 k PUT/day, plus re-uploads of the growing hour | < USD 1 |
| CloudFront + tiles bucket (45 MB) | `[VERIFY: Free-plan CloudFront terms; the always-free 1 TB/month]` | ≈ USD 0 |
| CloudWatch: 2 agent metrics + 2 custom metrics + 6 alarms | `[VERIFY]` | ≤ USD 2 |
| ECR: 1 repo, a few ~20 MB images | | < USD 0.10 |
| Vercel Hobby | free; traffic is the snapshot (≈ 10 s polls) + up to 3 history hours (≈ 3–4.5 MB gzip per time-machine session) + route files `[VERIFY: Hobby's monthly Fast Data Transfer and edge request allowances, and what happens on overrun: pause, not bill]` | USD 0 |
| **Total** | | **≈ USD 14** |

Together with renewable-pulse (~12) and Apuração (~40), that's about USD 66/month against the
shared credits. Record the remaining credit in §6 at the start `[VERIFY: Billing console →
Credits]`, and work out the month in which the credits run out. If that's before Réveillon, tell
the user before Phase 2.

### 2.2 Phase 0: account checks (Mac, CLI; nothing is created except the budget)

1. `aws login`, then `aws sts get-caller-identity` → `user/benito-admin`.
2. Spot: `describe-spot-price-history` for `t4g.small` Linux in every us-east-1 AZ over the last
   24 h. Pick the cheapest AZ and record it. The data volume is bound to that AZ for good.
3. Quota: `service-quotas get-service-quota --service-code ec2 --quota-code L-34B43A08`. It needs
   ≥ 4 with renewable-pulse running.
4. **Budgets: both are decided by the user.** Add `transit-pulse-monthly` (USD 20, filtered on the
   tag `project=transit-pulse` `[VERIFY: cost allocation tag activation in Billing; tags take up
   to 24 h to appear]`), with the same alerts as renewable-pulse (actual 50/85/100%, forecast
   100%, `IncludeCredit=false`), outside Terraform. **Check whether `renewable-pulse-monthly`
   has a filter.** If it has none, it's already the account-wide budget, and its USD 20 limit is
   below the three projects' combined ~USD 66. Ask the user whether to raise it or add an
   `account-total` budget.
5. **Fallback instance type, checked now so an OOM later isn't a surprise.** Architecture §6 says
   `t4g.medium`, but it's **not** in the Free-plan eligible list renewable-pulse recorded
   (`t3.micro t3.small t4g.micro t4g.small c7i-flex.large m7i-flex.large t8i.micro t8i.small`).
   The eligible next step up is `m7i-flex.large` (2 vCPU, 8 GiB, **x86**). If it's ever needed,
   the image must be built for `linux/amd64` too. Record its Spot price, and fix architecture §6.

### 2.3 Phase 1: Terraform

```
infra/terraform/
  bootstrap/main.tf     # local state → state bucket (versioned, SSE-S3, public access blocked), then migrated into it
  prod/
    versions.tf         # terraform + aws provider pins, S3 backend with use_lockfile = true
    network.tf          # VPC 10.30.0.0/16, one public subnet in the §2.2 AZ, IGW, SG 80+443 in
    compute.tf          # instance, EIP, data volume (prevent_destroy), instance profile
    storage.tf          # raw bucket, ops bucket, tiles bucket + CloudFront
    ecr.tf              # one repo: transit-pulse/go
    observability.tf    # SNS + email, alarms (§2.6)
    ssm.tf              # parameter names only: /transit-pulse/prod/POSTGRES_PASSWORD, /ITS_CONTACT
    user-data.sh.tftpl
    outputs.tf
```

- **Provider default tags:** `project = transit-pulse` on everything (for the budget filter).
- **Instance:** the same block as renewable-pulse:
  - `t4g.small`, AL2023 arm64 AMI from the SSM parameter;
  - Spot `persistent`, interruption `stop`, `use_spot` variable;
  - IMDSv2 required;
  - root 16 GiB gp3. 12 GiB was enough for renewable-pulse, but this stack adds a local raw
    buffer and pmtiles tooling, so the extra 4 GiB is margin.
- **Data volume: 30 GiB gp3** (decided, architecture §8), `prevent_destroy`, mounted at `/data`.
  user-data runs `mkfs` only when `blkid` finds no filesystem.
- **Instance role:**
  - `AmazonSSMManagedInstanceCore`, `AmazonEC2ContainerRegistryReadOnly`,
    `CloudWatchAgentServerPolicy`;
  - inline: `s3:PutObject`, `s3:GetObject`, `s3:ListBucket` on the raw bucket. **No
    `s3:DeleteObject`, `s3:DeleteObjectVersion` or `s3:PutLifecycleConfiguration`**, so the box
    can never destroy the raw log. `GetObject` + `ListBucket` on `ops/*`.
    `ssm:GetParameters` on `/transit-pulse/prod/*`. `cloudwatch:PutMetricData` limited by
    condition to the namespace `TransitPulse`.
- **Raw bucket** `transit-pulse-raw-<account>`:
  - versioning **on**: a re-upload of a growing hour file keeps the old version, and nothing is
    ever lost to an overwrite;
  - lifecycle: current versions → Standard-IA after 30 days (architecture §8); **noncurrent
    versions expire after 30 days**. That's safe because raw files are append-only, so an older
    version is always a prefix of the newer one;
  - public access blocked; SSE-S3.
  - `[VERIFY: S3 Object Lock is not used; versioning plus a role without delete is enough, and
    Object Lock would get in the way of the account move]`
- **Ops bucket** `transit-pulse-ops-<account>`: holds `deploy/<sha>/` (compose file, Caddyfile,
  scripts). Lifecycle expires deploy files after 30 days.
- **Tiles bucket + CloudFront:**
  - the bucket is private; CloudFront uses Origin Access Control.
  - CloudFront has a response-headers policy with CORS for the Vercel origin and
    `Access-Control-Expose-Headers: Content-Range, Content-Length, ETag`.
  - `Range` requests must pass through and return `206`
    `[VERIFY: CloudFront caching of byte-range requests to S3; the pmtiles docs' "Cloud Storage /
    S3 + CloudFront" recipe]`.
  - It holds `rio.pmtiles` (from `make tiles`), plus glyphs and sprites copied from
    `protomaps/basemaps-assets` at a pinned commit `[VERIFY: that repo's license and its
    recommended way to self-host]`.

### 2.4 Phase 2: images, compose, deploy

**One image.** A new `Dockerfile` at the repo root (multi-stage):
- build `adapter-its`, `consumer`, `api` and `rebuild` with `CGO_ENABLED=0` for `linux/arm64`;
- copy them into `gcr.io/distroless/static-debian12:nonroot` `[VERIFY: current distroless tag]`;
- compose picks the binary with `command:`.
- It's tagged with the git SHA and pushed to the single ECR repo (immutable tags, scan on push,
  keep 5 tagged and expire untagged after 1 day; that rule is renewable-pulse's Phase 2
  follow-up).
- Built natively on the Mac (arm64), so there's no emulation.

**`infra/aws/compose.prod.yml`.** Local `compose.yaml` is unchanged.

| Service | Image | `mem_limit` | Notes |
|---|---|---|---|
| redpanda | same tag as `compose.yaml` (v26.2.4) | 700m | `--smp 1 --memory 512M`, internal listener only, data in `/data/redpanda` |
| timescaledb | same tag as `compose.yaml` (`pg18.6-ts2.30.2`), **pinned by digest** | 600m | data in `/data/pg`; `shared_buffers` and `work_mem` set explicitly for 600m `[VERIFY: timescaledb-ha image docs on tuning env vars vs a mounted postgresql.conf]` |
| adapter-its | ECR `go:<sha>` | 128m | `RAW_DIR=/data/raw`, state in `/data/adapter`, `ITS_CONTACT` from SSM |
| consumer | ECR `go:<sha>` | 192m | |
| api | ECR `go:<sha>` | 128m | `API_ADDR=0.0.0.0:8080`, not published on the host; `HISTORY_HOURS=3`. The 128m comes from the D2 budget (< 100 MB RSS); Phase 5 measures it on a full window, and the first OOM lowers `HISTORY_HOURS`, never raises the limit quietly |
| caddy | `caddy:2` pinned | 64m | TLS for the API hostname; proxies `/v1/*` (snapshot and history) and `/healthz` to `api:8080` and nothing else; passes `Cache-Control`, `ETag` and `Content-Encoding` through unchanged |

- The limits add up to 1.81 GiB of the box's ≈ 1.84 GiB, plus a 2 GiB swapfile (as in
  renewable-pulse). TimescaleDB gets 150m more than renewable-pulse because this project writes
  ≈ 155 rows/s, against renewable-pulse's bursts. Phase 4 measures whether these numbers are
  right. The first OOM kill is investigated, never fixed by quietly raising a limit.
- Every service has `restart: unless-stopped` and a healthcheck. Ports 5432, 9092 and 8080 are
  never published on the host.
- **Hostname:** `transit-pulse.duckdns.org` `[DECIDE: user creates the DuckDNS name and points it
  at the EIP, once]`.
- **`make deploy`** → `scripts/deploy.sh`:
  1. refuse to run if the git tree is dirty;
  2. `docker build`, then push `go:<sha>`;
  3. upload `infra/aws/*` to `s3://ops/deploy/<sha>/`;
  4. `aws ssm send-command` runs `/opt/transit-pulse/remote-deploy.sh <sha>` on the box, which:
     - pulls the files;
     - writes `/opt/transit-pulse/.env` (mode 0600) from SSM;
     - sets `CADDYFILE_SHA256` so a Caddyfile change recreates Caddy (renewable-pulse's reload
       bug);
     - runs `docker compose up -d`;
     - waits up to 120 s for `curl https://<host>/healthz` to return 200, and exits non-zero
       otherwise;
  5. wait on `ssm get-command-invocation` and print its output.
- **First start:**
  - The consumer runs `store.Migrate` itself (it already does).
  - The adapter backfills up to 23 h by itself (`ITS_BACKFILL_WINDOW`), so a fresh box fills the
    last day from the provider, politely, at 1 request/s.
  - **The Mac's recorded raw log is uploaded once** with `aws s3 sync ./var/raw
    s3://raw/raw/` from the Mac, so nothing recorded locally is lost. Check it with §2.5's
    manifest.

### 2.5 Phase 3: the raw log to S3

`infra/aws/raw-sync.sh` runs from a systemd timer every 10 minutes:

1. `aws s3 sync /data/raw s3://<raw>/raw/ --checksum-algorithm SHA256`. Only files whose size
   or mtime changed are uploaded `[VERIFY: aws s3 sync supports --checksum-algorithm in
   aws-cli 2.x, and HeadObject --checksum-mode ENABLED returns a full-object SHA256 for
   single-part uploads (multipart returns a composite checksum; hour files are ≈ 21 MiB, so set
   multipart_threshold above 64 MiB in the CLI config)]`.
2. **Sealing.** An hour file can still be appended to while its minutes are inside the adapter's
   backfill window (23 h). Once an hour is **more than 25 h old**, it's sealed:
   - compute its local SHA-256;
   - `head-object --checksum-mode ENABLED` and compare;
   - on a match, append `<sha256>  <key>  <bytes>` to `s3://<raw>/manifests/<yyyy>/<mm>/<dd>.sha256`;
     the day's manifest file is rewritten whole, and versioning keeps the old ones.
3. **Local pruning.** A local file is deleted only when it's sealed, its manifest line is in S3,
   **and** it's older than 4 days. That keeps a local buffer of about 2 GiB and covers a multi-day
   S3 problem.
4. **Metrics** in namespace `TransitPulse`:
   - `RawSyncSucceeded = 1` on each clean run;
   - `RawNewestFileAgeSeconds`: the age of the newest *write* under `/data/raw`. It goes up when
     the adapter stalls, even if S3 is fine.
5. **The GTFS raw log is written on the Mac, not the box** (`make gtfs` runs only there). So
   `raw-sync.sh` takes its root, its S3 prefix, a seal age and a no-prune flag as arguments, and
   the Mac runs it as `make gtfs-raw-upload`: root `var/raw/gtfs`, prefix `raw/gtfs/`, seal age 0
   (a GTFS hour file is written once per capture), never prune. It also copies
   `var/gtfs/state.json` to `s3://<ops>/gtfs/state.json`, so CI can rebuild `public/gtfs/` from
   the raw log (`TASK-ci-cd.md` §2.2). `scripts/web-deploy.sh` (§2.7)
   runs it first, so a route export never reaches the public site before its raw ZIP is on S3 with
   a manifest line (invariant 2). It doesn't push metrics from the Mac.
6. **Exit runbook** (architecture §7.1): `scripts/pull-raw.sh` on the Mac runs `aws s3 sync` of
   `raw/` and `manifests/` to a local directory, then checks every line of every manifest with
   `shasum -a 256`. It prints the counts, and exits non-zero on any mismatch or missing file.
   It's run once in Phase 5 as a drill, then at least monthly, and once more before the account
   closes.

### 2.6 Phase 3b: alarms (SNS email)

| Alarm | Condition | Why |
|---|---|---|
| `/data` disk | ≥ 70% for 15 min | architecture §8 (renewable-pulse `abb09de`) |
| memory | ≥ 90% for 10 min | |
| status check | `StatusCheckFailed` ≥ 1 | |
| raw sync | `RawSyncSucceeded` missing for 2 h (`treat_missing_data = breaching`) | invariant 2: the raw log isn't reaching S3 |
| adapter stall | `RawNewestFileAgeSeconds` > 900 for 10 min | nothing is being recorded |
| Spot stop | EventBridge rule: EC2 state change to `stopped` for this instance → SNS | so an interruption is noticed |

Each alarm is triggered once with `aws cloudwatch set-alarm-state`, and the email arrives (§5).

### 2.7 Phase 4: web on Vercel, tiles on CloudFront

- **Vercel project** for `apps/web`, deployed from the Mac with the Vercel CLI (there's no git
  remote). **Build locally, upload the output:** `vercel build --prod`, then
  `vercel deploy --prebuilt --prod`. That way Vercel never needs Go or the network to make
  `public/gtfs/`, and the question of whether the CLI uploads git-ignored files doesn't arise
  `[VERIFY: current Vercel CLI docs for `build` / `deploy --prebuilt` with a pnpm project in a
  subdirectory, Node 24, and that `public/` lands in `.vercel/output/static/`]`.
- **`make web-deploy`** → `scripts/web-deploy.sh`:
  1. refuse to run if the git tree is dirty, or if `apps/web/public/gtfs/index.json` is missing
     (tell the user to run `make gtfs`, or `make gtfs-export` offline);
  2. `make gtfs-raw-upload` (§2.5 item 5);
  3. `vercel build --prod` in `apps/web` with `API_ORIGIN` and `NEXT_PUBLIC_TILES_BASE_URL` set
     for production `[VERIFY: whether `vercel build` reads these from `vercel env pull` or the
     shell; set them as Vercel project env vars once and record which]`;
  4. delete `.vercel/output/static/tiles/`: the basemap is served from CloudFront, so the 43 MB
     local extract is never uploaded to Vercel;
  5. `vercel deploy --prebuilt --prod` and print the URL.
- **`next.config.ts`:** the rewrite destination already comes from `API_ORIGIN`. On Vercel it's
  `https://transit-pulse.duckdns.org`. Fix the comment that says Caddy serves the web. Add
  `headers()`:
  - `/gtfs/:feed/:file` → `public, max-age=31536000, immutable` (the paths are content hashes,
    so a new export is a new URL);
  - `/gtfs/index.json` → `public, max-age=0, must-revalidate` (it changes with each export).
  - `[VERIFY: Next 16 `headers()` applies to `public/` files on Vercel; read
    `node_modules/next/dist/docs/` for `headers` first, as `apps/web/AGENTS.md` says]`
- **Caching keeps readers off the box (invariant 5).**
  - The snapshot already sends `Cache-Control: public, max-age=10` and an `ETag`
    (`apps/api/server.go:159`). Add `s-maxage=10` for shared caches. The history endpoints
    already send `s-maxage` (index 10 s, open hours 30 s) or `immutable` (closed hours); leave
    them as they are.
  - Check that Vercel's CDN caches the rewritten response, so a traffic spike reaches the box
    at most once per 10 s per region, not once per viewer.
  - `[VERIFY: Vercel docs on caching responses of external rewrites, and that gzip
    Content-Encoding and ETag/304 pass through]`
  - If Vercel doesn't cache them, record that here. The API is in-memory and costs
    microseconds, but then a spike reaches the box, and §5 must measure it. History hours matter
    most: they're ≈ 1 MB each, and the box's egress is the cost (EC2 data transfer out
    `[VERIFY: price and the Free-plan allowance]`).
- **Tiles:** a new variable `NEXT_PUBLIC_TILES_BASE_URL` gives the base URL for `rio.pmtiles`,
  `glyphs/` and `sprites/`. It defaults to the current local paths, so `make web-dev` is
  unchanged. `make tiles-upload` copies the extract and assets to the tiles bucket.

### 2.8 Phase 5: the deferred measurements and the rebuild drill

Medians of 3 where it's a number (CLAUDE.md "Tests").

1. **E8, the sustained run** (from `TASK-positions-consumer.md`):
   - record over 30 min of live traffic, sampled every 60 s:
     - consumer lag (`rpk group describe`);
     - consumer RSS;
     - `docker stats` for every service;
     - host memory and swap.
   - Pass: lag is under 1 minute's worth of events at the end; no OOM kill; swap use is
     recorded.
2. **After 24 h live:**
   - bytes per row in `positions` (re-measures architecture §8's 289 B);
   - **the history window on full traffic** (D2's budget was measured on 118 minutes on macOS):
     api RSS right after a restart (the full 3 h load) and steady state, `docker stats`, median
     of 3 restarts; the load's duration from the api's logs; each closed hour's gzip size
     (D2 target ≤ 1.5 MB). Pass: peak RSS < 100 MB. Otherwise set `HISTORY_HOURS=2`, record it, and
     tell the user;
   - the snapshot query's time on a full-day chunk, median of 3 refreshes from the api's logs.
   - If the median is above 5 s, stop and plan the index or a summary table. Don't add it in
     this task.
3. **After 4 days:** `/data` use is under 70%, retention has dropped at least one chunk, and
   the raw-sync buffer is pruning.
4. **Rebuild drill** (the restore procedure; there are no backups):
   - into a **scratch database** on the box, rebuild one full hour from files pulled back from
     S3 into a temp directory;
   - compare its rows with the live database for the same `event_time` window using
     `sameTables`' column set, as a throwaway SQL script;
   - pass: 0 differing rows. Record the time it took.
5. **`scripts/pull-raw.sh` drill:** the first day's manifest checks 24 of 24 hours locally.

### 2.9 Alternatives considered and rejected

- **One bigger box shared with renewable-pulse:** that couples the two projects' failures, and
  `t4g.medium` isn't Free-plan eligible.
- **The web on the box behind Caddy:** there's no memory left for it, and the link would die
  with the account.
- **The adapter writing to S3 directly:** it would add an AWS dependency and an AWS failure mode
  to the ingestion hot path. The local file stays the first write, as it is today, and shipping
  is a separate, restartable process.
- **One S3 object per poll:** at 1,440+ objects/day that's more PUTs and is under IA's 128 KB
  minimum (architecture §8). Hour files are kept.
- **GitHub Actions + OIDC in this task:** it's `TASK-ci-cd.md` Part B, on top of the scripts built
  here, so the deploy works from the Mac first and recording isn't held up by CI.
- **Route files on CloudFront next to the tiles:** 14 MB in about 500 small files fits the
  Vercel build easily, and they then outlive the AWS account with the site, as the replay must.
  Tiles go to CloudFront because they need `Range` and 43 MB per deploy.
- **Running `apps/gtfs` on the box:** the feed hasn't changed since 2026-06-10 and the export is
  per deploy anyway, so a schedule on the box would add a job and gain nothing.
- **Building on Vercel (`vercel deploy` from source):** it would need Go and the provider during
  the build, which means a provider request per deploy, from Vercel.
- **The static replay export (architecture §7, §9):** out of scope. This task only records the
  replay days into the raw log. The export reuses `history-hour.v1` and gets its own task once a
  replay day is in S3.
- **`pg_dump` backups + DLM snapshots:** the database is a projection, and recovery is the
  rebuild (§2.8.4).

## 3. Why

- **It satisfies invariant 2 for real.** The raw log is on S3, checksummed, and pulled back
  to the Mac in a drill.
- **Recording starts early.** That decides how many replay days exist, including Réveillon.
- **It answers the deferred questions** on the hardware that matters: memory, lag, bytes per
  row, and the snapshot query on a full chunk.
- **It gives the map a public URL**, ready for the UX work (`docs/research/ux-direction.md`).
- **Cost:** ≈ USD 14/month, about a day of a Sonnet session to execute, and a fixed date by
  which the exit runbook must run.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `infra/terraform/bootstrap/*` | new | state bucket |
| `infra/terraform/prod/*` | new | §2.3 |
| `infra/aws/compose.prod.yml` | new | §2.4 |
| `infra/aws/Caddyfile` | new | API only |
| `infra/aws/remote-deploy.sh` | new | runs on the box |
| `infra/aws/raw-sync.sh` + `raw-sync.service` + `raw-sync.timer` | new | §2.5 |
| `infra/aws/cloudwatch-agent.json` | new | disk `/data`, memory |
| `Dockerfile`, `.dockerignore` | new | one image, four binaries |
| `scripts/deploy.sh`, `scripts/pull-raw.sh` | new | Mac side |
| `scripts/web-deploy.sh` | new | §2.7: GTFS raw upload, `vercel build`, drop `tiles/`, `deploy --prebuilt` |
| `Makefile` | edit | `deploy`, `web-deploy`, `gtfs-raw-upload`, `tiles-upload`, `pull-raw` |
| `apps/api/server.go` | edit | `s-maxage=10` on the snapshot (history headers unchanged) |
| `apps/web/next.config.ts` | edit | fix the Caddy comment; `headers()` for `/gtfs/*` |
| `apps/web/components/basemap.ts` | edit | `NEXT_PUBLIC_TILES_BASE_URL` for pmtiles, glyphs, sprites |
| `.env.example` | edit | `API_ORIGIN` (production value documented), `NEXT_PUBLIC_TILES_BASE_URL`, `HISTORY_HOURS` in prod, deploy variables |
| `.gitignore` | edit | `*.tfstate*`, `.terraform/`, `*.tfvars` except `*.tfvars.example`, `.vercel/` |
| `CLAUDE.md` | edit | stack table (Hosting, Raw log, Web), the web on Vercel, the layout tree, status line |
| `docs/architecture.md` | edit | §6 (fallback type, real costs), §7.1 (pull-raw), §10 #8 (glyphs and sprites resolved) |
| `docs/tasks/TASK-positions-consumer.md` | edit | E8: point to §5 here |
| `README.md` | edit | status, deploy section |

## 5. Verification

"Works" is not a criterion. Each phase is committed when its checks pass.

- **Phase 0:**
  - identity is `benito-admin`;
  - the AZ and Spot prices are recorded;
  - quota ≥ 4;
  - both budgets are confirmed with the user and created;
  - the `m7i-flex.large` Spot price is recorded.
- **Phase 1:**
  - `terraform plan` shows 0 changes after `apply`;
  - `aws ssm start-session` opens a shell;
  - the security group rules (`describe-security-group-rules`, not `nc`, which renewable-pulse
    found unreliable from this network) show ingress 80/443 only;
  - `findmnt /data` shows 30G; `swapon --show` shows 2G;
  - an attempted `aws s3api delete-object` on the raw bucket **from the box** returns
    `AccessDenied`.
- **Phase 2:**
  - `docker compose ps` shows all 6 services healthy;
  - `curl https://transit-pulse.duckdns.org/healthz` returns `refresh_ok: true`;
  - `/v1/vehicles/latest` returns a count within ±10% of a fresh direct ITS count for the same
    minute (one `curl` of the provider, done once by hand and politely);
  - no OOM kill (`docker inspect … OOMKilled`).
- **Phase 3:**
  - after 30 min, S3 holds the current hour's file and its size matches the local one;
  - after 26 h, the first manifest exists, and every line in it matches `shasum` of the local
    file;
  - the Mac's old raw log is uploaded and its manifest checks out;
  - all six alarms are triggered once and their emails arrive.
- **Phase 4:**
  - the Vercel URL renders the map, the table and the status strip with live data;
  - the browser's network panel shows `/api/v1/vehicles/latest` served same-origin, and
    `rio.pmtiles` from CloudFront with `206`;
  - 2 requests within 10 s from one location: the second shows a Vercel cache hit (record the
    header);
  - the same for `/api/v1/history` and an open hour; a closed hour is served with
    `immutable` and its second request is a cache hit; a hit's body is still gzip and its ETag
    matches the box's;
  - the time machine plays the last 3 h on the Vercel URL (60×, start to newest minute), with
    failed minutes drawn as gaps, as in D2's T6;
  - picking a line draws its route and stops; `/gtfs/index.json` is `max-age=0` and a route file
    is `immutable` (record both headers); `.vercel/output/static/tiles/` was absent from the
    upload;
  - S3 holds `raw/gtfs/v1/…` with a manifest line whose SHA-256 matches the Mac's file;
  - axe reports 0 violations on the deployed URL.
- **Phase 5:** §2.8 items 1–5, each recorded with numbers in §6.
- **Cost:** after 7 days, Cost Explorer's daily run rate × 30 for `project=transit-pulse` is
  within ±30% of §2.1.

## 6. Results

*(Filled in by the executing session, phase by phase.)*

### Phase 0 (2026-10-13)

- **Identity:** `aws sts get-caller-identity` → `arn:aws:iam::860897618882:user/benito-admin`.
- **Prerequisite:** `TASK-replay-flake.md` has its Results section filled; treated as done.
- **Spot price, last 24 h, Linux, USD/h (min–max):**

  | AZ | t4g.small | m7i-flex.large |
  |---|---|---|
  | us-east-1a | 0.0076–0.0078 | 0.0399–0.0405 |
  | us-east-1b | 0.0067 | 0.0401–0.0403 |
  | **us-east-1c** | **0.0064–0.0067** | 0.0397–0.0420 |
  | us-east-1d | 0.0078–0.0080 | 0.0444–0.0455 |
  | us-east-1f | 0.0090–0.0092 | 0.0373–0.0375 |

  **AZ chosen (user, 2026-10-13): `us-east-1c`.** The data volume is bound to it. renewable-pulse
  (`i-044f3e0dad659d507`, `t4g.small`, running) is in the same AZ, in its own VPC.
- **Fallback type:** `m7i-flex.large` (x86, 2 vCPU, 8 GiB) Spot is 0.0397–0.0420 USD/h in
  `us-east-1c`, about 6× `t4g.small` (≈ USD 29/month). Needs a `linux/amd64` image if ever used.
- **Quota `L-34B43A08`:** 8 vCPU (≥ 4 needed; renewable-pulse uses 2).
- **Budgets (user decision, 2026-10-13: add both).** `renewable-pulse-monthly` (USD 20) has **no
  filter**, so it was already account-wide and below the projects' combined ~USD 66. Created, outside
  Terraform, with the alerts of `renewable-pulse-monthly` (actual 50/85/100%, forecast 100%,
  `IncludeCredit=false`, email `benitopedro21@outlook.com`):
  - `transit-pulse-monthly`: USD 20, filter `TagKeyValue user:project$transit-pulse`. The tag only
    matches once `project` is **activated as a cost allocation tag** in Billing (up to 24 h to
    appear): `[VERIFY: activate it after Phase 1 creates tagged resources]`;
  - `account-total`: USD 80, no filter.
  `renewable-pulse-monthly` is left as it was.
- **Credits remaining:** not readable from the CLI (Cost Explorer shows October usage of ≈ USD 0 net
  of credit). `[VERIFY: user reads Billing console → Credits and records the balance here before
  Phase 2; if the month the credits run out is before 2026-12-31, tell the user]`

### Phase 1 (2026-10-13)

- **Applied:** the state bucket (`transit-pulse-tfstate-860897618882`, bootstrap state migrated into it,
  0 differences afterwards) and `prod` (47 resources). `terraform plan` after the last apply: no
  changes.
- **Instance:** `i-0226cf8c069a7d5f3`, `t4g.small` Spot, `us-east-1c`, EIP `52.2.45.217`, online in SSM
  (`aws ssm send-command` works; no SSH).
- **Checks (§5):**
  - security group ingress (`describe-security-group-rules`): tcp 80 and tcp 443 from `0.0.0.0/0`
    only;
  - `findmnt /data`: 29.9G xfs; `swapon --show`: `/swapfile` 2G; Docker 25.0.14, Compose v5.5.1;
  - from the box, `s3:PutObject` on the raw bucket succeeded, and both `delete-object` (no version)
    and `delete-object --version-id` returned `AccessDenied` (no `s3:DeleteObject` and no
    `s3:DeleteObjectVersion` in the role). The probe object `_probe/p.txt` (6 bytes) stays in the raw
    bucket because the box can't delete it; remove it with the admin user if wanted.
- **Deviation: CloudFront was refused.** `CreateDistributionWithTags` returned 403 *"Your account must
  be verified before you can add new CloudFront resources. To verify your account, please contact AWS
  Support"* (request `c7114380-24a2-4382-a42e-df8c81e32eec`). **Decision (user, 2026-10-13): the tiles
  bucket is public-read S3** (`s3:GetObject` for `*`, CORS exposing `Content-Range`, `Content-Length`,
  `ETag`), served at `https://transit-pulse-tiles-860897618882.s3.us-east-1.amazonaws.com`
  (`NEXT_PUBLIC_TILES_BASE_URL`). S3 honours `Range` natively; the `206` check moves to Phase 4.
  CloudFront can be added later if a Support case verifies the account. The raw and ops buckets stay
  private.
- **Still open:** the cost-allocation tag `project` must be activated in Billing for
  `transit-pulse-monthly` to match; the credit balance (Phase 0); the SSM parameters
  `/transit-pulse/prod/POSTGRES_PASSWORD` and `/ITS_CONTACT` hold `UNSET`; the DuckDNS name.

### Phase 2 (2026-10-14, first deploy `54dc9d504d8d`)

- **Deploy:** `make deploy` built the arm64 image (16.7 MB), pushed `transit-pulse/go:54dc9d504d8d`
  and ran `remote-deploy.sh` over SSM. `https://transit-pulse.duckdns.org/healthz` returned 200 after
  15 s (`vehicles: 2816`, `refresh_ok: true`). Caddy got its certificate on the first start.
- **Services:** `docker compose ps` shows all 6 up (redpanda and timescaledb healthy). `OOMKilled=false`
  and `RestartCount=0` on all 6.
- **First memory reading** (about 3 minutes after start, backfill running; **not** the Phase 5
  measurement): caddy 15 MiB, consumer 25, api 7, adapter-its 18, redpanda 155, timescaledb 122;
  host 588 of 1840 MiB used, swap 3 MiB; `/data` 928 MiB (4%).
- **Public surface:** `/v1/vehicles/latest` returns `Cache-Control: public, max-age=10, s-maxage=10`,
  `Content-Encoding: gzip` and a weak `ETag` through Caddy; `/foo` and `/` return 404 by design (Caddy
  proxies only `/v1/*` and `/healthz`; the web app is on Vercel, Phase 4).
- **Count check (§5, ±10%): NOT MET, 89.1%.** The API's snapshot covers a 5-minute window
  (`window_seconds: 300`). Like-for-like: distinct `id_veiculo` from 17 direct provider requests over
  the 5 minutes ending 2026-10-14T00:26Z = 4138; the API said 3689 → ratio 0.891. Two earlier
  attempts compared the wrong quantities and are discarded (one minute vs a 5-minute window: 3021
  distinct in one minute; a `(fornecedor, id_veiculo)` key counts a vehicle once per supplier).
  Cause not established. `[VERIFY: repeat once the backfill has finished and the box is steady, and
  check whether the snapshot window is by event_time while minuto_utc is by arrival, and whether
  vehicles with invalid/unmapped fixes explain the gap]`.
- **Deviations from the plan:**
  - the Go services have no container healthcheck (distroless has no shell or curl);
  - no adapter state directory (the adapter keeps no state outside the raw log), so only `/data/raw`
    is mounted;
  - `remote-deploy.sh` treats a 503 "no snapshot yet" as "up, data pending" (exit 0); a 502 or no
    answer fails;
  - `s-maxage=10` added to the snapshot in the first image;
  - timescaledb sizing: the image's own tune script reads the cgroup limit (verified in
    `001_timescaledb_tune.sh`), so `mem_limit: 600m` is the tuning input.

### Phase 4, first deploy (2026-10-14)

- **Public URL:** `https://transit-pulse-tau.vercel.app` (Vercel project `transit-pulse`, team
  `benito-3af53aa4`, built locally with `vercel build --prod`, uploaded with
  `vercel deploy --prebuilt --prod`; `.vercel/output/static/tiles/` removed before the upload).
  **`transit-pulse.vercel.app` is someone else's app: do not use it.** The duckdns name can't carry the
  page: it points at the box, which the rewrite needs.
- **User decisions (2026-10-14):** the site is public, so Deployment Protection (`ssoProtection`) was
  turned off through the Vercel API; "the dashboard on the live domain" was answered with the Vercel
  URL. `enableExternalRewriteCaching` was set to `true` on the project.
- **Env:** `API_ORIGIN` and `NEXT_PUBLIC_TILES_BASE_URL` were read from the shell by `vercel build`
  (not stored as project env vars): the next deploy must pass them again, so `scripts/web-deploy.sh`
  has to set them.
- **Tiles:** `rio.pmtiles` (44,628,646 B) is on the public S3 tiles bucket. A `Range: 0-15` request
  returned `206 Partial Content` with `Content-Range: bytes 0-15/44628646` and
  `Access-Control-Allow-Origin: *`. Glyphs and sprites still come from Protomaps' hosted assets
  (architecture §10 #8 stays open).
- **Checked from the command line:**
  - `/`, `/tabela`, `/api/v1/vehicles/latest` (68,395 B gzip), `/api/v1/history` and `/gtfs/index.json`
    return 200 on the public URL;
  - two snapshot requests in a row: `x-vercel-cache: HIT`, `age: 1`, `content-encoding: gzip`, the
    same ETag as the box (`W/"2026-10-14T00:33:00Z.2026-10-14T00:35:11Z"`);
  - `/gtfs/index.json` is `max-age=0, must-revalidate` (Next's default for `public/`).
- **Not done yet:** the browser checks (map renders, `206` from S3 in the network panel, time machine,
  line route and stops, axe 0 violations); `Cache-Control: immutable` on `/gtfs/:feed/:file`
  (`next.config.ts` `headers()`); a closed history hour served as `immutable` and cached;
  `scripts/web-deploy.sh` and the `make web-deploy`, `tiles-upload` and `gtfs-raw-upload` targets.
- **GTFS raw file on S3:** `raw/gtfs/v1/2026/10/13/22.raw.gz` (25,289,339 B), manifest
  `manifests/gtfs/2026/10/13.sha256` with SHA-256 `4f3217534d3327d1…df0cbb7`, checked by downloading
  the object and hashing it (matches the Mac's file). The upload was multipart, so S3's checksum is
  composite (`…-4`): to compare against `head-object` directly the CLI's `multipart_threshold` must
  be above the file size (as the §2.5 `[VERIFY]` expected).
