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

# Raft mTLS: one CA, and one certificate per replica, named for its ordinal.
kubectl -n quicbaas create secret generic quicbaas-raft-tls \
  --from-file=ca.crt \
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
