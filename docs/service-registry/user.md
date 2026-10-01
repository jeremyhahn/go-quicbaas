# Service Registry — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-01
> What it is for and how to use it. Feature folder: `docs/service-registry/`.

Use the registry to let instances of a service find each other at runtime.

1. **Register** each instance with a lease TTL (say 15 s) and **renew** it on a
   timer well inside the TTL (every 5 s). If the process or its host dies, the
   renewals stop, the lease lapses and the instance is removed automatically.
2. **Resolve** a service to get its current instances and the revision they were
   read at.
3. **Watch** from that revision plus one to keep a local view current. Every
   change arrives once, in order; the SDK reconnects transparently from where it
   left off.
4. **Deregister** on clean shutdown so watchers see the removal at once.

```go
reg := client.Registry()
lease, _ := reg.Register(ctx, &quicbaas.RegistryRegisterRequest{
    Instance:   quicbaas.RegistryServiceInstance{Service: "api", ID: "node-a", Address: "10.0.4.7:8443"},
    TTLSeconds: 15,
})
snap, _ := reg.Resolve(ctx, "api")
events, _ := reg.Watch(ctx, &quicbaas.RegistryWatchRequest{Service: "api", FromRevision: snap.Revision + 1})
for ev := range events {
    if ev.Err != nil {
        // WatchCompactedError: resolve again and watch from the new revision.
        break
    }
    // apply ev.Type (PUT/DELETE) to ev.Instance
}
```

Registrations are private to your tenant: another tenant can neither see them
nor renew or revoke your leases. A runnable example lives in
`examples/client/registry/main.go`.
