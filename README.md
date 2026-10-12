# Transit Pulse

A live public-transport map for **Greater Rio de Janeiro**. It starts with the city's buses
and BRT, then adds the state-run trains, metro, tram, ferries and intercity buses. Every
operator is shown honestly as **live**, **scheduled only** or **no public data**.

It's also a portfolio case study in distributed, data-heavy real-time systems:

- one adapter per provider, all emitting one canonical event;
- an immutable raw log on S3;
- rebuildable projections;
- a static replay that keeps working after the live backend is switched off.

**Status: planning — no code yet.**

## Docs

- [`CLAUDE.md`](CLAUDE.md): workflow, decisions, invariants, tests
- [`docs/architecture.md`](docs/architecture.md): design skeleton and open questions
- [`docs/research/README.md`](docs/research/README.md): the source inventory (unverified)
- [`docs/tasks/`](docs/tasks/): task documents
