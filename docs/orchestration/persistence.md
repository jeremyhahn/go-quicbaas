# Orchestration Persistence

Projects, agent registrations and tasks are durable KV-backed records. They
survive a daemon restart, stay isolated per tenant by key construction, are
encrypted at rest by the owning tenant's barrier, and emit an audit event on
every create, update and delete.

## One implementation, two backings

There is exactly one implementation of each store. Whether state is durable is
decided solely by which `interfaces.KVStore` is injected:

```go
// Durable: qrdb-backed, replicated, barrier-encrypted.
store := orchestration.NewProjectStore(server.durableKVStore())

// Ephemeral: qrdb's in-memory engine, for unit tests and embedded callers.
store := orchestration.NewInMemoryProjectStore() // == NewProjectStore(kvstore.NewMemory())
```

`pkg/kvstore` is the single bridge from a qrdb storage engine
(`qrdbsdk.Storage`) to `interfaces.KVStore`. `kvstore.NewMemory()` wraps
`qrdbsdk.NewMemoryStorage()`, qrdb's ordered B-tree engine, so tests observe the
same lexical scan ordering the durable engines produce. QuicBaaS does not
implement a storage engine of its own.

| Constructor | Backing |
| --- | --- |
| `NewProjectStore(kv, opts...)` | caller's KV |
| `NewAgentRegistry(kv, opts...)` | caller's KV |
| `NewTaskStore(kv, opts...)` | caller's KV |
| `NewInMemoryProjectStore()` | `kvstore.NewMemory()` |
| `NewInMemoryAgentRegistry()` | `kvstore.NewMemory()` |
| `NewInMemoryTaskStore()` | `kvstore.NewMemory()` |
| `NewInMemoryTaskStoreWithKV(kv, limit)` | caller's KV, custom inline limit |

A store constructed with a nil KV fails closed on every verb. A nil KV is a
wiring bug, not a storage mode.

## Key layout

Every record's key puts the owning tenant in the first variable segment:

```
orch/project/{tenant}/{projectID}            -> Project (JSON)
orch/projectname/{tenant}/{name}             -> projectID (name index)
orch/agent/{tenant}/{agentID}                -> AgentRegistration (JSON)
orch/task/{tenant}/{taskID}                  -> Task (JSON)
orch/taskartifact/{tenant}/{taskID}/{name}   -> artifact payload (bytes)
```

Keys carry identifiers only — no secret, token or payload value is ever encoded
into a key, so keys stay safe to log and to expose in operator tooling.

## Tenant isolation

Cross-tenant access is prevented by construction, not by convention:

1. Every method builds its key or scan prefix from the caller-supplied
   `tenantID`. No method accepts a bare entity ID.
2. A list is a prefix scan bounded to `orch/{entity}/{tenant}/`, so it cannot
   observe another tenant's subtree. `pkg/kvstore` implements `Scan` as a
   bounded iterator range rather than a filtered full-keyspace walk.
3. Identifiers that are empty or contain the `/` separator are rejected
   (`ErrInvalidKeySegment`). Without this a crafted ID such as
   `other-tenant/x` could escape its tenant's subtree.
4. After decoding a record the store re-checks `record.TenantID == tenantID`
   and treats a mismatch as not-found. This is defence in depth against key
   corruption, not the primary control.
5. A task artifact reference is dereferenced only when it points inside that
   task's own artifact prefix, so a caller-supplied `Ref.Path` cannot be turned
   into an arbitrary read of the shared keyspace.

The store layer's own tests assert these properties directly, including the
case where one tenant ID is a lexical prefix of another (`t1` and `t10`).

## Encryption at rest

The stores hold no crypto path of their own. Confidentiality comes from qrdb's
`StorageBarrier`, which encrypts everything written through the node's store.

The server binds them to `Server.durableKVStore()`
(`pkg/server/tenant_kvstore.go`), the same seam the IAM stores use. When the
embedded qrdb node exposes a per-tenant `StoreResolver`, that returns a
`tenantRoutingKVStore`: each operation routes to the context tenant's backend,
so a barrier-isolated tenant's orchestration records live inside that tenant's
own barrier with its own DEK and namespace. A sealed barrier tenant **fails
closed** — the operation errors and never falls back to the shared store.

Without a resolver (seal disabled, storage-less boot) the store is the shared
platform shard, where the tenant-first key layout still keeps records separated.

The routing store reads the request tenant from either the IAM or the
orchestration context key, because the two packages deliberately keep separate
unexported keys carrying the same value.

## Audit

Every create, update and delete emits one `orchestration.StoreAuditEvent`
through the `StoreAuditor` sink. The server binds that sink to the platform
`iam.AuditLogger` (`pkg/server/orchestration_store_auditor.go`), so
orchestration mutations land on the same tamper-evident trail as IAM changes —
what FedRAMP AU-2/AU-3, PCI-DSS 10.2 and HIPAA 164.312(b) each require.

- Rejected mutations are recorded with `Success: false`; a denied change is
  itself an auditable event.
- Reads are not audited.
- Agent heartbeats are not audited — they are high-frequency telemetry, not a
  state change.
- Events carry identifiers only, never record contents, so the trail never
  becomes a second copy of the data.
- The logger is resolved per event rather than captured at construction,
  because the stores are built before IAM initialization publishes it.

## Concurrency

`interfaces.KVStore` exposes no compare-and-swap verb, and neither does qrdb's
re-exported `kvstore.KVStore`. Atomic transitions therefore use two mechanisms
together:

- a `keyedMutex` lock held across the whole read-modify-write, which makes the
  update atomic against other writers **in this process**; and
- the record's `Version` counter, which **detects** a writer that raced from
  another process or Raft node and surfaces `ErrTaskVersionConflict`.

Cross-record writes stay parallel. A true multi-writer CAS needs a
compare-and-swap verb that qrdb's SDK does not currently re-export; when it
does, the read-modify-write is replaced by it and `Version` keeps its meaning.

## Still in memory

Two orchestration components remain process-local and reset on restart:

- the job KV store (`pkg/server/lifecycle_orchestration.go`), which also backs
  the session event log — so `RecoverJobs` runs against a store that is empty
  by construction at boot; and
- the agent session store (`orchestration.NewInMemoryAgentStore`).

Both are separate from the project, agent-registry and task state described
here.
