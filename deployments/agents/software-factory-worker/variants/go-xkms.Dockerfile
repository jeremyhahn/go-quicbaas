# go-xkms lane image: the published factory worker plus the system packages
# go-xkms's CI installs (.github/scripts/apt-deps.sh callers), so lanes and
# lane-verify compile the cgo TPM simulator, PKCS#11, USB and desktop code.
# A project that needs more than the default worker image builds FROM it.
#
# Built FROM the ghcr base image, so it takes the base's labels, the
# io.quicbaas.worker.languages label the factory reads among them. The
# worker-image-variants workflow builds it against the base image's digest
# and publishes it to ghcr.io beside the base:
#
#   ghcr.io/jeremyhahn/quicbaas-software-factory-worker-go-xkms:base-<base digest, 12 hex>
#   (and the same image as :current)
#
# go-xkms's factory env then names it:
#   FACTORY_IMAGE=ghcr.io/jeremyhahn/quicbaas-software-factory-worker-go-xkms:current
ARG BASE=ghcr.io/jeremyhahn/quicbaas-software-factory-worker:current
FROM ${BASE}
USER root
RUN apt-get -o Acquire::ForceIPv4=true update \
 && DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true install -y --no-install-recommends \
      build-essential autoconf automake libtool pkg-config \
      libssl-dev libtss2-dev libusb-1.0-0-dev libicu-dev libpcsclite-dev \
      libgtk-3-dev libwebkit2gtk-4.1-dev \
      swtpm swtpm-tools softhsm2 opensc jq ca-certificates curl \
 && rm -rf /var/lib/apt/lists/* \
 && { usermod -aG softhsm agent 2>/dev/null || true; }
USER 1000:1000
LABEL io.quicbaas.worker.project="go-xkms"
