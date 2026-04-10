# Multi-Tenant Database Architecture (2026-03-14)

## Status: IN DESIGN

## Core Concept
Each tenant creates their own databases (KV, vector, timeseries). No more global shared shards.
The qrdb SDK's DatabaseClient interface (9 methods) is fully implemented instead of stubbed.

## Shard Scheme
- Shard 1: _system (IAM, KMS, secrets, scheduler, params) — single shard, key-prefix isolation
- Shards 2-9: reserved, unused
- Shards 10000+: dynamically allocated to tenant databases via CAS-based allocator

## Key Packages
- pkg/shards/ — shard ID constants, registry, access guard
- pkg/database/ — DatabaseRecord, DatabaseRegistry, ShardAllocator, DatabaseRouter, DatabaseHandle
- pkg/service/database.go — DatabaseService (9 methods: CRUD + 5 Update*)
- pkg/server/baas_transport_database.go — transport layer implementation

## Encryption Hierarchy
Master Key → Tenant KEK (kms/tenant/{t}/kek) → Database DEK (kms/tenant/{t}/db/{d}/dek)
- Per-database DEK via extended DEKID format: dek:tenant:{t}:db:{d}
- Per-database barrier via DatabaseBarrierManager extension to SealManager

## System Shard Key Layout
__sys:db:registry:{tenantID}:{dbName} → DatabaseRecord JSON
__sys:db:shard_counter → next shard ID (starts 10000)
__sys:db:shard_index:{shardID} → reverse lookup
__sys:db:tenant_count:{tenantID} → quota enforcement

## What Already Works (reuse)
- KMS tenant isolation: pkg/kms/storage_adapter.go
- Per-tenant DEKs: pkg/secrets/dekid.go, pkg/secrets/dek.go
- DEK cluster sync: pkg/secrets/cluster.go
- KeyspaceResolver: pkg/service/keyspace.go
- IAM tenant context: pkg/iam/tenant_context.go
