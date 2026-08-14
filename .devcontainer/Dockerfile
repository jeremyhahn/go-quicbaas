# QuicBaaS Development Container
# Base image for all development, testing, and integration work
# All integration test Dockerfiles should inherit from this image
#
# Hardware crypto emulators (swtpm TPM 2.0 simulator + SoftHSM2 PKCS#11) are
# built from source in dedicated alpine:3.22.1 builder stages — reusing the
# EXACT recipes from test/integration/docker/{swtpm,softhsm}/Dockerfile — and
# their artifacts are copied into the final golang:1.26-alpine image so that
# `go test -tags 'integration tpm2 pkcs11'` can run IN the devcontainer against
# real swtpm + SoftHSM2 (parity with the go-xkms devcontainer). The SoftHSM2
# library is ABI-compatible with the Alpine pkcs11 CGO binary because both are
# built on Alpine (same approach proven by test/integration/Dockerfile.seal-hardware).

# ============================================================================
# Stage: swtpm-builder — libtpms + swtpm from source (matches docker/swtpm)
# ============================================================================
FROM alpine:3.22.1 AS swtpm-builder

RUN apk add --no-cache \
    git \
    build-base \
    automake \
    autoconf \
    libtool \
    bash \
    gawk \
    socat \
    libseccomp-dev \
    gnutls-dev \
    libtasn1-dev \
    expect \
    json-glib-dev \
    glib-dev \
    openssl-dev

WORKDIR /build
RUN git clone https://github.com/stefanberger/libtpms.git && \
    cd libtpms && \
    git checkout b4d81572c15b504a4e60b4b46c91d3ec0a92c79e && \
    ./autogen.sh --prefix=/usr --with-openssl --with-tpm2 && \
    make -j$(nproc) && \
    make install DESTDIR=/build/libtpms-install

