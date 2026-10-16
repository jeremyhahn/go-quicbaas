# Service Registry — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-01
> Design, component boundaries and data flow. Feature folder: `docs/service-registry/`.

## Purpose

The service registry is a directory of **service-instance endpoints**. An
instance registers under a lease and stays listed while its owner renews the
lease. When the owner stops — or its host dies — the lease expires, the entry
disappears, and every watcher observes the removal. It is the etcd/Consul
service-registry pattern built on go-qrdb's Lease, MVCC and Watch primitives.

Ownership: handbook doc 00 §R names the registry go-quicbaas's ("lease-based
endpoints: register, renew, deregister, resolve, watch"); doc 13 **G9** recorded
that it had no SDK client. Consumers (go-trusted-platform's service layer first)
realise the network side — addresses, load balancing, DNS — themselves.

## Components

| Layer | Code | Responsibility |
|---|---|---|
| Storage logic | `pkg/registry` | Keys, leases, ownership marks, revisions, watch translation |
| Test store | `pkg/registry/registrytest` | In-memory `Backend` (leases, revisions, history, compaction) |
| Service | `pkg/service/registry_service.go` | Validation, TTL bounds, status codes, platform shard |
| gRPC / Unix | `api/proto/registry.proto`, `pkg/api/grpc/server_registry.go` | `quicbaas.registry.v1.RegistryService` |
| REST / QUIC | `pkg/api/rest/handlers_registry.go` | `/api/v1/registry/*`, SSE watch |
| MCP | `pkg/api/mcp/registry_tools.go`, `registry_mcp.go` | `quicbaas_registry_*` tools, watch long poll |
| SDK | `sdk/go/client/registry` + one handler per transport | `Client.Registry()` |
| CLI | `cmd/quicbaas/registry.go` | `quicbaas registry …` |

## Data model

Every key lives in qrdb's reserved `__sys:` namespace on the platform shard
(shard 1), hidden from user-facing KV list and scan:

| Record | Key | Value | Lease |
|---|---|---|---|
| Instance | `__sys:svc-t/{tenant}/{service}/{id}` | JSON `ServiceInstance` | the registration's |
| Ownership mark | `__sys:svc-lease-t/{tenant}/{leaseID}` | `{service, id}` | the registration's |

A caller with no tenant in context (single-tenant mode, embedded use) uses
`__sys:svc/…` and `__sys:svc-lease/…`. Raw KV is the sanctioned form here
(handbook `conventions/data-access.md` §3 case 5, internal metadata keyed by
name, and case 7, consumed as a watch stream); expiry is the qrdb lease, never a
hand-rolled sweep.

## Run mode: raft only

go-qrdb serves the Lease, MVCC and Watch planes only on a raft node; a
standalone node (`server.mode: standalone`, built with `NewStandaloneNode`)
gates them off. `registry.NewFromClient` therefore asks the node for its
capabilities (`Admin().Capabilities`) before building the registry and refuses
a node that lacks any of the three with `RegistryRequiresRaftError`, which names
the node's reported mode. The service maps it to a failed-precondition status
(`ErrPreconditionFailed`) on every transport, so a client does not retry it as an
outage. A standalone boot still starts, as it does for the scheduler and task
claims, and logs one startup line saying the registry is unavailable; cluster
and devcluster boots serve it.

## Tenancy and lease ownership

The tenant of every call is the **authenticated** tenant the transport's
authorization interceptor puts in the context (`iam.TenantFromContext`); no
request field names one. Register writes the ownership mark before the
instance, both under one lease. Renew and Deregister first prove the mark exists
under the caller's tenant; otherwise they answer `LeaseNotFoundError` — the same
answer for "never existed", "expired" and "another tenant's", so lease IDs
cannot be probed across tenants.

## Revisions and the watch contract

`Resolve` is an MVCC range read and returns the store revision it was served
at. `Watch(service, fromRevision)` opens a qrdb prefix watch at that revision
and delivers each change with its own revision. Resolving and then watching from
`revision+1` therefore observes every later change exactly once.

The SDK client builds on that:

- A watch "from now" (`FromRevision` 0) is first pinned to a revision with
  `Resolve`, so every later reconnect names a concrete revision.
- A dropped connection is resumed from the last delivered revision plus one
  (bounded retries with backoff), on every transport.
- A start revision older than the store's retained history ends the watch with
  `WatchCompactedError`; the caller resolves again. A watch the store cancels for
  another reason ends with `WatchEndedError`; exhausted reconnects end with
  `WatchDisconnectedError`. Every such failure — including a refusal to open —
  arrives as the final event's `Err`.

## Transport mapping of the watch

| Transport | Wire form | Compaction signal |
|---|---|---|
| gRPC, Unix | server stream `RegistryService/Watch` | status `OUT_OF_RANGE` |
| REST, QUIC | `GET …/watch?from_revision=N`, `text/event-stream` | HTTP 410 at open; `event: error` `{"code":"watch_compacted"}` mid-stream |
| MCP | `quicbaas_registry_watch` long poll (≤30 s), cursor = last delivered revision | result `compacted: true` |
| Embedded | in-process stream of SDK events | `WatchCompactedError` message |

MCP is request/response, so the watch is a long poll whose cursor advances only
on delivered changes — the same loss-free cursor contract as go-qrdb's own
`qrdb_watch` tool.
