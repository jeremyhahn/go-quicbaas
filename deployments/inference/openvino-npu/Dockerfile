# OpenVINO Model Server with the Intel NPU user-mode driver.
#
# WHY A CONTAINER RATHER THAN A HOST INSTALL. Everything else the Software
# Factory runs — the worker, the reviewer — ships as an image, and the one
# host-installed OpenVINO on the GPU box had already drifted: its venv carried
# openvino 2026.3 where `openvino.runtime` no longer exists, so code written
# against the documented import failed on a machine that "had OpenVINO". An
# image pins what it needs and says so.
#
# WHY THE STOCK IMAGE IS NOT ENOUGH. openvino/model_server ships
# libopenvino_intel_npu_plugin.so but NO level-zero: neither libze_loader nor
# libze_intel_npu is present, so the plugin cannot reach /dev/accel/accel0 and
# the NPU never appears in available_devices. Verified on the image directly
# before this file existed. The driver packages below are what close that gap.
#
# THE VERSIONS ARE PINNED TO THE HOST'S KERNEL DRIVER. intel_vpu on the GPU box
# reports firmware vpu_50xx and the host runs intel-level-zero-npu 1.38.0; a
# user-mode driver that disagrees with the kernel module fails at device open,
# not at load, which surfaces as an empty device list rather than an error.
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
    # The release ships ONE tarball of .deb files, not loose debs — guessing
    # per-package URLs 404s.
    curl -fsSLO "https://github.com/intel/linux-npu-driver/releases/download/v${NPU_DRIVER_TAG}/linux-npu-driver-v${NPU_DRIVER_VERSION}-ubuntu2404.tar.gz"; \
    tar xzf "linux-npu-driver-v${NPU_DRIVER_VERSION}-ubuntu2404.tar.gz"; \
    curl -fsSLO "https://github.com/oneapi-src/level-zero/releases/download/v${LEVEL_ZERO_VERSION}/level-zero_${LEVEL_ZERO_VERSION}+u24.04_amd64.deb"; \
    # dbgsym packages are debug symbols only; installing them costs hundreds of
    # megabytes in an image nothing debugs.
    rm -f ./*dbgsym*; \
    dpkg -i ./intel-fw-npu_*.deb ./intel-level-zero-npu_*.deb ./intel-driver-compiler-npu_*.deb ./level-zero_*.deb \
      || apt-get install -y -f --no-install-recommends; \
    cd /; rm -rf "$tmp"; \
    apt-get clean; rm -rf /var/lib/apt/lists/*; \
    ldconfig; \
    # A missing symlink here is the difference between "NPU" appearing in
    # available_devices and not, so it is checked at BUILD time rather than
    # discovered when a model fails to compile.
    test -e /usr/lib/x86_64-linux-gnu/libze_loader.so.1; \
    test -e /usr/lib/x86_64-linux-gnu/libze_intel_npu.so.1

USER ovms
