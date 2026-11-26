# go-xkms lane image: the published worker plus the system packages go-xkms's CI installs.
ARG BASE=ghcr.io/jeremyhahn/quicbaas-software-factory-worker:current
FROM ${BASE}
USER root
RUN apt-get -o Acquire::ForceIPv4=true update \
 && DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true upgrade -y \
 && DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true install -y --no-install-recommends \
      build-essential autoconf automake libtool pkg-config \
      libssl-dev libtss2-dev libusb-1.0-0-dev libicu-dev libpcsclite-dev \
      libgtk-3-dev libwebkit2gtk-4.1-dev \
      swtpm swtpm-tools softhsm2 opensc jq ca-certificates curl \
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
 && { usermod -aG softhsm agent 2>/dev/null || true; }
USER 1000:1000
LABEL io.quicbaas.worker.project="go-xkms"
