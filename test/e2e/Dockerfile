# QuicBaaS dockerized Playwright e2e runner.
#
# Single self-contained image so the host needs only Docker — no Node, no
# Playwright browsers, no daemon toolchain. Three stages:
#
#   ui-builder   (node)     — builds the TypeScript SDK + Svelte UI dist.
#   daemon-builder (golang) — resolves first-party modules from Gitea by tag
#                             test/integration/Dockerfile), embeds the fresh UI
#                             dist, and builds quicbaasd.
#   runner (playwright)     — Playwright image (browsers preinstalled) carrying
#                             the daemon binary + the e2e specs. The entrypoint
#                             boots the daemon and runs the suite in one place,
#                             so the CA cert and first-boot invite log are shared
#                             without cross-container plumbing.
#
# The workspace named context (sibling repos) is supplied by
# docker-compose.e2e.yml via additional_contexts, exactly like the integration
# compose files.

ARG BUILD_TAGS="local quic raft rest rag timeseries vector qrdb_vector iam mcp hashicorpmetrics codec_json codec_yaml codec_toml codec_msgpack codec_bson codec_cbor codec_protobuf gms_pure_go awss3 gcpstorage azureblob minio"
ARG PLAYWRIGHT_VERSION=v1.49.0-jammy

# ============================================================================
# Stage 1: Build the TypeScript SDK + Svelte UI dist.
# ============================================================================
FROM node:22-bookworm-slim AS ui-builder

WORKDIR /src

# The frontend resolves @quicbaas/sdk + @quicbaas/svelte-security via file:
# paths into sdk/typescript, so both must be present with the repo layout.
COPY sdk/typescript ./sdk/typescript
COPY cmd/quicbaas/frontend ./cmd/quicbaas/frontend

# Build the SDK (tsc) so its dist/ exists for the frontend's file: dependency.
WORKDIR /src/sdk/typescript
RUN npm install --no-audit --no-fund && node ./node_modules/typescript/bin/tsc

# Build the frontend (vite). npm install resolves the file: SDK deps against the
# sibling sdk/typescript tree; we then pin them to absolute symlinks so the
# build sees the freshly-compiled SDK dist (mirrors the Makefile build-frontend).
WORKDIR /src/cmd/quicbaas/frontend
RUN npm install --no-audit --no-fund \
    && rm -rf node_modules/@quicbaas/sdk node_modules/@quicbaas/svelte-security \
    && ln -sfn /src/sdk/typescript node_modules/@quicbaas/sdk \
    && ln -sfn /src/sdk/typescript/svelte-security node_modules/@quicbaas/svelte-security \
    && rm -rf node_modules/.vite dist \
    && npm run build \
    && test -f dist/index.html

# ============================================================================
# Stage 2: Build quicbaasd with the fresh UI embedded.
# ============================================================================
FROM golang:1.27.3-alpine AS daemon-builder

ARG BUILD_TAGS
RUN apk add --no-cache git make bash ca-certificates

WORKDIR /workspace/go-quicbaas

COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Gitea credentials arrive as a BuildKit secret, mounted on tmpfs for this RUN
# alone and sourced rather than exported, so the token reaches neither a layer,
# image metadata, nor a command line (dependency-resolution.md §6). Optional by
# design, so the same Dockerfile builds against the public proxy.
RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then set -a; . /run/secrets/gomodauth; set +a; fi; \
    go mod download

COPY . .

# The built UI is baked into the daemon's embedded asset directories.
COPY --from=ui-builder /src/cmd/quicbaas/frontend/dist ./pkg/api/rest/ui/dist
COPY --from=ui-builder /src/cmd/quicbaas/frontend/dist ./cmd/quicbaas/frontend/dist

RUN rm -f pkg/api/rest/debug_integration.go pkg/api/rest/debug_noop.go || true
RUN CGO_ENABLED=0 GOOS=linux \
    go build -tags "${BUILD_TAGS}" -buildvcs=false -ldflags "-w -s" -o bin/quicbaasd ./cmd/quicbaasd

# ============================================================================
# Stage 3: Playwright runner (browsers preinstalled) + daemon + e2e specs.
# ============================================================================
FROM mcr.microsoft.com/playwright:v1.49.0-jammy AS runner

# The daemon (static CGO_ENABLED=0 binary) runs alongside Playwright.
COPY --from=daemon-builder /workspace/go-quicbaas/bin/quicbaasd /usr/local/bin/quicbaasd
RUN chmod +x /usr/local/bin/quicbaasd

WORKDIR /app

# Bring the SDK trees so the frontend's file: deps resolve, then the e2e harness.
# The SDK comes from the ui-builder stage (already compiled, so the frontend's
# file: dependency does not trigger a rebuild at install time); the frontend
# source carries the e2e specs + playwright config.
COPY --from=ui-builder /src/sdk/typescript /app/sdk/typescript
COPY cmd/quicbaas/frontend /app/cmd/quicbaas/frontend

WORKDIR /app/cmd/quicbaas/frontend
# Install the e2e deps (Playwright test runner), then fetch the chromium build
# that the resolved @playwright/test version expects. The base image ships
# browsers for its own Playwright release, but npm may resolve a newer
# @playwright/test (^1.49.0); `playwright install chromium` reconciles the two
# into PLAYWRIGHT_BROWSERS_PATH. System deps are already present in the base.
RUN npm install --no-audit --no-fund \
    && npx playwright install chromium

# Optionally install opencode (the open-source ACP agent) for the live ACP
# suite. Gated by a build arg so the default no-backend image stays lean; the
# ACP compose override sets WITH_OPENCODE=1. The ollama provider config is
# written at runtime by run-e2e.sh from E2E_OLLAMA_URL.
ARG WITH_OPENCODE=0
RUN if [ "$WITH_OPENCODE" = "1" ]; then \
      npm install -g opencode-ai && opencode --version; \
    fi

# Install the docker CLI for the container-sandbox spawn path (QUICBAAS_E2E_SANDBOX=1).
# The daemon shells out to `docker` against the mounted host socket to launch
# sibling agent containers; the raw-subprocess path does not need it. Gated by
# WITH_OPENCODE so the default no-backend image stays lean. Only the client is
# installed — dockerd runs on the host, reached via /var/run/docker.sock.
RUN if [ "$WITH_OPENCODE" = "1" ]; then \
      apt-get update \
      && apt-get install -y --no-install-recommends docker.io \
      && rm -rf /var/lib/apt/lists/* \
      && docker --version; \
    fi

# Entrypoint boots the daemon and runs the requested Playwright project.
COPY test/e2e/run-e2e.sh /usr/local/bin/run-e2e.sh
RUN chmod +x /usr/local/bin/run-e2e.sh

# Defaults (overridable via compose/Make):
#   E2E_PROJECT   — Playwright project (chromium-no-backend | chromium-acp)
#   E2E_BACKEND   — daemon embedded-fleet-node backend (ollama | opencode)
#   E2E_OLLAMA_URL— ollama endpoint for the backend
ENV E2E_PROJECT=chromium-no-backend \
    E2E_BACKEND=ollama \
    E2E_OLLAMA_URL=http://ollama:11434

ENTRYPOINT ["/usr/local/bin/run-e2e.sh"]
