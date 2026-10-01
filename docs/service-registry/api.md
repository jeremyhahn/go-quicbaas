# Service Registry — API

> **Status: NORMATIVE** · Last verified: 2026-10-01
> The capability on every transport, with permissions. Feature folder: `docs/service-registry/`.

All verbs are scoped to the authenticated tenant. Lease IDs are 64-bit integers;
the JSON transports carry them as **decimal strings**.

| Verb | Permission | gRPC (`quicbaas.registry.v1.RegistryService`) | REST / QUIC | MCP tool |
|---|---|---|---|---|
| Register | `service_registry:register` | `Register` | `POST /api/v1/registry/instances` → 201 | `quicbaas_registry_register` |
| Renew | `service_registry:register` | `Renew` | `POST /api/v1/registry/leases/{lease_id}/renew` | `quicbaas_registry_renew` |
| Deregister | `service_registry:register` | `Deregister` | `DELETE /api/v1/registry/leases/{lease_id}` → 204 | `quicbaas_registry_deregister` |
| Resolve | `service_registry:read` | `Resolve` | `GET /api/v1/registry/services/{service}` | `quicbaas_registry_resolve` |
| Watch | `service_registry:read` | `Watch` (server stream) | `GET /api/v1/registry/services/{service}/watch?from_revision=N` (SSE) | `quicbaas_registry_watch` (long poll) |

Unix is gRPC over the domain socket; QUIC is the REST dialect over HTTP/3; the
embedded transport calls the service in process.

## Bodies

```json
// Register request
{"instance": {"service": "api", "id": "node-a", "address": "10.0.4.7:8443",
              "metadata": {"zone": "z1"}}, "ttl_seconds": 15}
// Register / Renew response
{"lease_id": "42", "ttl_seconds": 15}
// Resolve response
{"instances": [{"id": "node-a", "service": "api", "address": "10.0.4.7:8443"}], "revision": 118}
```

SSE frames: `id: <revision>` / `event: registry` /
`data: {"type":"PUT"|"DELETE","instance":{…},"revision":N}`; a store-ended
watch closes with `event: error` / `data: {"code":"watch_compacted"|"watch_ended","error":"…"}`.
Idle streams carry `: ping` comments every 15 s.

MCP `quicbaas_registry_watch` arguments `{service, from_revision, timeout_seconds ≤ 30}`
return `{events, revision, compacted, ended, error}`; continue with
`from_revision = revision + 1`.

## Errors

| Condition | gRPC | REST | SDK |
|---|---|---|---|
| Invalid instance, name or TTL | `INVALID_ARGUMENT` | 400 | invalid input |
| Lease not the caller tenant's | `NOT_FOUND` | 404 | not found |
| Watch start revision compacted | `OUT_OF_RANGE` | 410 / `watch_compacted` | `*registry.WatchCompactedError` |
| Store unreachable | `UNAVAILABLE` | 503 | unavailable (watch resumes) |

## Go SDK

```go
reg := client.Registry()                     // or client.ClusterMgmt().Registry()
lease, err := reg.Register(ctx, &quicbaas.RegistryRegisterRequest{...})
renewed, err := reg.Renew(ctx, lease.LeaseID)
err = reg.Deregister(ctx, lease.LeaseID)
snap, err := reg.Resolve(ctx, "api")
events, err := reg.Watch(ctx, &quicbaas.RegistryWatchRequest{Service: "api", FromRevision: snap.Revision + 1})
```

Types live in `sdk/go/client/registry` and are aliased in the root package as
`Registry*` (`RegistryClient`, `RegistryServiceInstance`, `RegistryEvent`,
`RegistryWatchCompactedError`, …).
