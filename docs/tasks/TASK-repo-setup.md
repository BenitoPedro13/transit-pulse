# TASK: Set up the repo (planning only)

**Status (2026-10-12): done.** Written before any other file in the repo.

## 1. Current scenario

`~/Documents/personal/transit-pulse` doesn't exist yet. The project has been planned in
conversation only. That planning settled these decisions:

- Rio first.
- Provider adapters that emit one canonical event.
- Three honest coverage tiers.
- Four phases.
- Cheap AWS hosting copied from renewable-pulse.
- A short live run that ends in a static replay.
- Storage discipline.
- A MapLibre + deck.gl map.

Nothing has been verified against a real source. Every claim so far about which Rio
operators publish live GPS, static GTFS or nothing at all came from memory.

The sibling repos (`apuracao-2026`, `renewable-pulse`, `flora`) share one workflow: a
`CLAUDE.md` in §0–§4 + TL;DR, task documents in `docs/tasks/`, and verified facts with
captured samples in `docs/research/`. Apuração is the closest model: a polled public feed,
an immutable raw log, idempotent projections and a replay.

## 2. Planned changes

This task covers the repo, git, `CLAUDE.md` and `docs/` only. It adds no application code,
no scaffold, no infrastructure and no package-manager files.

1. **`git init`** with default branch `main`. No GitHub remote; the user decides that
   separately.
2. **`docs/tasks/TASK-repo-setup.md`**: this document, written first.
3. **`CLAUDE.md`** uses the sibling structure: §0 project context (decisions, a stack table
   with a status column, how to write, invariants, tests), §1 plan before executing, §2 use
   CLIs (with §2.0), §3 update documentation, §4 project conventions (with §4.1 commits), and
   a TL;DR. Every stack row is marked **planned**. The backend language, frontend framework
   and stream-processing approach are left as open questions.
4. **`README.md`**: what the project is, "Status: planning — no code yet", and links to the
   docs.
5. **`docs/architecture.md`**: a skeleton holding the decisions, the canonical-event draft,
   the phases, a hosting and cost sketch, the replay-first ending, and an **Open questions**
   section.
6. **`docs/research/README.md`**: the source inventory as a table, one row per operator.
   Every unconfirmed cell is `[VERIFY]`. The table isn't filled in from memory; filling it is
   `TASK-source-inventory.md`.
7. **`docs/research/samples/.gitkeep`**: an empty folder for captured samples.
8. **`.gitignore`**: OS and editor files, `.env*`, and large files in
   `docs/research/samples/`.
9. **One commit**, with a plain message and no `Co-Authored-By` trailer.

Alternatives I rejected:

- **Pre-filling the source inventory from memory.** Rejected because every availability claim
  is unverified, and a confident wrong URL is worse than a `[VERIFY]`.
- **Choosing the backend language and frontend framework now, by copying Apuração's
  TypeScript or renewable-pulse's Go.** Rejected because the user left those choices open.

## 3. Why

- **It unblocks `TASK-source-inventory.md`**, the next task. That task verifies every Rio
  source live and captures real samples, and it needs somewhere to put them.
- **It puts the decisions in writing** before code exists, so later tasks build against a
  contract instead of conversation memory.
- **Cost:** a few documents. No money, no infrastructure.

## 4. Affected files

| File | Change type | Notes |
|---|---|---|
| `docs/tasks/TASK-repo-setup.md` | new | this document |
| `CLAUDE.md` | new | workflow guidelines, sibling structure |
| `README.md` | new | short; status "planning — no code yet" |
| `docs/architecture.md` | new | skeleton plus open questions |
| `docs/research/README.md` | new | source inventory, every cell `[VERIFY]` |
| `docs/research/samples/.gitkeep` | new | empty samples folder |
| `.gitignore` | new | OS/editor files, `.env*`, large samples |
