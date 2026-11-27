# OpenVINO Model Server with the Intel NPU user-mode driver.
ARG OVMS_TAG=latest
FROM openvino/model_server:${OVMS_TAG}

# Matches the host's intel_vpu build; see the note above before bumping.
ARG NPU_DRIVER_TAG=1.38.0
ARG NPU_DRIVER_VERSION=1.38.0.20260910-34487311128
ARG LEVEL_ZERO_VERSION=1.24.2

USER root

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl libtbb12; \
    tmp="$(mktemp -d)"; cd "$tmp"; \
    # The release ships one tarball of .deb files.
    curl -fsSLO "https://github.com/intel/linux-npu-driver/releases/download/v${NPU_DRIVER_TAG}/linux-npu-driver-v${NPU_DRIVER_VERSION}-ubuntu2404.tar.gz"; \
    tar xzf "linux-npu-driver-v${NPU_DRIVER_VERSION}-ubuntu2404.tar.gz"; \
    curl -fsSLO "https://github.com/oneapi-src/level-zero/releases/download/v${LEVEL_ZERO_VERSION}/level-zero_${LEVEL_ZERO_VERSION}+u24.04_amd64.deb"; \
    # dbgsym packages are debug symbols only.
    rm -f ./*dbgsym*; \
    dpkg -i ./intel-fw-npu_*.deb ./intel-level-zero-npu_*.deb ./intel-driver-compiler-npu_*.deb ./level-zero_*.deb \
      || apt-get install -y -f --no-install-recommends; \
    cd /; rm -rf "$tmp"; \
    apt-get clean; rm -rf /var/lib/apt/lists/*; \
    ldconfig; \
    # Without this symlink NPU is missing from available_devices, so check it at build time.
    test -e /usr/lib/x86_64-linux-gnu/libze_loader.so.1; \
    test -e /usr/lib/x86_64-linux-gnu/libze_intel_npu.so.1

USER ovms
