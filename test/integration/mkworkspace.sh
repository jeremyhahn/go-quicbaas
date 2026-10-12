#!/bin/sh
# mkworkspace.sh writes the go.work the seal integration images build in.
#
# Usage: mkworkspace.sh [OUTPUT]   (default: ./go.work)
#
# Run it from the directory that holds the sibling checkouts (go-qrdb,
# go-xkms, ... and go-quicbaas itself) copied in from the BuildKit `workspace`
# named context. Dockerfile.seal and Dockerfile.seal-hardware both run this one
# script, so the module list lives in exactly one place
# (go-quicbaas#399; `make check-workspace-script` keeps it that way).
#
# The `go` line is the toolchain the image actually carries (`go env
# GOVERSION`), never a literal: a literal drifts from the base image tag, and a
# go line newer than the toolchain makes every build in the image fail.
#
# GO overrides the go binary consulted, for the script's own test.
set -eu

out=${1:-go.work}
gobin=${GO:-go}

goversion=$("$gobin" env GOVERSION) || {
	echo "mkworkspace: '$gobin env GOVERSION' failed" >&2
	exit 1
}
# GOVERSION may carry a suffix such as " X:nodwarf5"; the first word is the
# release ("go1.27.2", "go1.28rc1").
goversion=${goversion%% *}
version=${goversion#go}
case $goversion in
go[0-9]*) ;;
*) version= ;;
esac
case $version in
"" | *[!0-9a-z.]*)
	echo "mkworkspace: cannot derive a go line from GOVERSION '$goversion'" >&2
	exit 1
	;;
esac

cat >"$out" <<EOF
go $version

use (
	./go-libsignal
	./go-objstore
	./go-objstore/api/sdks/go
	./go-qrdb
	./go-qrdb/sdk/go
	./go-quicbaas
	./go-quicbaas/sdk/go
	./go-quicraft
	./go-signal
	./go-xkms
	./go-codec
	./go-frost
	./go-xkms/sdk/go
	./go-xkms/xkey
)

replace (
	github.com/jeremyhahn/go-libsignal v0.0.0       => ./go-libsignal
	github.com/jeremyhahn/go-objstore v0.0.0        => ./go-objstore
	github.com/jeremyhahn/go-qrdb v0.0.0            => ./go-qrdb
	github.com/jeremyhahn/go-qrdb/sdk/go v0.0.0     => ./go-qrdb/sdk/go
	github.com/jeremyhahn/go-quicbaas v0.0.0        => ./go-quicbaas
	github.com/jeremyhahn/go-quicbaas/sdk/go v0.0.0 => ./go-quicbaas/sdk/go
	github.com/jeremyhahn/go-quicraft v0.0.0        => ./go-quicraft
	github.com/jeremyhahn/go-signal v0.0.0          => ./go-signal
	github.com/jeremyhahn/go-xkms v0.0.0            => ./go-xkms
	github.com/jeremyhahn/go-codec v1.3.0          => ./go-codec
	github.com/jeremyhahn/go-frost v0.1.4-alpha    => ./go-frost
	github.com/jeremyhahn/go-frost v0.1.6-alpha    => ./go-frost
	github.com/jeremyhahn/go-xkms/sdk/go v0.0.0     => ./go-xkms/sdk/go
	github.com/jeremyhahn/go-objstore v0.1.4-alpha => ./go-objstore
	github.com/jeremyhahn/go-xkms v0.2.2-alpha    => ./go-xkms
)
EOF
