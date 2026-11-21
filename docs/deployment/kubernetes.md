# Kubernetes Deployment Guide

> **Status: REFERENCE** · Last verified: 2026-11-14
> Kubernetes deployment of QuicBaaS: the manifests in
> [`deployments/kubernetes`](../../deployments/kubernetes).

QuicBaaS runs on Kubernetes as a 3-node Raft cluster. All three nodes run one
go-quicbaas release tag. The manifests are plain YAML and kustomize; there is
no Helm chart.

| File | What it holds |
|------|---------------|
| `kustomization.yaml` | Namespace, common labels, and the one image tag |
| `namespace.yaml` | Namespace `quicbaas`, Pod Security `restricted` |
| `configmap.yaml` | `quicbaas.yaml`, the node configuration every replica shares |
| `services.yaml` | Headless `quicbaas-raft` (Raft peers) and `quicbaas` (clients) |
| `statefulset.yaml` | Three `quicbaasd node` replicas with a data volume each |
| `poddisruptionbudget.yaml` | Keeps two of three nodes up during voluntary disruption |
| `networkpolicy.yaml` | Raft from QuicBaaS pods only; REST and gRPC from any pod |

## Prerequisites

- Kubernetes 1.25 or newer, and `kubectl` with built-in kustomize (`kubectl apply -k`).
- At least three schedulable nodes. The pods require anti-affinity on
  `kubernetes.io/hostname`, so two replicas never share a node.
- A default StorageClass that provisions `ReadWriteOnce` volumes. Each replica
  claims 20Gi.
- A CNI that enforces NetworkPolicy, if you rely on `networkpolicy.yaml`.
- Access to `ghcr.io/jeremyhahn/quicbaasd`.

## Ports

| Port | Protocol | Use |
|------|----------|-----|
| 8080 | TCP | REST API (HTTPS) and health probes |
| 50051 | TCP | gRPC (TLS) |
| 63000 | UDP | Raft over QUIC, mutual TLS between the nodes |

## Secrets

Create the namespace and three Secrets before the first apply:

```bash
kubectl create namespace quicbaas

# Raft member identities: spki: plus the SHA-256 of each replica
# certificate's SubjectPublicKeyInfo, keyed by Raft node ID (ordinal + 1).
spki() { openssl x509 -in "$1" -noout -pubkey |
  openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1; }
printf '1=spki:%s,2=spki:%s,3=spki:%s\n' "$(spki quicbaas-0.crt)" \
  "$(spki quicbaas-1.crt)" "$(spki quicbaas-2.crt)" > member-identities

# Raft mTLS: one CA, one certificate per replica named for its ordinal, and
# the member identities.
kubectl -n quicbaas create secret generic quicbaas-raft-tls \
  --from-file=ca.crt --from-file=member-identities \
  --from-file=quicbaas-0.crt --from-file=quicbaas-0.key \
  --from-file=quicbaas-1.crt --from-file=quicbaas-1.key \
  --from-file=quicbaas-2.crt --from-file=quicbaas-2.key

# The certificate REST and gRPC serve.
kubectl -n quicbaas create secret tls quicbaas-server-tls \
  --cert=server.crt --key=server.key

# The passphrase that unseals each node at start.
kubectl -n quicbaas create secret generic quicbaas-seal \
  --from-literal=passphrase="$(openssl rand -base64 32)"
```

Issue each Raft certificate for its replica's stable name,
`quicbaas-<ordinal>.quicbaas-raft.quicbaas.svc.cluster.local`. Issue the server
certificate for the names clients use, such as `quicbaas.quicbaas.svc`.

go-quicraft admits a founding member only by its identity. Without the
`member-identities` key the cluster could not bootstrap, so the start command
refuses to launch `quicbaasd` when the key is missing, malformed or lacks a
peer. Regenerate the key, and restart the pods, whenever a replica certificate
is reissued with a new key. The format matches `raft.member_identities` in
[cluster architecture](../cluster/architecture.md).
Keep the seal passphrase outside the cluster as well. Without it, a node's
data cannot be unsealed.

## Deploy

```bash
kubectl apply -k deployments/kubernetes
kubectl -n quicbaas rollout status statefulset/quicbaas
```

## One release tag

