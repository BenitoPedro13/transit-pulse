# TASK-ci-cd

> Written 2026-10-13. Adds CI (lint, tests, build on every push and PR) and CD (deploy the box and
> the web from `main`) with GitHub Actions. It copies renewable-pulse's pattern
> (`renewable-pulse/.github/workflows/ci.yml`, `renewable-pulse/docs/tasks/TASK-aws-infra.md` §2.5)
> and changes what this project needs differently.
>
> **Two parts, run at different times:**
> - **Part A (CI)** needs nothing but the GitHub repo. Run it now, before or alongside
>   `TASK-aws-infra.md`.
> - **Part B (CD)** needs `TASK-aws-infra.md` Phases 1, 2 and 4 to be done (the ECR repo, the box,
>   `scripts/deploy.sh`, `scripts/web-deploy.sh` and the Vercel project). Until then the deploy jobs
>   skip cleanly.
>
> Rules for the executing session: every `[VERIFY: …]` is resolved against the live docs before the
> code that depends on it is written, and the answer is recorded in §6. Every third-party action is
> pinned to a full commit SHA, with the release tag in a comment.

## 1. Current scenario

- **The remote already exists.** `origin` = `https://github.com/BenitoPedro13/transit-pulse`,
  **public**, created 2026-10-12, in sync with local `main` (checked 2026-10-13). CLAUDE.md
  §4 ("There is no CI yet (no remote …)"), `TASK-scaffold.md` §2.7 and `TASK-aws-infra.md` §1/§2.9
  still say there's no remote. That's stale.
- **No `.github/`** in the repo, and no Actions secrets.
- **What a check run means today** is the Makefile:
  - `make lint`: `gofmt -l`, `go vet`, `golangci-lint run` (v2.14.0, `.golangci.yml`),
    `pnpm --dir apps/web lint` (ESLint);
  - `make test`: `go test -count=1 ./...` (testcontainers starts real Redpanda v26.2.4 and
    `timescale/timescaledb-ha:pg18.6-ts2.30.2`), then `pnpm --dir apps/web test` (Vitest);
  - `make web-build`: `next build`.
  - Go `1.26.0` with `toolchain go1.26.9`; pnpm `11.21.0` (`apps/web/package.json`
    `packageManager`); Node 24.
- **Deploys** are planned as two Mac-only scripts in `TASK-aws-infra.md`: `make deploy` (Go image
  → ECR → SSM restart of the box) and `make web-deploy` (GTFS raw ZIP → S3, `vercel build`,
  drop `tiles/`, `vercel deploy --prebuilt`).
- **The web build depends on files git doesn't have.** `apps/web/public/gtfs/` (D3, 14 MB, 495
  files) comes from `make gtfs`, which needs a provider request and the raw ZIP in
  `var/raw/gtfs/`. `make gtfs-export` rebuilds it offline from `var/gtfs/state.json` + the raw
  file (`apps/gtfs/main.go:38`, flags `-state` and `-out`; it reads `RAW_DIR`).
- **Most commits are docs only** (the plan-only workflow: task documents, then code). A deploy on
  every push would restart the box for nothing.

## 2. Planned changes

### 2.1 Part A: CI (`.github/workflows/ci.yml`)

Triggers: `push` to `main`, `pull_request` to `main`, and `workflow_dispatch` with an optional
`sha` input (Part B's rollback; it runs only the deploy jobs, for that SHA). `concurrency: ci-${{ github.ref }}`,
`cancel-in-progress: true` for the check jobs only.

| Job | Runner | Steps |
|---|---|---|
| `changes` | `ubuntu-latest` | `dorny/paths-filter` `[VERIFY: current release]` → outputs `go`, `web`, `infra`, `docs_only` (filters in §2.3) |
| `go` | `ubuntu-24.04-arm` | `actions/setup-go` with `go-version-file: go.mod`; `gofmt -l` empty; `go vet ./...`; `golangci-lint` via `golangci/golangci-lint-action` pinned to **v2.14.0** (same as local); `go test -count=1 ./...` |
| `web` | `ubuntu-24.04-arm` | `pnpm/action-setup` (version from `packageManager`, see note), `actions/setup-node` Node 24 with pnpm cache keyed on `apps/web/pnpm-lock.yaml`; `pnpm install --frozen-lockfile`; `lint`; `test`; `build` |
| `terraform` | `ubuntu-latest` | `hashicorp/setup-terraform` 1.16.4; `terraform fmt -check -recursive infra/terraform`; `terraform init -backend=false` + `validate` in `bootstrap/` and `prod/`. No AWS credentials. Skipped until `infra/terraform/` exists |

- **Arm64 runners**, because the box is arm64, the images are built for arm64, and
  `timescaledb-ha` is used as arm64 locally. Free on public repos (renewable-pulse uses
  them) `[VERIFY: GitHub docs, arm64 hosted runners for public repositories, label
  ubuntu-24.04-arm]`. Docker is preinstalled on it `[VERIFY: the runner image README]`, so
  testcontainers needs no setup.
- **The jobs run whenever their own paths changed**, and always on `main` pushes that touch code.
  A docs-only change runs `changes` and nothing else. The required checks are written so a
  skipped job counts as passing (see §2.4).
- **No `public/gtfs/` in CI.** `next build` doesn't read it (the routes are fetched at runtime by
  `hooks/use-line-routes.ts`) `[VERIFY: a clean clone without public/gtfs/ builds; if it
  doesn't, record why and stop]`. No `public/tiles/` either.
