# trusted-firmware lane image: the published factory worker plus cppcheck and
# trivy, which trusted-firmware's `lint` stage requires (mk/stages.mk
# require-tools) and the bench lanes run, and the host tools the OP-TEE QEMU
# stack build needs: `file` at /usr/bin/file for Buildroot's host check,
# libgnutls28-dev for U-Boot's tools, python3-pip and python3-setuptools for
# QEMU's configure, python3-venv for the ensurepip QEMU's configure runs under
# `make optee-qemu`, with bc, bison, cpio, device-tree-compiler, flex,
# libslirp-dev, ninja-build, python3-cryptography, python3-pyelftools and wget
# for the rest of that build.
# trivy is the CI image's trivy pin, checked against its release sha256;
# change both together.
# A project that needs more than the default worker image builds FROM it.
#
# Built FROM the ghcr base image, so it takes the base's labels, the
# io.quicbaas.worker.languages label the factory reads among them. The
# worker-image-variants workflow builds it against the base image's digest
# and publishes it to ghcr.io beside the base:
#
#   ghcr.io/jeremyhahn/quicbaas-software-factory-worker-trusted-firmware:base-<base digest, 12 hex>
#   (and the same image as :current)
#
# A trusted-firmware lane then names it, in the factory env or on the launch:
#   FACTORY_IMAGE=ghcr.io/jeremyhahn/quicbaas-software-factory-worker-trusted-firmware:current
ARG BASE=ghcr.io/jeremyhahn/quicbaas-software-factory-worker:current
FROM ${BASE}
USER root
ARG TRIVY_VERSION=v0.75.0
ARG TRIVY_SHA256_AMD64=c6e65abddb348e25f10549df887045629cf28cc72453cd1c63acb717316b3f3f
ARG TRIVY_SHA256_ARM64=a1ee9f6ffb7d112b64ff726a2a0717c21175c1114361391f4a132956751a13b3
RUN apt-get -o Acquire::ForceIPv4=true update \
 && DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true install -y --no-install-recommends cppcheck ca-certificates curl \
      file libgnutls28-dev python3-pip python3-setuptools \
      bc bison cpio device-tree-compiler flex libslirp-dev ninja-build \
      python3-cryptography python3-pyelftools python3-venv wget \
 && rm -rf /var/lib/apt/lists/* \
 && cppcheck --version && test -x /usr/bin/file \
 && python3 -c 'import ensurepip, cryptography, elftools' \
 && dtc --version && ninja --version
RUN set -eu; \
    case "$(dpkg --print-architecture)" in \
      amd64) a=64bit; sha="${TRIVY_SHA256_AMD64}" ;; \
      arm64) a=ARM64; sha="${TRIVY_SHA256_ARM64}" ;; \
      *) echo "unsupported architecture" >&2; exit 1 ;; \
    esac; \
    dl="$(mktemp -d)"; \
    curl -4 -fsSL -o "$dl/trivy.tar.gz" \
      "https://github.com/aquasecurity/trivy/releases/download/${TRIVY_VERSION}/trivy_${TRIVY_VERSION#v}_Linux-${a}.tar.gz"; \
    echo "${sha}  $dl/trivy.tar.gz" | sha256sum -c -; \
    tar -xzf "$dl/trivy.tar.gz" -C "$dl" trivy; \
    install -m 0755 "$dl/trivy" /usr/local/bin/trivy; \
    rm -rf "$dl"; \
    case "$(trivy --version)" in *"Version: ${TRIVY_VERSION#v}"*) ;; *) echo "trivy does not match ${TRIVY_VERSION}" >&2; exit 1 ;; esac
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
LABEL io.quicbaas.worker.project="trusted-firmware"
