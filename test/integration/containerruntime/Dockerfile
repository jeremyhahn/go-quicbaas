# Container runtime integration test runner.
# libseccomp-dev and the seccomp tag give runc the seccomp support unprivileged containers need.

FROM golang:1.27.4

RUN apt-get update \
    && apt-get install -y --no-install-recommends busybox-static libseccomp-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace/go-quicbaas

COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Module credentials arrive as a BuildKit secret, sourced for this run only, as in test/integration/Dockerfile.
RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then set -a; . /run/secrets/gomodauth; set +a; fi; \
    go mod download

COPY . .

RUN CGO_ENABLED=1 go test -c -race -buildvcs=false \
    -tags 'integration containers_image_openpgp exclude_graphdriver_btrfs seccomp' \
    -o /usr/local/bin/containerruntime.test ./test/integration/containerruntime
# The internal/containerruntime layer-store tests need root, so they run here too.
RUN CGO_ENABLED=1 go test -c -race -buildvcs=false \
    -tags 'integration containers_image_openpgp exclude_graphdriver_btrfs seccomp' \
    -o /usr/local/bin/containerruntime-internal.test ./internal/containerruntime

ENV CONTAINERRUNTIME_BUSYBOX=/bin/busybox

CMD ["sh", "-c", "set -e; /usr/local/bin/containerruntime.test -test.v -test.count=1 -test.timeout=10m; /usr/local/bin/containerruntime-internal.test -test.v -test.count=1 -test.timeout=10m -test.run '^(TestStorageLayerCommitter|TestBuildImage)_'"]
