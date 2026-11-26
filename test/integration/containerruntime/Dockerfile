# Container runtime integration test runner.

FROM golang:1.27.4

RUN apt-get update \
    && apt-get install -y --no-install-recommends busybox-static \
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
    -tags 'integration containers_image_openpgp exclude_graphdriver_btrfs' \
    -o /usr/local/bin/containerruntime.test ./test/integration/containerruntime

ENV CONTAINERRUNTIME_BUSYBOX=/bin/busybox

CMD ["/usr/local/bin/containerruntime.test", "-test.v", "-test.count=1", "-test.timeout=10m"]
