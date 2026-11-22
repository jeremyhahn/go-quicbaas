#!/bin/sh
# Moves the three QuicBaaS replicas onto the StatefulSet's update revision,
# one at a time, without losing the Raft quorum.
#
# Usage: sh rolling-upgrade.sh
#
# The StatefulSet uses updateStrategy OnDelete, so applying a new image tag
# changes no running pod. This script then replaces the replicas, highest
# ordinal first. Before each one it waits until all three replicas are
# ready, and it removes the pod through the Eviction API, so the
# PodDisruptionBudget (minAvailable: 2) decides whether the pod may go. A
# refused eviction, or replicas that do not become ready, stop the script. A
# replica already on the update revision is skipped, so running the script
# again resumes the upgrade. See docs/deployment/kubernetes.md, "Upgrades".
set -eu

ns=quicbaas
sts=quicbaas

# all_ready waits up to 5 minutes for all three replicas to be ready, and
# stops the script, naming $1, when they are not.
all_ready() {
  tries=0
  while :; do
    ready="$(kubectl -n "$ns" get statefulset "$sts" -o 'jsonpath={.status.readyReplicas}')"
    if [ "${ready:-0}" = 3 ]; then
      return 0
    fi
    tries=$((tries + 1))
    if [ "$tries" -ge 150 ]; then
      echo "rolling-upgrade: only ${ready:-0} of 3 replicas are ready; $1" >&2
      exit 1
    fi
    sleep 2
  done
}

rev="$(kubectl -n "$ns" get statefulset "$sts" -o 'jsonpath={.status.updateRevision}')"
if [ -z "$rev" ]; then
  echo "rolling-upgrade: StatefulSet $sts reports no update revision" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

for n in 2 1 0; do
  pod="quicbaas-$n"
  cur="$(kubectl -n "$ns" get pod "$pod" -o 'jsonpath={.metadata.labels.controller-revision-hash}')"
  if [ "$cur" = "$rev" ]; then
    echo "$pod already runs $rev"
    continue
  fi
  all_ready "$pod was not evicted"
  old_uid="$(kubectl -n "$ns" get pod "$pod" -o 'jsonpath={.metadata.uid}')"

  # The Eviction API refuses (HTTP 429) an eviction that would break the
  # PodDisruptionBudget's minAvailable; kubectl then exits non-zero and set -e
  # stops the script with the pod still running.
  printf '{"apiVersion":"policy/v1","kind":"Eviction","metadata":{"name":"%s","namespace":"%s"}}\n' "$pod" "$ns" \
    > "$tmp/eviction.json"
  kubectl create --raw "/api/v1/namespaces/$ns/pods/$pod/eviction" -f "$tmp/eviction.json"

  tries=0
  while :; do
    uid="$(kubectl -n "$ns" get pod "$pod" -o 'jsonpath={.metadata.uid}' 2>/dev/null || true)"
    if [ -n "$uid" ] && [ "$uid" != "$old_uid" ]; then
      break
    fi
    tries=$((tries + 1))
    if [ "$tries" -ge 150 ]; then
      echo "rolling-upgrade: $pod was not replaced within 5 minutes" >&2
      exit 1
    fi
    sleep 2
  done
  kubectl -n "$ns" wait --for=condition=Ready "pod/$pod" --timeout=5m
  echo "$pod runs $rev and is ready"
done

all_ready "the upgrade did not settle"
echo "all three replicas run $rev"