- **pnpm version:** renewable-pulse pins `pnpm/action-setup` to an explicit version because a
  `packageManager` range resolved through Corepack hit a bug (its
  `TASK-railway-deploy.md` §5.1). Here `packageManager` is an exact version (`pnpm@11.21.0`), so
  let the action read it; if it fails, pin the same exact version and record why.
- **Testcontainers on CI:** the first run records the `go` job's duration and the image pull
  time. If image pulls dominate (> 50% of the job), cache nothing yet and record it; a cache is a
  later decision.
- **Flakiness is a failure.** Part A's check is 3 consecutive green runs of the same commit
  (`gh run rerun`). A flake found here gets its own task document, like `TASK-replay-flake.md`,
  never a retry loop in the workflow.

### 2.2 Part B: CD (jobs added to the same workflow)

Both deploy jobs run only on `push` to `main`, `needs:` every check job that ran, and use
`concurrency: deploy-${{ job }}` with **`cancel-in-progress: false`**, so a deploy is never cut off
halfway. Each one skips cleanly, with a message, until its prerequisites exist (renewable-pulse's
gate pattern): the `AWS_DEPLOY_ROLE_ARN` repository variable for the box, the `VERCEL_TOKEN`
secret for the web.

**`deploy-box`** (`ubuntu-24.04-arm`), when the `go` or `infra` filter matched:

1. `aws-actions/configure-aws-credentials` with `role-to-assume: ${{ vars.AWS_DEPLOY_ROLE_ARN }}`,
   region `us-east-1`, `permissions: id-token: write, contents: read`. **No AWS keys in GitHub.**
2. Run **the same `scripts/deploy.sh`** as the Mac. It already refuses a dirty tree, builds the
   one arm64 image, pushes `go:<sha>`, uploads `infra/aws/*` to `s3://ops/deploy/<sha>/`, runs
   `remote-deploy.sh <sha>` over SSM, and fails if `/healthz` doesn't return 200 within 120 s.
   The only change to the script: it reads `GITHUB_SHA` when it's set, and it works without a
   TTY. The Mac path stays as the fallback.
3. On failure, the job prints the SSM invocation's stdout/stderr. **No automatic rollback.** The
   previous image is still in ECR (the last 5 are kept), and rollback is
   `scripts/deploy.sh --sha <previous>` from the Mac or a `workflow_dispatch` run with a `sha`
   input.

**`deploy-web`** (`ubuntu-24.04-arm`), when the `web` filter (or the GTFS code) matched:

1. The same OIDC role (read-only on what it needs, §2.3).
2. **Rebuild `public/gtfs/` from the raw log, never from the provider:**
   - download `s3://<ops>/gtfs/state.json` and the raw file it points to
     (`s3://<raw>/raw/gtfs/v1/…`) into `var/`;
   - check the raw file's SHA-256 against its manifest line in `s3://<raw>/manifests/…`;
   - `go run ./apps/gtfs export -state var/gtfs/state.json -out apps/web/public/gtfs` with
     `RAW_DIR=var/raw`.
   - That keeps invariant 2 (the published routes are a projection of the stored raw ZIP) and
     invariant 4 (CI never calls SMTR). A new GTFS capture still happens on the Mac with
     `make gtfs`, and `make gtfs-raw-upload` must also upload `var/gtfs/state.json` to
     `s3://<ops>/gtfs/state.json` (a change to `TASK-aws-infra.md` §2.5 item 5, made by this task).
   - `[VERIFY: the export is byte-identical to the Mac's for the same raw file
     (TASK-line-routes.md says the export is deterministic); compare the two index.json SHA-256s]`
