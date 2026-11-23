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
# gh: the pinned release and its sha256s; change them together.
ARG GH_VERSION=v2.103.3
ARG GH_SHA256_AMD64=4c806978ffce1cec70853302a56d70ac6da4f19bae7adca4e1a8ab00633dd91d
ARG GH_SHA256_ARM64=ee32df2323c8847f5fac6cb6b4b69ca75828e9329a8f22f677021f7b3306ce63
RUN set -eu; \
    a="$(dpkg --print-architecture)"; \
    case "$a" in \
      amd64) sha="${GH_SHA256_AMD64}" ;; \
      arm64) sha="${GH_SHA256_ARM64}" ;; \
      *) echo "unsupported architecture" >&2; exit 1 ;; \
    esac; \
    dl="$(mktemp -d)"; \
    curl -4 -fsSL -o "$dl/gh.tar.gz" \
      "https://github.com/cli/cli/releases/download/${GH_VERSION}/gh_${GH_VERSION#v}_linux_${a}.tar.gz"; \
    echo "${sha}  $dl/gh.tar.gz" | sha256sum -c -; \
    tar -xzf "$dl/gh.tar.gz" -C "$dl"; \
    install -m 0755 "$dl/gh_${GH_VERSION#v}_linux_${a}/bin/gh" /usr/local/bin/gh; \
    rm -rf "$dl"; \
    case "$(gh --version)" in *"gh version ${GH_VERSION#v} "*) ;; *) echo "gh does not match ${GH_VERSION}" >&2; exit 1 ;; esac
USER 1000:1000
LABEL io.quicbaas.worker.project="go-xkms"
