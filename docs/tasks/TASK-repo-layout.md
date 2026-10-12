# TASK: `apps/` + `packages/` repo layout

**Status (2026-10-12): done.** L1–L6 pass: `go list` shows only the two `packages/*`; `make lint` 0 issues; `make test` ok, 0 skipped; `make web-build` ok; every tracked move is a rename. Git paired the two identical empty `.gitkeep` files crosswise (`schemas/.gitkeep → apps/web/hooks/.gitkeep`); both paths exist, so it is cosmetic.

Originally Asked for by the user on 2026-10-12, after the scaffold
(`docs/tasks/TASK-scaffold.md`) and before the first adapter, while there are only 4 Go
packages and one Next.js app to move.

## 1. Current scenario

The scaffold put the Go code in the usual Go layout and the web app at the top level:

```
internal/testharness/   internal/samples/   schemas/   web/   (cmd/<name>/ planned, not created)
```

The scaffold rejected the renewable-pulse-style `apps/` + `packages/` layout (TASK-scaffold
§2.1). Its reason was that there's no workspace tooling to share code between packages. The user
prefers the `apps/` + `packages/` layout anyway, for the same top-level shape as
renewable-pulse (`apps/{api,consumer,ingest,web}`, `packages/{config,contracts}`): you can
see where every deployable and every shared piece lives without knowing Go's conventions.

Nothing outside the repo depends on these paths: there's no CI, no remote build and nothing
deployed.

## 2. Planned changes

The **folders** change. The **tooling** decisions from the scaffold stay as they are:

- **One Go module at the root.** The rejection of one module per app + `go.work`
  (TASK-scaffold §2.1) still holds. `apps/` and `packages/` are directories, not modules.
- **`apps/web` stays a standalone pnpm project.** It keeps its own `package.json`,
  `pnpm-lock.yaml` and settings-only `pnpm-workspace.yaml`. No root `package.json` and no
  Turborepo: there's still exactly one JS package. A root pnpm workspace is added the day a
  second JS package appears (e.g. TS types generated from `packages/contracts`).

### 2.1 Target layout

```
apps/
  web/               ← web/ (Next.js; untracked node_modules/.next move with it)
  adapter-its/       (arrives with TASK-its-adapter: Go `package main`)
  consumer/  api/    (arrive with their tasks: Go `package main`)
packages/
  testharness/       ← internal/testharness/
  samples/           ← internal/samples/
  contracts/         ← schemas/ (JSON Schema; empty until the canonical-event task)
infra/  scripts/  docs/   unchanged
```

- **Go binaries live in `apps/<name>/`** (`package main`), replacing the planned
  `cmd/<name>/`. `go build ./apps/adapter-its` works the same way, because Go doesn't give
  the name `cmd` any special meaning.
- **Shared Go packages live in `packages/<name>/`.** Import paths become
  `github.com/BenitoPedro13/transit-pulse/packages/<name>`.
- **`schemas/` → `packages/contracts/`,** the same name renewable-pulse uses. The JSON Schema
  files are the language-neutral contract between the Go apps and `apps/web`.

### 2.2 File-by-file

- `git mv internal/testharness packages/testharness` and `git mv internal/samples
  packages/samples`. Both resolve repo paths with `runtime.Caller(0)` + `../..`. They stay
  exactly two levels deep, so `harness.go:87` and `samples.go` need **no path change**. Only
  the import paths in the two `_test.go` files change.
- `git mv schemas packages/contracts` (moves `.gitkeep`).
- `mv web apps/web`. A plain `mv`, so the untracked `node_modules/` and `.next/` move with it
  and no reinstall is needed. Then `git add` both paths, and git detects the renames.
- `go.mod`: `ignore ./web` → `ignore ./apps/web`.
- `Makefile`: `--dir web` → `--dir apps/web` (lint, web-dev, web-build).
- `compose.yaml`: two comments that name `internal/testharness/harness.go`.
- `CLAUDE.md` §0 stack table and §4 tree: new paths; "each binary arrives as
  `apps/<name>/main.go`".
- `README.md`: the `web/` paths in Setup (`.nvmrc`, `package.json`, `pnpm --dir`).
- `docs/tasks/TASK-scaffold.md`: **not rewritten.** It's the historical record of what the
  scaffold did. A one-line note at the top points here.

### 2.3 Alternatives considered and rejected

- **Keep Go's `internal/` for the shared packages** (`apps/` + `internal/`). It keeps the
  compiler's import fence, but the user wants the `packages/` shape. The fence protects
  against importers outside this module, and nothing here is published, so it's worth
  nothing in practice.
- **`packages/go/<name>` and `packages/ts/<name>`.** Organising by language adds a level
  of nesting and changes the `../..` depth in two files, for one TS package that doesn't
  exist yet.
- **A root pnpm workspace + Turborepo now**, as in renewable-pulse. It's tooling for
  exactly one package. Deferred until there's a second one (see §2).
- **Folding this into the adapter task.** It would mix a mechanical move with new code in
  one diff and make the adapter task bigger for Sonnet. Doing it first means the adapter task
  is written against the final paths.

## 3. Why

- Every deployable is in `apps/` and every shared piece in `packages/`, the same shape as
  renewable-pulse, so moving between the two repos needs no translation.
- It's cheapest now: 2 Go packages, 1 empty directory and 1 app. Every later task adds files
  under these paths.
- Cost: one move commit and the loss of the `internal/` import fence, which doesn't matter
  here (§2.3).

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `internal/testharness/*` → `packages/testharness/*` | move | `harness_test.go` import path |
| `internal/samples/*` → `packages/samples/*` | move | `samples_test.go` import path |
| `schemas/.gitkeep` → `packages/contracts/.gitkeep` | move | |
| `web/**` → `apps/web/**` | move | untracked `node_modules`/`.next` moved too, not reinstalled |
| `go.mod` | edit | `ignore ./apps/web` |
| `Makefile` | edit | `pnpm --dir apps/web` ×3 |
| `compose.yaml` | edit | two comments |
| `CLAUDE.md` | edit | §0 stack table, §4 tree and binary convention |
| `README.md` | edit | Setup paths |
| `docs/tasks/TASK-scaffold.md` | edit | one-line pointer to this task |

## 5. Acceptance criteria

- **L1** `go list ./...` lists exactly `packages/samples` and `packages/testharness`, and
  nothing under `apps/web/node_modules`.
- **L2** `make lint` exits 0 (gofmt, vet, golangci-lint 0 issues, ESLint clean).
- **L3** `make test` exits 0 with no test skipped (real Redpanda + TimescaleDB).
- **L4** `make web-build` exits 0.
- **L5** `git status` shows renames (`R`), not delete + add, for every tracked moved file.
- **L6** `grep -rn "internal/\|schemas/\|[^/]web/" --exclude-dir={node_modules,.next,.git}`
  outside `docs/tasks/TASK-scaffold.md`, `TASK-source-inventory.md` and
  `TASK-repo-setup.md` returns no stale path.
