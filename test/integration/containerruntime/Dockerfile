# ============================================================================
# Container runtime integration test runner
# ============================================================================
#
# Builds the pkg/containerruntime integration test binary with cgo, so that
# libcontainer's nsenter init is linked in, and runs it. libcontainer starts
# each container by re-executing the current binary, so the test binary is
# also the container init.
#
# busybox-static supplies the one file of the test image the tests push to
# the compose file's registry.
#
# Used by test/integration/containerruntime/docker-compose.yml; run it with
# `make integration-test-containerruntime`. The container must be privileged.

FROM golang:1.27.2

RUN apt-get update \
    && apt-get install -y --no-install-recommends busybox-static \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace/go-quicbaas

COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/

# Gitea credentials arrive as a BuildKit secret, sourced for this RUN alone
# (dependency-resolution.md §6), the same way test/integration/Dockerfile
# takes them.
RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then . /run/secrets/gomodauth; fi; \
    go mod download

COPY . .

RUN CGO_ENABLED=1 go test -c -race -buildvcs=false \
    -tags 'integration containers_image_openpgp exclude_graphdriver_btrfs' \
    -o /usr/local/bin/containerruntime.test ./test/integration/containerruntime

ENV CONTAINERRUNTIME_BUSYBOX=/bin/busybox

CMD ["/usr/local/bin/containerruntime.test", "-test.v", "-test.count=1", "-test.timeout=10m"]
