# QuicBaaS dockerized Playwright e2e runner.

ARG BUILD_TAGS="local quic raft rest rag timeseries vector qrdb_vector iam mcp hashicorpmetrics codec_json codec_yaml codec_toml codec_msgpack codec_bson codec_cbor codec_protobuf gms_pure_go awss3 gcpstorage azureblob minio"
ARG PLAYWRIGHT_VERSION=v1.49.0-jammy

# Stage 1: build the TypeScript SDK and the Svelte UI.
FROM node:22-bookworm-slim AS ui-builder

WORKDIR /src

# The frontend resolves its SDK packages through file: paths into sdk/typescript.
COPY sdk/typescript ./sdk/typescript
COPY cmd/quicbaas/frontend ./cmd/quicbaas/frontend

# Build the SDK (tsc) so its dist/ exists for the frontend's file: dependency.
WORKDIR /src/sdk/typescript
RUN npm install --no-audit --no-fund && node ./node_modules/typescript/bin/tsc

# Build the frontend against the freshly compiled SDK, as the Makefile's build-frontend does.
WORKDIR /src/cmd/quicbaas/frontend
RUN npm install --no-audit --no-fund \
    && rm -rf node_modules/@quicbaas/sdk node_modules/@quicbaas/svelte-security \
    && ln -sfn /src/sdk/typescript node_modules/@quicbaas/sdk \
    && ln -sfn /src/sdk/typescript/svelte-security node_modules/@quicbaas/svelte-security \
    && rm -rf node_modules/.vite dist \
    && npm run build \
    && test -f dist/index.html

# Stage 2: build quicbaasd with the UI embedded.
FROM golang:1.27.4-alpine AS daemon-builder

ARG BUILD_TAGS
RUN apk add --no-cache git make bash ca-certificates

WORKDIR /workspace/go-quicbaas

COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Module credentials arrive as a BuildKit secret on tmpfs, sourced for this run only, so the token reaches no layer or command line.
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

# Stage 3: Playwright runner, daemon and e2e specs.
FROM mcr.microsoft.com/playwright:v1.49.0-jammy AS runner

# The daemon (static CGO_ENABLED=0 binary) runs alongside Playwright.
COPY --from=daemon-builder /workspace/go-quicbaas/bin/quicbaasd /usr/local/bin/quicbaasd
RUN chmod +x /usr/local/bin/quicbaasd

WORKDIR /app

# The SDK trees for the frontend's file: dependencies, then the e2e harness.
COPY --from=ui-builder /src/sdk/typescript /app/sdk/typescript
COPY cmd/quicbaas/frontend /app/cmd/quicbaas/frontend

WORKDIR /app/cmd/quicbaas/frontend
# The e2e dependencies, then the chromium build @playwright/test expects.
RUN npm install --no-audit --no-fund \
    && npx playwright install chromium

# Optionally opencode for the live ACP suite.
ARG WITH_OPENCODE=0
RUN if [ "$WITH_OPENCODE" = "1" ]; then \
      npm install -g opencode-ai && opencode --version; \
    fi

# The docker CLI for the container-sandbox spawn path (QUICBAAS_E2E_SANDBOX=1).
RUN if [ "$WITH_OPENCODE" = "1" ]; then \
      apt-get update \
      && apt-get install -y --no-install-recommends docker.io \
      && rm -rf /var/lib/apt/lists/* \
      && docker --version; \
    fi

# Entrypoint boots the daemon and runs the requested Playwright project.
COPY test/e2e/run-e2e.sh /usr/local/bin/run-e2e.sh
RUN chmod +x /usr/local/bin/run-e2e.sh

# Defaults, overridable through compose or make.
ENV E2E_PROJECT=chromium-no-backend \
    E2E_BACKEND=ollama \
    E2E_OLLAMA_URL=http://ollama:11434

ENTRYPOINT ["/usr/local/bin/run-e2e.sh"]
