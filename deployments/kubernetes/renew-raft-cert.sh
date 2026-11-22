#!/bin/sh
# Renews replica n's Raft certificate on its existing key.
#
# Usage: sh renew-raft-cert.sh <0|1|2>
#
# Run it from the directory that holds quicbaas-0..2.crt/.key (with the
# renewed quicbaas-<n>.crt) and old/quicbaas-<n>.crt (the certificate it
# replaces). go-quicraft admits a member only by the SPKI identity committed
# at formation, so the script stops, before it touches the cluster, when the
# renewed certificate presents a different key. It then updates Secret
# quicbaas-raft-keys, evicts quicbaas-<n> through the Eviction API, so the
# PodDisruptionBudget decides whether the pod may go, and returns once the
# replacement pod is Ready. See docs/deployment/kubernetes.md.
set -eu

n="${1:-}"
case "$n" in
  0 | 1 | 2) ;;
  *)
    echo "usage: sh renew-raft-cert.sh <0|1|2>" >&2
    exit 2
    ;;
esac

for f in quicbaas-0.crt quicbaas-0.key quicbaas-1.crt quicbaas-1.key \
  quicbaas-2.crt quicbaas-2.key "old/quicbaas-$n.crt"; do
  if [ ! -f "$f" ] || [ ! -s "$f" ]; then
    echo "renew-raft-cert: $f is missing or empty" >&2
    exit 1
  fi
done

spki() { openssl x509 -in "$1" -noout -pubkey |
  openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1; }

# The SHA-256 of no input: what spki prints when openssl read nothing.
empty_sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
new_id="$(spki "quicbaas-$n.crt")"
old_id="$(spki "old/quicbaas-$n.crt")"
if [ "${#new_id}" -ne 64 ] || [ "${#old_id}" -ne 64 ] ||
  [ "$new_id" = "$empty_sha256" ] || [ "$old_id" = "$empty_sha256" ]; then
  echo "renew-raft-cert: cannot read the SPKI identity" >&2
  exit 1
fi
if [ "$new_id" != "$old_id" ]; then
  echo "renew-raft-cert: quicbaas-$n.crt presents a different key than old/quicbaas-$n.crt; a new key is a membership change, not a renewal" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# No pipe: sh has no pipefail, so a failed create must stop the script here.
kubectl -n quicbaas create secret generic quicbaas-raft-keys \
  --from-file=quicbaas-0.crt --from-file=quicbaas-0.key \
  --from-file=quicbaas-1.crt --from-file=quicbaas-1.key \
  --from-file=quicbaas-2.crt --from-file=quicbaas-2.key \
  --dry-run=client -o yaml > "$tmp/secret.yaml"
kubectl apply -f "$tmp/secret.yaml"

old_uid="$(kubectl -n quicbaas get pod "quicbaas-$n" -o 'jsonpath={.metadata.uid}')"

# The Eviction API refuses (HTTP 429) an eviction that would break the
# PodDisruptionBudget's minAvailable; kubectl then exits non-zero and set -e
# stops the script with the pod still running.
printf '{"apiVersion":"policy/v1","kind":"Eviction","metadata":{"name":"quicbaas-%s","namespace":"quicbaas"}}\n' "$n" \
  > "$tmp/eviction.json"
kubectl create --raw "/api/v1/namespaces/quicbaas/pods/quicbaas-$n/eviction" -f "$tmp/eviction.json"

tries=0
while :; do
  uid="$(kubectl -n quicbaas get pod "quicbaas-$n" -o 'jsonpath={.metadata.uid}' 2>/dev/null || true)"
  if [ -n "$uid" ] && [ "$uid" != "$old_uid" ]; then
    break
  fi
  tries=$((tries + 1))
  if [ "$tries" -ge 150 ]; then
    echo "renew-raft-cert: quicbaas-$n was not replaced within 5 minutes" >&2
    exit 1
  fi
  sleep 2
done

kubectl -n quicbaas wait --for=condition=Ready "pod/quicbaas-$n" --timeout=5m
echo "quicbaas-$n renewed and ready"
