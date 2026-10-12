.PHONY: up down test lint fmt web-dev web-build adapter-its consumer rebuild tiles

GO_DIRS = $$(go list -f '{{.Dir}}' ./...)

.env:
	cp .env.example .env

up: .env
	docker compose up -d --wait --wait-timeout 120

# Keeps the volumes. Deleting them (`down -v`) is deliberately not a target.
down: .env
	docker compose down

# -count=1 so the integration test is never served from cache. Needs Docker running.
test:
	go test -count=1 ./...

lint:
	test -z "$$(gofmt -l $(GO_DIRS))"
	go vet ./...
	golangci-lint run ./...
	pnpm --dir apps/web lint

fmt:
	gofmt -w $(GO_DIRS)

web-dev:
	pnpm --dir apps/web dev

web-build:
	pnpm --dir apps/web build

# Needs `make up` (Redpanda). Reads .env; stop it with Ctrl-C (it exits 0 within 10 s).
adapter-its: .env
	set -a && . ./.env && set +a && go run ./apps/adapter-its

# Needs `make up`. Reads .env; stop it with Ctrl-C (it exits 0 within 10 s). Run it next to
# `make adapter-its`, or after it: a new group starts at the oldest record the topics still hold.
consumer: .env
	set -a && . ./.env && set +a && go run ./apps/consumer

# Rebuilds positions and feed_status from the raw log for FROM..TO (both minutes inclusive, RFC 3339
# UTC). Writes DATABASE_URL from .env, or DB=postgres://... to rebuild into another database.
#   make rebuild FROM=2026-10-12T14:27:00Z TO=2026-10-12T16:24:00Z
rebuild: .env
	test -n "$(FROM)" -a -n "$(TO)"
	set -a && . ./.env && set +a && DATABASE_URL="$(if $(DB),$(DB),$$DATABASE_URL)" go run ./apps/rebuild -from $(FROM) -to $(TO)

# Rio basemap extract (Protomaps daily build, ~45 MB) -> apps/web/public/tiles/rio.pmtiles.
# Needs the pmtiles CLI (`brew install pmtiles`). Override the build: make tiles TILES_BUILD=20261012.pmtiles
# Daily builds are kept for a week, so the default is the newest one in the build index.
# Progress bar goes to /dev/null: it is thousands of carriage-return lines when not on a TTY.
TILES_BBOX = -43.80,-23.10,-42.70,-22.45
TILES_BUILD ?= $(shell curl -fsS https://build-metadata.protomaps.dev/builds.json | jq -r '.[-1].key')
tiles:
	test -n "$(TILES_BUILD)"
	mkdir -p apps/web/public/tiles
	pmtiles extract https://build.protomaps.com/$(TILES_BUILD) apps/web/public/tiles/rio.pmtiles --bbox=$(TILES_BBOX) --maxzoom=15 2>&1 | grep -E 'Completed|Extract|rror'
