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
# The variant runs `apt-get upgrade` because the base image's packages can
# predate Debian security fixes the vulnerability scan reads, and the floor
# list fails the build if a flagged package is still below its fixed version;
# raise a floor (never drop one) when the scan names a newer fix.
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
 && DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true upgrade -y \
 && DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true install -y --no-install-recommends cppcheck ca-certificates curl \
      file libgnutls28-dev python3-pip python3-setuptools \
      bc bison cpio device-tree-compiler flex libslirp-dev ninja-build \
      python3-cryptography python3-pyelftools python3-venv wget \
 && for floor in \
      libkrb5-3=1.21.3-5+deb13u2 libgssapi-krb5-2=1.21.3-5+deb13u2 libk5crypto3=1.21.3-5+deb13u2 \
      libkrb5support0=1.21.3-5+deb13u2 libgssrpc4t64=1.21.3-5+deb13u2 libkadm5clnt-mit12=1.21.3-5+deb13u2 \
      libkadm5srv-mit12=1.21.3-5+deb13u2 libkdb5-10t64=1.21.3-5+deb13u2 libkrb5-dev=1.21.3-5+deb13u2 \
      krb5-multidev=1.21.3-5+deb13u2 linux-libc-dev=6.12.118-1 \
      libwebkit2gtk-4.1-0=2.54.3-1~deb13u1 libjavascriptcoregtk-4.1-0=2.54.3-1~deb13u1 \
      libwebkit2gtk-4.1-dev=2.54.3-1~deb13u1 libjavascriptcoregtk-4.1-dev=2.54.3-1~deb13u1 \
      gir1.2-webkit2-4.1=2.54.3-1~deb13u1 gir1.2-javascriptcoregtk-4.1=2.54.3-1~deb13u1; do \
      pkg="${floor%%=*}"; min="${floor#*=}"; \
      v="$(dpkg-query -W -f='${Version}' "$pkg" 2>/dev/null || true)"; \
      [ -n "$v" ] || continue; \
      dpkg --compare-versions "$v" ge "$min" || { echo "$pkg $v is below its fixed version $min" >&2; exit 1; }; \
    done \
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
USER 1000:1000
LABEL io.quicbaas.worker.project="trusted-firmware"
