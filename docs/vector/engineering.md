# Vector — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract for the vector service surface. Feature folder:
> `docs/vector/`.

## Ownership: this repository owns the service, not the index

The vector **primitive** — storage and the index — belongs to go-qrdb and is
consumed through `go-qrdb/sdk/go`. What lives here is the tenanted, API-fronted
service surface over it, plus the local support packages
`pkg/vector/autoindex` and `pkg/vector/filter`.

The rule that follows from that, and the one most likely to be broken by a
well-meaning change: **never build an in-memory index in this repository.** A
local index would be a second implementation of a capability another project
owns, and it would silently diverge from the persisted one.

## Exported surface

`quicbaas.Vector` (`sdk/go/client/vector`), 22 methods:

| Group | Methods |
|---|---|
| Namespaces | `CreateNamespace`, `DeleteNamespace`, `ListNamespaces`, `GetNamespaceInfo` |
| Records | `Upsert`, `BatchUpsert`, `Get`, `Delete`, `BatchDelete`, `List`, `UpdateMetadata` |
| Search | `SearchSimilar`, `SearchByID`, `SearchByMetadata`, `HybridSearch` |
| Index | `RebuildIndex`, `GetIndexProgress`, `Compact`, `GetCompactionProgress` |
| Bulk | `Import`, `Export` |
| Stats | `GetStats` |

`api.md` maps every one to its REST route, permission and transport support.

## Index backends — what is actually implemented

**`auto` and `hnsw` both resolve to in-memory brute-force search today.** There
is no HNSW graph. A namespace created with `--index hnsw` will answer queries
correctly and will do so by scanning, which means recall is perfect and latency
is linear in the namespace size.

The GPU CAGRA backend returns `GPUUnavailableError`; it is a stub.

This is stated plainly because the alternative — documenting HNSW as though it
were built — produces capacity plans that are wrong by orders of magnitude at
exactly the size where it matters. Sizing must assume a scan.

## Invariants

- **Dimension is fixed at namespace creation.** Upserting a vector of a
  different dimension is an error, not a coercion. Getting this wrong is the
  most common integration failure, and it usually traces to an embedding
  provider change (`../embeddings/`).
- **Namespace deletion is not reversible** and drops every record in it.
- **Metadata updates do not re-embed.** `UpdateMetadata` changes the payload
  beside the vector; the vector is unchanged.
- **Batch operations are not transactional across the batch** unless the
  underlying qrdb call is. Do not assume all-or-nothing.

## Progress-reporting operations

`RebuildIndex` and `Compact` are asynchronous and paired with
`GetIndexProgress` and `GetCompactionProgress`. The same rule applies as
everywhere else in this codebase: the start call returns acceptance, not
completion. Automation must poll.

## Errors

- Dimension mismatch on upsert: rejected, with the expected and supplied
  dimensions.
- Unknown namespace: not-found, distinct from an empty namespace.
- GPU backend requested: `GPUUnavailableError`. This is a stub, not a
  misconfiguration, and no configuration change fixes it.

## Configuration

`pkg/config/vector_config.go` carries `cache_enabled`, `cache_ttl`,
`cache_max_size` and `filter_cache_size`. Config-file only — no vector key
appears in the explicit environment-variable list `pkg/config/loader.go` reads.

RAG's own vector settings are separate (`pkg/config/rag_config.go`:
`num_centroids`, `dimensions`, `nprobe`, `metric`) and belong to
[`../rag/`](../rag/engineering.md). Two blocks, two features; do not tune one
expecting the other to change.

## Test strategy

- Unit tests cover namespace lifecycle, dimension validation and the filter
  package.
- **A performance test written against the documented HNSW behaviour will
  mislead.** Benchmark against a scan, because that is what runs.
- Integration coverage belongs with the multiprotocol parity suite; every verb
  in `cli.md` needs a `CommandDefinition`.

## Observability

`GetStats` reports namespace-level counts. Vector health has its own route,
`GET /api/v1/vector/health`, separate from the process health endpoint so a
degraded vector subsystem does not take the whole server out of a load-balancer
pool. See [`../observability/`](../observability/architecture.md).