WORKDIR /build
RUN git clone https://github.com/stefanberger/swtpm.git && \
    cd swtpm && \
    git checkout 665486b8179fef6eba845e8437acd2da6ae2634e && \
    cp -r /build/libtpms-install/usr/* /usr/ && \
    ./autogen.sh --prefix=/usr --with-openssl --with-tpm2 && \
    make -j$(nproc) && \
    make install DESTDIR=/build/swtpm-install

# ============================================================================
# Stage: softhsm-builder — SoftHSM2 v2.6.1 from source (matches docker/softhsm)
# ============================================================================
FROM alpine:3.22.1 AS softhsm-builder

RUN apk add --no-cache \
    git \
    build-base \
    automake \
    autoconf \
    libtool \
    openssl-dev \
    sqlite-dev \
    p11-kit-dev

WORKDIR /build
RUN git clone https://github.com/opendnssec/SoftHSMv2.git && \
    cd SoftHSMv2 && \
    git checkout 2.6.1 && \
    sh autogen.sh && \
    ./configure \
        --prefix=/usr/local \
        --disable-gost \
        --with-crypto-backend=openssl \
        --with-objectstore-backend-db \
        --with-sqlite3=/usr && \
    make -j$(nproc) && \
    make install DESTDIR=/build/softhsm-install

# ============================================================================
# Final stage: the development container
# ============================================================================
FROM golang:1.26.5-alpine

LABEL maintainer="QuicBaaS Team"
LABEL description="QuicBaaS development container with all dependencies"
LABEL version="1.0.0"

# Build arguments for customization
ARG USERNAME=quicbaas
ARG USER_UID=1000
ARG USER_GID=1000

# Install all development and testing dependencies
RUN apk add --no-cache \
    # Build essentials
    git \
    make \
    bash \
    coreutils \
    # CGO toolchain (required by the pkcs11 build tag — miekg/pkcs11 C binding)
    gcc \
    musl-dev \
    # Editors and utilities
    vim \
    less \
    # Networking and testing tools
    curl \
    wget \
    jq \
    netcat-openbsd \
    bind-tools \
    iputils \
    # Security and certificates
    ca-certificates \
    tzdata \
    openssl \
    # Hardware crypto emulator runtime deps (swtpm)
    gnutls \
    libtasn1 \
    json-glib \
    glib \
    libseccomp \
    socat \
    su-exec \
    # Hardware crypto emulator runtime deps (SoftHSM2) + PKCS#11 tooling
    sqlite-libs \
    p11-kit \
    p11-kit-server \
    opensc \
    # Protobuf
    protobuf \
    protobuf-dev \
    # Docker support for integration tests
    docker-cli \
    docker-cli-compose \
    # Shell utilities
    shadow \
    sudo \
    # Process management
    procps \
    htop

# Install the swtpm TPM 2.0 simulator (libtpms + swtpm) built from source.
COPY --from=swtpm-builder /build/libtpms-install/usr /usr
COPY --from=swtpm-builder /build/swtpm-install/usr /usr

# Install SoftHSM2 (library + softhsm2-util) built from source. Library lands
# at /usr/local/lib/softhsm/libsofthsm2.so, utility at /usr/local/bin/softhsm2-util.
COPY --from=softhsm-builder /build/softhsm-install/usr/local /usr/local
RUN ldconfig /usr/local/lib 2>/dev/null || true && \
    swtpm --version && \
    softhsm2-util --version

# Install Ollama for embedding tests (best-effort).
# Alpine requires the glibc compatibility layer for Ollama. The Ollama release
# layout moved from a bare binary to a .tgz on GitHub releases; this step is
# wrapped best-effort so a download/layout change can NEVER block the image
# build — the embedding/RAG tests are optional (rag build tag) and excluded
# from the default test run.
RUN apk add --no-cache libc6-compat && \
    ( wget -qO /tmp/ollama.tgz https://github.com/ollama/ollama/releases/latest/download/ollama-linux-amd64.tgz \
        && tar -xzf /tmp/ollama.tgz -C /usr/local \
        && rm -f /tmp/ollama.tgz \
        && /usr/local/bin/ollama --version >/dev/null 2>&1 \
        && echo "ollama installed" ) \
    || echo "WARN: ollama download failed; embedding/RAG tests unavailable in this image"

# Install Go development tools
# Note: Some tools may need specific versions for Go 1.26 compatibility
RUN go install github.com/air-verse/air@latest && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    go install github.com/securego/gosec/v2/cmd/gosec@latest && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest && \
    go install golang.org/x/tools/gopls@latest && \
    go install github.com/fatih/gomodifytags@latest && \
    go install github.com/josharian/impl@latest && \
    go install github.com/cweill/gotests/gotests@latest && \
    go install honnef.co/go/tools/cmd/staticcheck@latest && \
    go install gotest.tools/gotestsum@latest

# Create non-root user with sudo access
RUN addgroup -g ${USER_GID} ${USERNAME} && \
    adduser -D -u ${USER_UID} -G ${USERNAME} -s /bin/bash ${USERNAME} && \
    echo "${USERNAME} ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/${USERNAME} && \
    chmod 0440 /etc/sudoers.d/${USERNAME}

# SoftHSM2 config + token directory and swtpm state directory. The SoftHSM2
# token is initialised at container start (post-start hook), not at build time,
# so the token store lives on the writable container layer / volume.
RUN mkdir -p /etc/softhsm /var/lib/softhsm/tokens /var/lib/swtpm/tpmstate && \
    printf 'directories.tokendir = /var/lib/softhsm/tokens\nobjectstore.backend = file\nlog.level = INFO\nslots.removable = false\n' > /etc/softhsm/softhsm2.conf && \
    chown -R ${USERNAME}:${USERNAME} /etc/softhsm /var/lib/softhsm /var/lib/swtpm

# Create standard directories
RUN mkdir -p \
    /home/${USERNAME}/.cache/go-build \
    /home/${USERNAME}/go/pkg/mod \
    /data \
    /var/log/quicbaas \
    /var/run/quicbaas && \
    chown -R ${USERNAME}:${USERNAME} \
    /home/${USERNAME} \
    /data \
    /var/log/quicbaas \
    /var/run/quicbaas

# Set working directory
WORKDIR /workspace

# Copy go.mod and go.sum for dependency caching (if available)
# These are copied at build time for caching; will be overwritten by mount
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download 2>/dev/null || true

# Environment variables
ENV GOPATH=/home/${USERNAME}/go \
    GOCACHE=/home/${USERNAME}/.cache/go-build \
    PATH="/home/${USERNAME}/go/bin:/go/bin:${PATH}" \
    CGO_ENABLED=0 \
    GO111MODULE=on \
    QUICBAAS_LOG_LEVEL=debug \
    QUICBAAS_LOG_FORMAT=console \
    QUICBAAS_DATA_DIR=/data \
    QUICBAAS_TEST_MODE=development \
    # Hardware crypto emulator wiring (swtpm runs in-container on localhost:2321;
    # SoftHSM2 token store + library for the pkcs11 build tag).
    SOFTHSM2_CONF=/etc/softhsm/softhsm2.conf \
    SOFTHSM2_LIB=/usr/local/lib/softhsm/libsofthsm2.so \
    PKCS11_LIBRARY=/usr/local/lib/softhsm/libsofthsm2.so \
    TPM_USE_SIMULATOR=true \
    TPM_SIMULATOR_HOST=localhost \
    TPM_SIMULATOR_PORT=2321 \
    SWTPM_HOST=localhost

# Switch to non-root user
USER ${USERNAME}

# Expose common ports
# 8080: HTTP/REST API
# 9090: Metrics/Prometheus
# 50051: gRPC API
# 4433: QUIC
# 63001-63010: Raft cluster communication
# 2345: Delve debugger
EXPOSE 8080 9090 50051 4433 63001-63010 2345

# Default to bash shell
CMD ["/bin/bash"]
