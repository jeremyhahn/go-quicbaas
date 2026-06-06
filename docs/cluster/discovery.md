# Service Discovery

QuicBaaS provides **service discovery** — registering service instances and
resolving them by name — built entirely on the go-qrdb lease + watch + KV
primitives (the etcd/Consul pattern). Use it to let service instances find one
another at runtime.

> **Peer discovery has moved.** Discovering *Raft peers* to form or join a
> cluster (static / multicast / DNS SRV / token) is no longer a QuicBaaS
> concern — it lives in **go-qrdb** (`pkg/discovery` + `pkg/cluster`), the layer
> that owns the cluster. QuicBaaS operates as a qrdb client. See go-qrdb's
> `docs/discovery.md`. The old QuicBaaS peer-discovery engine and the dormant
> raft-membership manager have been removed.

## How it works

The registry (`pkg/registry`) maps each registry operation onto a qrdb
primitive:

| Operation                       | qrdb primitive                              |
|---------------------------------|---------------------------------------------|
| `Register(instance, ttl)`       | `LeaseGrant` + `PutWithLease`               |
| `Renew(leaseID)` (heartbeat)    | `LeaseKeepAlive`                            |
| `Deregister(leaseID)`           | `LeaseRevoke` (deletes the attached entry)  |
| `Resolve(service)`              | KV prefix `Scan`                            |
| `Watch(service)`                | `WatchPrefix`                               |

Key schema → JSON-encoded `ServiceInstance`. Keys live in qrdb's `__sys:`
system namespace (hidden from user-facing KV list/scan). The registry is
**multi-tenant using the platform-standard request-context tenant** (the same
convention as the rest of the stack — see `iam.TenantFromContext`):

- Single-tenant (no tenant in context): `__sys:svc/{service}/{instanceID}`
- Tenant-scoped (tenant in context): `__sys:svc-t/{tenant}/{service}/{instanceID}`

Each `Register`/`Resolve`/`Watch` resolves the tenant from its `context.Context`,
so instances of different tenants are isolated; no per-instance tenant
configuration is needed.

When an instance stops sending heartbeats (e.g. the node dies), its lease
expires, qrdb deletes the attached key, and every `Watch` observer receives a
`Delete` event — automatic, self-healing deregistration.

## SDK usage

The QuicBaaS Go SDK exposes the registry via `Discovery()`:

```go
client, _ := quicbaas.NewClient(addr)
reg, _ := client.Discovery()

// Register under a 10s lease and heartbeat it.
inst := registry.ServiceInstance{ID: "instance-1", Service: "payments", Address: "10.0.0.5:8080"}
leaseID, _ := reg.Register(ctx, inst, 10*time.Second)
// ... periodically: reg.Renew(ctx, leaseID)

// Resolve and watch.
instances, _ := reg.Resolve(ctx, "payments")
events, _ := reg.Watch(ctx, "payments")
for ev := range events {
    // ev.Type is registry.Put or registry.Delete; ev.Instance describes it.
}

// Explicit removal.
reg.Deregister(ctx, leaseID)
```

A runnable example lives at `examples/client/registry/main.go`.

## Notes

- `Resolve` returns a point-in-time snapshot; combine it with `Watch` to track
  changes.
- Choose a TTL comfortably larger than your heartbeat interval so a single
  missed heartbeat does not deregister a healthy instance.
- The registry stores entries in the qrdb system namespace under `__sys:svc/`; do not write
  application data under that prefix.
