# =============================================================================
# Gas Town (gt) — Nostr-Native Multi-Agent Orchestrator
# =============================================================================
# Multi-stage build producing a minimal runtime image with:
#   - gt binary (CGO enabled)
#   - bd (beads) binary
#   - git, tmux, ssh, curl
#   - Nostr spool directory + config mount points
#
# Build:
#   docker build -t gastown:latest .
#   docker build --build-arg GT_VERSION=v0.3.0 -t gastown:v0.3.0 .
#
# Run:
#   docker run -d --name gastown \
#     -v $HOME/gt:/home/gt/gt \
#     -e GT_NOSTR_ENABLED=1 \
#     gastown:latest
# =============================================================================

# ---------------------------------------------------------------------------
# Stage 1: Build gt binary
# ---------------------------------------------------------------------------
FROM golang:1.25-bookworm AS builder

ARG GT_VERSION=dev
ARG GT_COMMIT=unknown
ARG GT_BUILD_TIME=unknown

# CGO is required for some dependencies
ENV CGO_ENABLED=1

WORKDIR /build

# Cache Go module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .

RUN go build \
    -ldflags "-s -w \
      -X github.com/steveyegge/gastown/internal/cmd.Version=${GT_VERSION} \
      -X github.com/steveyegge/gastown/internal/cmd.Commit=${GT_COMMIT} \
      -X github.com/steveyegge/gastown/internal/cmd.BuildTime=${GT_BUILD_TIME} \
      -X github.com/steveyegge/gastown/internal/cmd.BuiltProperly=1" \
    -o /build/gt ./cmd/gt

# ---------------------------------------------------------------------------
# Stage 2: Build beads (bd) binary
# ---------------------------------------------------------------------------
FROM golang:1.25-bookworm AS beads-builder

ENV CGO_ENABLED=1

WORKDIR /build

# Install beads from the latest release
RUN go install github.com/steveyegge/beads/cmd/bd@latest && \
    cp $(go env GOPATH)/bin/bd /build/bd

# ---------------------------------------------------------------------------
# Stage 3: Runtime image
# ---------------------------------------------------------------------------
FROM debian:bookworm-slim AS runtime

# Labels
LABEL org.opencontainers.image.title="Gas Town"
LABEL org.opencontainers.image.description="Nostr-native multi-agent orchestrator"
LABEL org.opencontainers.image.source="https://github.com/steveyegge/gastown"
LABEL org.opencontainers.image.licenses="MIT"

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    tmux \
    openssh-client \
    ca-certificates \
    curl \
    jq \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for gastown
RUN groupadd -r gt && useradd -r -g gt -m -d /home/gt -s /bin/bash gt

# Copy binaries from build stages
COPY --from=builder /build/gt /usr/local/bin/gt
COPY --from=beads-builder /build/bd /usr/local/bin/bd

# Copy example configs for reference
COPY docs/examples/ /usr/share/gastown/examples/
COPY templates/ /usr/share/gastown/templates/

# Create standard directories
RUN mkdir -p \
    /home/gt/gt \
    /home/gt/.config/gt \
    /home/gt/.local/share/gt/spool \
    /home/gt/.ssh \
    && chown -R gt:gt /home/gt

# Configure git for container use
RUN git config --system init.defaultBranch main && \
    git config --system safe.directory '*'

# Switch to non-root user
USER gt
WORKDIR /home/gt

# Configure git identity (can be overridden via env)
RUN git config --global user.name "Gas Town" && \
    git config --global user.email "gt@gastown.local"

# Environment defaults
ENV GT_HOME=/home/gt/gt
ENV GT_NOSTR_ENABLED=0
ENV GT_NOSTR_CONFIG=/home/gt/gt/.nostr.json
ENV GT_SPOOL_DIR=/home/gt/.local/share/gt/spool
ENV PATH="/usr/local/bin:${PATH}"

# Health check — uses gt doctor when available, falls back to process check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD gt doctor --quiet 2>/dev/null || pgrep -x gt > /dev/null || exit 1

# Expose MCP server port (when running in MCP provider mode)
EXPOSE 9500

# Default entrypoint — starts the gt daemon
# Override with: docker run gastown gt <any-command>
ENTRYPOINT ["gt"]
CMD ["daemon", "start", "--foreground"]
