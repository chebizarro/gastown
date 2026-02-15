# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
  build-essential pkg-config git ca-certificates \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=1970-01-01T00:00:00Z
ENV CGO_ENABLED=1

RUN go build -trimpath -o /out/gt \
  -ldflags "\
    -s -w \
    -X github.com/steveyegge/gastown/internal/cmd.Version=${VERSION} \
    -X github.com/steveyegge/gastown/internal/cmd.Commit=${COMMIT} \
    -X github.com/steveyegge/gastown/internal/cmd.BuildTime=${BUILD_TIME} \
    -X github.com/steveyegge/gastown/internal/cmd.BuiltProperly=1 \
  " \
  ./cmd/gt

# beads CLI (bd) is required by agentloop tools (bd_show, bd_list, bd_update).
# NOTE: @latest is non-reproducible; pin in production if needed.
RUN GOBIN=/out go install github.com/steveyegge/beads/cmd/bd@latest


FROM debian:bookworm-slim AS runtime

# Runtime tool deps required by agentloop.Executor:
# - git: git_* tools
# - bash: shell_exec
# - grep: file_search
# - bd: beads tools
# Plus operational deps:
# - ca-certificates: WSS/HTTPS (relays, bunker, Blossom)
# - curl: health checks
# - tmux: used by other GT paths; harmless in API-mode deployments
RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates git bash grep tmux curl \
  && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 10001 -s /bin/bash gastown

COPY --from=builder /out/gt /usr/local/bin/gt
COPY --from=builder /out/bd /usr/local/bin/bd

WORKDIR /gt
USER gastown:gastown

ENTRYPOINT ["gt"]
