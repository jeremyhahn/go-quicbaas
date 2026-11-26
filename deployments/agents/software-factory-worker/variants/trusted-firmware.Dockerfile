# trusted-firmware lane image: the published worker plus cppcheck, trivy and the OP-TEE QEMU build's host tools; trivy's pins match pkg/version/toolchain.mk (variants_test.go).
ARG BASE=ghcr.io/jeremyhahn/quicbaas-software-factory-worker:current
FROM ${BASE}
USER root
ARG TRIVY_VERSION=v0.76.2
ARG TRIVY_SHA256_AMD64=bc0fc726e017b8927a62b2d3531d7a84ee01769daf5c99ed866dced54089fdbf
ARG TRIVY_SHA256_ARM64=ab9d51f86e6a5fb936f605faff6b593ac51bc378fff4d3e8b9d58358b9c367c3
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
