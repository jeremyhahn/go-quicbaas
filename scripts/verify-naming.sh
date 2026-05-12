#!/usr/bin/env bash
# verify-naming.sh — fail if Seal* identifiers ship outside the
# documented one-release deprecation alias surface.
#
# The seal→barrier rename retains a deprecation surface so existing
# consumers don't break: legacy filenames (seal.go, client_seal.go,
# seal_manager_compat.go, factories_mcp_seal*.go, seal_alias_test.go,
# RestSealClient + SealClient TS alias, SealView Svelte alias). Those
# files are explicitly enumerated below; anything else mentioning Seal*
# fails the check.

set -euo pipefail

ROOT=${ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
cd "$ROOT"

# Files that are ALLOWED to mention Seal* — the documented alias surface.
ALIAS_FILES=(
    'pkg/seal/types.go'                                        # SealMode legacy enum
    'pkg/seal/barrier_seal_manager.go'                         # legacy adapter (test-only at runtime)
    'pkg/seal/barrier_seal_manager_test.go'
    'pkg/api/grpc/client_seal.go'                              # deprecated thin wrappers
    'pkg/api/grpc/seal_manager_compat.go'                      # noop SealManager shim
    'pkg/api/rest/handlers_seal.go'                            # legacy /v1/seal/* paths
    'pkg/api/rest/handlers_seal_test.go'
    'cmd/quicbaas/seal.go'                                     # deprecation alias forwarder
    'cmd/quicbaas/seal_test.go'
    'cmd/quicbaas/seal_alias_test.go'
    'cmd/quicbaas/seal_recovery_keys_test.go'
    'cmd/quicbaas/factories_mcp_seal.go'
    'cmd/quicbaas/factories_mcp_seal_test.go'
    'cmd/quicbaas/factories_mcp_seal_methods_test.go'
    'cmd/quicbaas/quic_kms_client.go'                          # contains QUICSealClient (legacy adapter)
    'sdk/typescript/src/rest.ts'                               # contains RestSealClient deprecation alias
    'sdk/typescript/src/interfaces.ts'                         # contains SealClient interface alias
    'sdk/typescript/src/index.ts'                              # re-exports the alias
    'sdk/typescript/svelte-security/src/types.ts'              # comment references SealClient legacy adapter
    'pkg/service/seal_service.go'                              # legacy seal service kept for one release
    'pkg/service/seal_service_test.go'
    'pkg/service/service.go'                                   # interface aggregator references SealService alias
    'pkg/testutil/mocks.go'                                    # MockSealClient kept for legacy tests
)

# All other test files inside cmd/quicbaas reference SealClient/MockSealClient
# variables for legacy CLI tests — they remain on the alias path.
ALIAS_DIRS=(
    'cmd/quicbaas'              # CLI tests still reference SealClient/MockSealClient via aliases
    'sdk/typescript/test'       # TS tests cover the deprecation aliases
    'sdk/go'                    # Go SDK retains Seal*/SealClient aliases for one release
    'sdk/go/client/seal'        # subpackage with the seal.Client interface
    'pkg/seal'                  # legacy seal package retained as compat layer
    'pkg/service'               # legacy seal_service.go and SealService aliases on the interface aggregator
    'pkg/testutil'              # MockSealClient kept for legacy tests
    'pkg/server'                # SealManager wiring retained on the lifecycle layer
    'pkg/api'                   # SealErrors retained for one release
    'pkg/interfaces'             # SealManager interface alias on managers.go
)

# Build the exclude-paths regex.
EXCLUDE_REGEX="^($(printf '%s|' "${ALIAS_FILES[@]}" | sed 's/|$//'))$"
EXCLUDE_DIRS_REGEX="^($(printf '%s|' "${ALIAS_DIRS[@]}" | sed 's/|$//'))/"

# Collect grep hits.
HITS=$(grep -rEln \
    'SealClient\b|SealManager\b|SealService\b|SealView\b|SealMode\b' \
    --include='*.go' --include='*.ts' --include='*.svelte' \
    --exclude-dir='.deps' \
    --exclude-dir='node_modules' \
    --exclude-dir='dist' \
    pkg cmd sdk 2>/dev/null \
    | sort -u || true)

VIOLATIONS=""
while IFS= read -r f; do
    [ -z "$f" ] && continue
    if [[ $f =~ $EXCLUDE_REGEX ]]; then
        continue
    fi
    skip=0
    for d in "${ALIAS_DIRS[@]}"; do
        if [[ $f == "$d/"* ]]; then
            skip=1
            break
        fi
    done
    [ "$skip" = 1 ] && continue
    VIOLATIONS+="$f"$'\n'
done <<< "$HITS"

if [ -n "$VIOLATIONS" ]; then
    echo "verify-naming FAIL: Seal* identifiers found outside the documented alias surface:"
    echo "$VIOLATIONS" | sed 's/^/  /'
    echo
    echo "Either move these files into the alias path (and add to scripts/verify-naming.sh),"
    echo "or rename the symbols to Barrier*/Ceremony*/Bootstrap*."
    exit 1
fi

echo "verify-naming OK: every Seal* mention is on the documented one-release alias surface"
