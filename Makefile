.PHONY: up down test lint fmt web-dev web-build

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
