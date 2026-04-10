# QuicBaaS Development Container
# Base image for all development, testing, and integration work
# All integration test Dockerfiles should inherit from this image

FROM golang:1.26-alpine

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

# Install Ollama for embedding tests
# Alpine requires glibc compatibility layer for Ollama
RUN apk add --no-cache libc6-compat && \
    wget -qO /usr/local/bin/ollama https://ollama.com/download/ollama-linux-amd64 && \
    chmod +x /usr/local/bin/ollama

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
    QUICBAAS_TEST_MODE=development

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
