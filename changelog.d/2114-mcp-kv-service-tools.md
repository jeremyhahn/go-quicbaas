### Fixed

- The Go SDK's MCP KV Put, Get and Delete now reach quicbaas's KV service through the new `quicbaas_kv_put`, `quicbaas_kv_get` and `quicbaas_kv_delete` tools, so an entity written over MCP has the same canonical, tenant-scoped key REST and gRPC use, the request's shard reaches the service, ids above 2^53 stay exact (every uint64 is a decimal string), and a service failure is an error rather than a miss. The old raw-key path through go-qrdb's `qrdb_put`/`qrdb_get`/`qrdb_delete` is gone (#2114).
