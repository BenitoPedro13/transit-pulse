.PHONY: up down dev test lint fmt web-dev web-build adapter-its consumer rebuild api tiles gtfs gtfs-export deploy

GO_DIRS = $$(go list -f '{{.Dir}}' ./...)

.env:
	cp .env.example .env

up: .env
	docker compose up -d --wait --wait-timeout 120

# Keeps the volumes. Deleting them (`down -v`) is deliberately not a target.
down: .env
	docker compose down

# -count=1 so the integration test is never served from cache. Needs Docker running. The web
# tests (Vitest, pure functions in apps/web/hooks) run after the Go ones.
test:
	go test -count=1 ./...
	pnpm --dir apps/web test

lint:
	test -z "$$(gofmt -l $(GO_DIRS))"
	go vet ./...
	golangci-lint run ./...
	pnpm --dir apps/web lint

fmt:
	gofmt -w $(GO_DIRS)

web-dev:
	pnpm --dir apps/web dev

# The whole screen in one terminal: local infra, the read-only API (in the background, built to
# var/api so Ctrl-C stops the real process) and the Next dev server (in the foreground). Stop with
# Ctrl-C. The database must already be filled (`make consumer` or `make rebuild`). To look at another
# database, such as the recorded run: make dev DB=postgres://.../transit_pulse_run?sslmode=disable
dev: up
	go build -o var/api ./apps/api
	( set -a; . ./.env; set +a; DATABASE_URL="$(if $(DB),$(DB),$$DATABASE_URL)" exec var/api ) & api=$$!; \
	trap 'kill $$api 2>/dev/null' EXIT INT TERM; \
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

# Needs `make up` and a filled database (`make consumer` or `make rebuild`). Reads .env; stop it
# with Ctrl-C. Listens on API_ADDR (default 127.0.0.1:8080); `make web-dev` reaches it through the
# /api rewrite in apps/web/next.config.ts. Read-only: it never migrates or writes.
api: .env
	set -a && . ./.env && set +a && go run ./apps/api

# Prepare static line geometry before web-build. One conditional fetch per 24 h maximum.
gtfs: .env
	set -a && . ./.env && set +a && go run ./apps/gtfs fetch

# Offline rebuild from the retained raw location in var/gtfs/state.json.
gtfs-export: .env
	set -a && . ./.env && set +a && go run ./apps/gtfs export

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

# Build the arm64 image, push it to ECR (tag = git SHA), upload infra/aws and run remote-deploy.sh on
# the box through SSM. Refuses a dirty tree. Needs `aws login` and Docker (TASK-aws-infra.md §2.4).
deploy:
	scripts/deploy.sh
