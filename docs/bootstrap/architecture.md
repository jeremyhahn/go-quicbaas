# Bootstrap — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/bootstrap/`.

Architectural migration from monolithic database engine to Backend-as-a-Service via the QRDB SDK.

## Overview

QuicBaaS migrated from a monolithic architecture -- where Raft consensus, PebbleDB storage, vector search, and MVCC were implemented locally -- to a BaaS architecture where all database operations route through the QRDB SDK. This reduces code duplication, centralizes storage logic in QRDB, and lets QuicBaaS focus on its BaaS capabilities (IAM, secrets, RAG, API gateway).

## Before and After

```
BEFORE (Monolithic):                    AFTER (BaaS):

QuicBaaS                               QuicBaaS
  |-- pkg/raft          (local)           |-- pkg/service     (business logic)
  |-- pkg/statemachine  (local)           |-- pkg/server      (transport factory)
  |-- pkg/storage/mvcc  (local)           |-- sdk/go          (QuicBaaS SDK)
  |-- pkg/storage/index (local)           +-- go-qrdb/sdk/go  (QRDB SDK)
  |-- pkg/storage/pebble(local)
  |-- pkg/vector        (local)
  |-- pkg/memory        (local)
  |-- pkg/operations    (local)
  |-- pkg/shard         (local)
  |-- pkg/retention     (local)
  +-- pkg/service       (business logic)
```

## Deleted Packages

The following local packages were removed and replaced by QRDB SDK operations:

| Deleted Package | Replacement |
|-----------------|-------------|
| `pkg/raft` | `go-qrdb/sdk/go` cluster and consensus operations |
| `pkg/statemachine` | `go-qrdb/sdk/go` state machine operations |
| `pkg/storage/mvcc` | `go-qrdb/sdk/go` MVCC operations |
| `pkg/storage/index` | `go-qrdb/sdk/go` index operations |
| `pkg/storage/watch` | `go-qrdb/sdk/go` watch streaming |
| `pkg/storage/partition` | `go-qrdb/sdk/go` shard routing |
| `pkg/storage/pebble` | `go-qrdb/sdk/go` storage backend |
| `pkg/storage/vector` | `go-qrdb/sdk/go` vector search |
| `pkg/vector` | `go-qrdb/sdk/go` vector operations |
| `pkg/memory` | `go-qrdb/sdk/go` in-memory operations |
| `pkg/operations` | `go-qrdb/sdk/go` batch and admin operations |
| `pkg/shard` | `go-qrdb/sdk/go` shard management |
| `pkg/retention` | `go-qrdb/sdk/go` retention policies |

## Import Policy

**Only allowed QRDB import:** `github.com/jeremyhahn/go-qrdb/sdk/go/**`

No direct imports from `go-quicraft` or `go-qrdb/pkg/**` are permitted in production or test code. The single exception is `pkg/seal/barrier_qcseal.go`, which wraps the go-quicraft cryptographic Barrier behind a local interface.

## QRDB Client

All QRDB SDK communication goes to the embedded go-qrdb node. go-qrdb under go-quicbaas is embedded only: the server's `ShardedClient` wraps the node's own SDK client (`node.Client()`), and every shard ID resolves to that client. go-quicbaas never connects to a separate go-qrdb daemon.

## Data Flow

```
QuicBaaS Service Layer
        |
        v
  ShardedClient (every shard ID -> the node client)
        |
        v
  Embedded qrdb node: in-process dispatch, no serialization
```

The node routes shards and Raft proposals itself.

## Key Architecture Decisions

1. **SDK-only access** -- All database operations go through the QRDB SDK, ensuring a clean separation between BaaS features and the database engine.

2. **Embedded only** -- go-qrdb runs in the `quicbaasd` process; there is no network transport between go-quicbaas and go-qrdb.

3. **Local interfaces** -- `pkg/interfaces/raft.go`, `pkg/interfaces/timeseries.go`, and `pkg/interfaces/statemachine.go` define local type aliases so QuicBaaS does not import QRDB internal types.

4. **Mock infrastructure** -- `pkg/testutil/MockQuicBaaS` with `svcBoostMockQRDBProvider` provides test coverage without requiring a running QRDB instance.

5. **Seal adapter exception** -- `pkg/seal/barrier_qcseal.go` is the only file that imports go-quicraft directly, wrapping the cryptographic Barrier behind a local interface.