3. `vercel pull --yes --environment=production`, `vercel build --prod`, delete
   `.vercel/output/static/tiles/`, `vercel deploy --prebuilt --prod`, using `VERCEL_TOKEN`,
   `VERCEL_ORG_ID` and `VERCEL_PROJECT_ID` `[VERIFY: Vercel's current docs for deploying from
   GitHub Actions with the CLI, and the CLI version to pin]`. That's `scripts/web-deploy.sh`'s
   build and deploy steps; split the script so CI calls those steps and skips the raw upload
   (CI has no new raw file to upload).
4. **Vercel's own Git integration stays off** for this project (Vercel project → Git → disconnected, or
   `git.deploymentEnabled: false` in `apps/web/vercel.json` `[VERIFY: the current setting]`), so a
   push doesn't build twice and Vercel never builds from source without `public/gtfs/`.

### 2.3 AWS and repository settings

- **`infra/terraform/prod/github-oidc.tf`** (new):
  - **The OIDC provider already exists in the account** (renewable-pulse created
    `token.actions.githubusercontent.com`), and an account can only have one per URL. Read it
    with `data "aws_iam_openid_connect_provider"`; don't create it `[VERIFY: renewable-pulse's
    Terraform owns it; transit-pulse must never destroy it]`.
  - Role `transit-pulse-gha-deploy`, trust conditioned on
    `token.actions.githubusercontent.com:aud = sts.amazonaws.com` and
    `token.actions.githubusercontent.com:sub = repo:BenitoPedro13/transit-pulse:ref:refs/heads/main`.
    PRs and other branches can't assume it.
  - Permissions, nothing more:
    - ECR: `GetAuthorizationToken`, and push on `transit-pulse/go` only;
    - S3: `PutObject` on `<ops>/deploy/*`; `GetObject` on `<ops>/gtfs/state.json`,
      `<raw>/raw/gtfs/*` and `<raw>/manifests/*`; **no write and no delete on the raw bucket**;
    - SSM: `SendCommand` on this instance (by ARN) and the `AWS-RunShellScript` document;
      `GetCommandInvocation`.
  - Output: the role ARN, which the user sets as the repository variable `AWS_DEPLOY_ROLE_ARN`
    (a variable, not a secret: it isn't sensitive, and it's what gates the job).
- **Path filters** (`.github/filters.yml`, read by `dorny/paths-filter`):
  - `go`: `**/*.go`, `go.mod`, `go.sum`, `.golangci.yml`, `Dockerfile`, `.dockerignore`,
    `packages/contracts/**`, `docs/research/samples/**` (the tests read them);
  - `web`: `apps/web/**`, `packages/gtfs/**`, `apps/gtfs/**`;
  - `infra`: `infra/**`, `scripts/**`, `compose.yaml`, `Makefile`;
  - everything else (`docs/**`, `*.md`) runs no job.
- **Branch protection on `main`** (`gh api` from the Mac, by the executing session after the
  user confirms):
  - require the `ci-ok` check: a final job that `needs:` every check job, uses `if: always()`,
    and fails only if one of them failed or was cancelled (skipped counts as passing). One
    required check, so path-filtered skips never block a merge;
  - no required reviews (one person), no force-push, no deletion;
  - the user can still push straight to `main`. Protection then just marks a red commit; it
    doesn't block the push `[VERIFY: GitHub rulesets vs classic branch protection for "allow
    the admin to push but require checks"; pick the one that does that and record it]`.
- **Dependabot** (`.github/dependabot.yml`), weekly, for `github-actions` only. Go and npm bumps
  stay a deliberate task (CLAUDE.md §2.0: check each tool's docs before upgrading).

### 2.4 What the user does (only they can)

1. Create a Vercel token (scoped to the project's team) and add the Actions secrets
   `VERCEL_TOKEN`, `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID` (the IDs come from
   `apps/web/.vercel/project.json` after `vercel link`, done in `TASK-aws-infra.md` Phase 4).
2. After `terraform apply` of `github-oidc.tf`: set the repository variable `AWS_DEPLOY_ROLE_ARN`.
3. Confirm the branch protection settings before the session applies them.

### 2.5 Alternatives considered and rejected

- **Vercel's Git integration** (build on Vercel from each push): Vercel would need Go and the raw
  GTFS file to make `public/gtfs/`, or would call SMTR on every build (invariant 4). Building in
  Actions from the raw log avoids both.
- **Committing `public/gtfs/`** (14 MB of generated files): every export would churn the history
  with a projection, and invariant 2 says projections are rebuilt, not stored.
- **Stored AWS access keys:** OIDC needs none, as in renewable-pulse.
- **`terraform apply` in CI:** one operator, and a mistake can destroy the data volume or the raw
  bucket. `plan`/`apply` stay manual from the Mac; CI runs `fmt` and `validate` only.
- **Deploying the box on every `main` push:** most commits are docs. Path filters keep restarts to
  code changes (the adapter backfills after a restart, so a restart loses nothing, but it's still
  a needless gap in the live view).
- **x86 runners + QEMU for the image:** slower, and the native arm64 runner is free here.
- **Automatic rollback:** `/healthz` failing is visible and the previous image is one command
  away. An automated rollback would need its own health criteria; not worth it for one box.

## 3. Why

- **Tests run on a clean machine on every change**, not only in the Sonnet session that wrote
  the code, so a check that passes only on the Mac (a cached container, a file in `var/`) shows up.
- **A merge to `main` is a deploy**, with the same scripts as the Mac and no AWS keys stored
  anywhere, so the live run during the replay days (architecture §9) doesn't depend on the Mac.
- **Cost:** USD 0 in Actions minutes (public repo); one IAM role; one Vercel token. About half a
  day of a Sonnet session for Part A, and a couple of hours for Part B.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `.github/workflows/ci.yml` | new | Part A jobs, then Part B's `deploy-box` and `deploy-web`, `ci-ok` |
| `.github/filters.yml` | new | path filters (§2.3) |
| `.github/dependabot.yml` | new | `github-actions`, weekly |
| `infra/terraform/prod/github-oidc.tf` | new (Part B) | data source for the existing provider, the deploy role, its policy, output |
| `scripts/deploy.sh` | edit (Part B) | `GITHUB_SHA`, no TTY, `--sha` for rollback |
| `scripts/web-deploy.sh` | edit (Part B) | split: raw upload (Mac only) vs build + deploy (Mac and CI) |
| `Makefile` | edit (Part B) | `gtfs-raw-upload` also uploads `var/gtfs/state.json` to ops |
| `apps/web/vercel.json` | new (Part B), if that's where Git deploys are switched off | |
| `docs/tasks/TASK-aws-infra.md` | edit | §1 and §2.9: the remote exists, CI/CD is this task; §2.5 item 5: `state.json` upload |
| `docs/tasks/TASK-scaffold.md` | edit | §2.7: point to this task |
| `CLAUDE.md` | edit | §4: "There is no CI yet" → CI/CD in `.github/workflows/ci.yml`; stack table row for CI/CD; layout tree gets `.github/` |
| `README.md` | edit | CI badge, a "Deploying" section (push to `main`; the Mac fallback) |

## 5. Verification

"Works" is not a criterion.

- **Part A:**
  - a push touching Go and web runs `go`, `web` and `ci-ok`; all green **3 runs in a row** on the
    same commit (record the run IDs and each job's duration; median of 3);
  - a docs-only push runs `changes` and `ci-ok` only (record the run);
  - a PR from a branch with a deliberate `gofmt` violation fails `go` and `ci-ok`; the branch is
    then deleted (ask first);
  - `grep -rn "uses:" .github/workflows | grep -v "@[0-9a-f]\{40\}"` is empty (every action pinned
    to a SHA);
  - a clean clone without `public/gtfs/` passes `web` (`next build`).
- **Part B:**
  - before the role variable and Vercel secret exist, both deploy jobs skip with their message;
  - a `main` push changing a Go file deploys the box: the job is green, `curl
    https://transit-pulse.duckdns.org/healthz` reports the new image (record how: the api's
    version log line or `docker inspect` over SSM), no OOM kill;
  - a `main` push changing only `apps/web/` deploys only the web; the box's containers keep
    their start time;
  - CI's `public/gtfs/index.json` SHA-256 equals the Mac's for the same raw file;
  - the Vercel URL renders the map, a line's route, the time machine and `/tabela`; axe 0
    violations;
  - a PR run can't assume the role (the OIDC step fails, as expected); `grep -r AKIA .github` is
    empty;
  - the deploy role can't write to or delete from the raw bucket: every raw-bucket write with the role's
    permissions is denied: `aws iam simulate-principal-policy` on the role for
    `s3:DeleteObject` and `s3:PutObject` on `<raw>/*` returns `implicitDeny` for both;
  - rollback drill: `workflow_dispatch` with the previous SHA restores it, `/healthz` 200.
- **Commit** each part when its checks pass (no `Co-Authored-By`).

## 6. Results

*(Filled in by the executing session.)*
