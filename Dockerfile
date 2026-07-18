# syntax=docker/dockerfile:1.7

FROM golang:1.26.2-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
  build-essential pkg-config git ca-certificates \
  libicu-dev \
  && rm -rf /var/lib/apt/lists/*

COPY --from=cascadia-go . /cascadia-go
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
    -X github.com/steveyegge/internal/cmd.Version=${VERSION} \
    -X github.com/steveyegge/internal/cmd.Commit=${COMMIT} \
    -X github.com/steveyegge/internal/cmd.BuildTime=${BUILD_TIME} \
    -X github.com/steveyegge/internal/cmd.BuiltProperly=1 \
  " \
  ./cmd/gt

# beads CLI (bd) is required by agentloop tools (bd_show, bd_list, bd_update).
# NOTE: @latest is non-reproducible; pin in production if needed.
RUN GOBIN=/out go install github.com/steveyegge/beads/cmd/bd@latest


FROM node:22-bookworm-slim AS runtime

ARG CLAUDE_CODE_VERSION=latest
ARG DOLT_VERSION=latest
ARG TARGETARCH

# Runtime tool deps required by agentloop.Executor:
# - git: git_* tools
# - bash: shell_exec
# - grep: file_search
# - bd: beads tools
# Plus operational deps:
# - ca-certificates: WSS/HTTPS (relays, bunker, Blossom)
# - curl: health checks
# - tmux: used by other GT paths; harmless in API-mode deployments
# - nodejs/npm: Claude Code CLI runtime
# - gosu: drop privileges after fixing bind-mount ownership
# - procps: deacon/agentloop process health checks
RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates git bash grep tmux curl libicu72 gosu procps \
  && npm install --global "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
  && case "${TARGETARCH}" in \
       amd64|arm64) dolt_arch="${TARGETARCH}" ;; \
       *) echo "unsupported Dolt architecture: ${TARGETARCH}" >&2; exit 1 ;; \
     esac \
  && if [ "${DOLT_VERSION}" = "latest" ]; then \
       dolt_url="https://github.com/dolthub/dolt/releases/latest/download/dolt-linux-${dolt_arch}.tar.gz"; \
     else \
       dolt_url="https://github.com/dolthub/dolt/releases/download/v${DOLT_VERSION}/dolt-linux-${dolt_arch}.tar.gz"; \
     fi \
  && curl -fsSL "${dolt_url}" -o /tmp/dolt.tar.gz \
  && tar -xzf /tmp/dolt.tar.gz -C /tmp \
  && install -m 0755 "/tmp/dolt-linux-${dolt_arch}/bin/dolt" /usr/local/bin/dolt \
  && rm -rf /tmp/dolt.tar.gz "/tmp/dolt-linux-${dolt_arch}" /root/.npm /var/lib/apt/lists/*

RUN useradd -m -u 10001 -s /bin/bash gastown

COPY --from=builder /out/gt /usr/local/bin/gt
COPY --from=builder /out/bd /usr/local/bin/bd

# Pre-create the default workspace root
RUN mkdir -p /gt && chown gastown:gastown /gt

COPY --chmod=755 scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

WORKDIR /gt

ENV HOME=/home/gastown

# Start as root only so the entrypoint can fix bind-mount permissions. It
# drops to the gastown user before starting gt.
ENTRYPOINT ["docker-entrypoint.sh"]
