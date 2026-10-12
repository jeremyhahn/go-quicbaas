#!/bin/sh
# Integration node entrypoint: derive QUICBAAS_RAFT_MEMBER_IDENTITIES, then exec.
#
# go-quicraft admits a Raft member by its identity -- "spki:" plus the hex
# SHA-256 of the certificate's SubjectPublicKeyInfo, what `qrdbd node-identity
# --cert` prints. The certs-init service issues the fixture certificates at run
# time, so the identities cannot be written into the compose file; each node
# reads its peers' public certificates from the shared certs volume instead.
# Replica <id> presents /etc/quicbaas/certs/node<id>.crt. The local replica's
# own identity is derived by the daemon from its certificate and is skipped.
#
# An explicitly set QUICBAAS_RAFT_MEMBER_IDENTITIES is left untouched.
set -eu

CERT_DIR="${QUICBAAS_CERT_DIR:-/etc/quicbaas/certs}"

member_identity() {
	digest=$(openssl x509 -in "$1" -noout -pubkey | openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1)
	printf 'spki:%s' "$digest"
}

if [ -z "${QUICBAAS_RAFT_MEMBER_IDENTITIES:-}" ] && [ -n "${QUICBAAS_RAFT_JOIN_ADDRESSES:-}" ]; then
	identities=""
	for peer in $(printf '%s' "$QUICBAAS_RAFT_JOIN_ADDRESSES" | tr ',' ' '); do
		id="${peer%%=*}"
		if [ "$id" = "$peer" ] || [ "$id" = "${QUICBAAS_RAFT_NODE_ID:-}" ]; then
			continue
		fi
		cert="$CERT_DIR/node$id.crt"
		if [ ! -f "$cert" ]; then
			echo "node-entrypoint: no certificate for replica $id at $cert; its identity is not set" >&2
			continue
		fi
		identities="${identities:+$identities,}$id=$(member_identity "$cert")"
	done
	if [ -n "$identities" ]; then
		export QUICBAAS_RAFT_MEMBER_IDENTITIES="$identities"
	fi
fi

exec "$@"