`kustomization.yaml` sets the image tag once, under `images:`. The containers
in `statefulset.yaml` name the image without a tag, so every replica runs the
same build. The tag is the repository's `VERSION`. The release workflow
publishes `v<VERSION>` as `ghcr.io/jeremyhahn/quicbaasd:<VERSION>`.
`test/buildgates/kubernetes_test.go` fails when the pin and `VERSION` drift
apart, when the pin is `latest`, or when a container carries a tag of its own.

The image must serve REST, because the probes and clients use it. Releases
build `Dockerfile` with its default `BUILD_TAGS`, which include `rest` from
the first release after 0.16.0-alpha. 0.16.0-alpha itself was built with
`gms_pure_go` alone and registers no REST routes, so its pods never pass a
probe. `test/buildgates/image_tags_test.go` fails when a published
Dockerfile's default tags exclude the probe routes.

## How the cluster forms

Membership is static, the same shape as
`test/integration/docker-compose.cluster.yml`:

- The StatefulSet gives replica *n* the stable DNS name
  `quicbaas-n.quicbaas-raft.<namespace>.svc.cluster.local`.
- Each container's start command derives the node's settings from its
  ordinal and exports them before `exec quicbaasd node`:
  - `QUICBAAS_RAFT_NODE_ID` is the ordinal plus 1.
  - `QUICBAAS_RAFT_RAFT_ADDRESS` is the node's own DNS name on port 63000.
  - `QUICBAAS_RAFT_LISTEN_HOST` is the pod IP.
  - `QUICBAAS_RAFT_JOIN_ADDRESSES` lists all three members as `id=host:63000`.
  - `QUICBAAS_RAFT_MEMBER_IDENTITIES` holds the other two replicas' entries
    from the `member-identities` Secret key. The node derives its own
    identity from its certificate.
  - `QUICBAAS_RAFT_TLS_CERT_FILE` and `QUICBAAS_RAFT_TLS_KEY_FILE` point at
    the node's own certificate.
- `podManagementPolicy: Parallel` starts all three pods at once. A Raft group
  needs a quorum of members before any of them is ready.
- `quicbaas-raft` sets `publishNotReadyAddresses: true`. The peers can then
  resolve one another before readiness, which waits for the group to form.
- Every node shares `raft.deployment_id: 1` from the ConfigMap.

`cmd/quicbaasd/kubernetes_config_test.go` runs the daemon's configuration path
on the ConfigMap plus replica 1's exports, and checks that it comes up as Raft
node 2 with the three members.

## Probes

All three probes use HTTPS on the `rest` port. REST always serves TLS.

| Probe | Path | Notes |
|-------|------|-------|
| startup | `/api/v1/health/live` | Allows up to 5 minutes for unseal and start |
| liveness | `/api/v1/health/live` | Restarts a wedged process |
| readiness | `/api/v1/health/ready` | Admits the pod to the `quicbaas` client Service |

`pkg/api/rest/health_paths_test.go` fails if a probe path is not a served route.

## Failover

- Losing one node of three keeps the Raft quorum, so the cluster keeps serving.
- Anti-affinity puts the replicas on different nodes, so one Kubernetes node
  failure takes at most one replica.
- The PodDisruptionBudget (`minAvailable: 2`) stops drains and evictions from
  taking a second replica.
- A replaced pod keeps its ordinal, DNS name and PersistentVolumeClaim, so
  it rejoins as the same Raft member with its log.

`make integration-test-cluster` and `make integration-test-cluster-failover`
prove formation, smoke traffic and leader failover on a 3-node cluster of one
build. They run under Docker in CI.

## Upgrades

Change `newTag` in `kustomization.yaml`, together with `VERSION`, and apply.
The StatefulSet's `RollingUpdate` replaces one pod at a time, highest ordinal
first, and waits for each to be ready. The PDB holds the quorum throughout.

## Security

- The pods run as uid/gid 1000, non-root, with a read-only root filesystem, all
  capabilities dropped, and the `RuntimeDefault` seccomp profile.
- The Secrets mount with mode `0440`, readable through `fsGroup: 1000`.
- The data at rest is sealed, and auth is on (`seal.enabled`, `auth.enabled`).
- Narrow the client ingress rule in `networkpolicy.yaml` to the namespaces
  that call QuicBaaS.

See the [production checklist](production-checklist.md) before going live.
