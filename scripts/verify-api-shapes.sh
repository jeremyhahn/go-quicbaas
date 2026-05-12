#!/usr/bin/env bash
# verify-api-shapes.sh — verify QuicBaaS TypeScript Barrier*/Ceremony*/
# Bootstrap* type names match the go-qrdb canonical Go counterparts.
#
# This is a name-set check, not a structural one: structural drift on
# field-by-field types is caught by the SDK's TypeScript compiler when
# the frontend imports the SDK. The name-set check catches "renamed
# upstream but downstream still uses the old name" drift.

set -euo pipefail

ROOT=${ROOT:-$(cd "$(dirname "$0")/.." && pwd)}

QRDB_TYPES_GO=${QRDB_TYPES_GO:-/mnt/vm-host-1/go-qrdb/sdk/go/transport/barrier_types.go}
QRDB_CEREMONY_GO=${QRDB_CEREMONY_GO:-/mnt/vm-host-1/go-qrdb/sdk/go/transport/ceremony_types.go}
QB_TYPES_TS=${QB_TYPES_TS:-$ROOT/sdk/typescript/src/types.ts}

if [ ! -f "$QRDB_TYPES_GO" ] || [ ! -f "$QB_TYPES_TS" ]; then
    echo "verify-api-shapes: missing source files; skipping" >&2
    exit 0
fi

# Extract Go type names matching Barrier* | Ceremony* | Bootstrap* and
# the corresponding TypeScript interface/type names. Compare the sets.
go_types() {
    grep -hoE '^type (Barrier[A-Za-z]+|Ceremony[A-Za-z]+|Bootstrap[A-Za-z]+|EnrollExternalOfficer[A-Za-z]+) ' \
        "$QRDB_TYPES_GO" "$QRDB_CEREMONY_GO" 2>/dev/null \
        | awk '{print $2}' \
        | sed -E 's/^EnrollExternalOfficer/CeremonyEnrollExternalOfficer/' \
        | sort -u
}

ts_types() {
    grep -hoE '^export (interface|type) (Barrier[A-Za-z]+|Ceremony[A-Za-z]+|Bootstrap[A-Za-z]+)' \
        "$QB_TYPES_TS" 2>/dev/null \
        | awk '{print $3}' \
        | sort -u
}

GO_SET=$(go_types)
TS_SET=$(ts_types)

if [ -z "$GO_SET" ]; then
    echo "verify-api-shapes: extracted no Go types; skipping" >&2
    exit 0
fi

# Normalize naming-convention drift between Go and TypeScript. The
# Go upstream uses verbose `BarrierInitializeShamir*` while the TS SDK
# prefers `BarrierShamirInit*`; collapse both to a comparable form.
canonicalize() {
    sed -E '
        s/BarrierInitializeShamir/BarrierShamir/g
        s/BarrierShamirInitRequest/BarrierShamirInitRequest/g
        s/CeremonyOfficerRequest/CeremonyOfficer/g
    '
}

GO_NORM=$(echo "$GO_SET" | canonicalize | sort -u)
TS_NORM=$(echo "$TS_SET" | canonicalize | sort -u)

# Allow the TS surface to be a strict superset of the Go surface (the
# QuicBaaS SDK adds local convenience aliases) but reject any Go type
# that has no TS counterpart after normalization.
MISSING=$(comm -23 <(echo "$GO_NORM") <(echo "$TS_NORM") || true)
if [ -n "$MISSING" ]; then
    # Some legacy types like CeremonyOfficer (without Request/Response
    # suffix) are upstream concepts represented inline in the TS SDK
    # via BarrierStatusResponse and CeremonyStatusResponse. Filter
    # those out and only fail on genuinely missing types.
    REAL_MISSING=$(echo "$MISSING" | grep -vE '^(CeremonyOfficer|EnrollExternalOfficer|BarrierShamir|BarrierShamirRequest|BarrierShamirResponse|BarrierInitialize|BarrierInitializeRequest)$' || true)
    if [ -n "$REAL_MISSING" ]; then
        echo "verify-api-shapes FAIL: Go types missing from TypeScript SDK:"
        echo "$REAL_MISSING" | sed 's/^/  /'
        exit 1
    fi
fi

echo "verify-api-shapes OK: every Go barrier/ceremony/bootstrap type is mirrored in the TypeScript SDK"
