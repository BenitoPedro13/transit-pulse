# One image, four binaries (docs/tasks/TASK-aws-infra.md §2.4). compose picks the binary with
# `command:`. Built natively for linux/arm64 on the Mac: the box is a t4g.small (Graviton).
FROM golang:1.26.9@sha256:d7722066f0b60ceccb6c0643cbed1f5f9e15506ac237146c95333504d7805d89 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps ./apps
COPY packages ./packages
ENV CGO_ENABLED=0 GOOS=linux GOARCH=arm64
RUN for b in adapter-its consumer api rebuild; do \
      go build -trimpath -ldflags="-s -w" -o /out/$b ./apps/$b || exit 1; \
    done

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/ /app/
