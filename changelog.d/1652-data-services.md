### Added

- `Client.QRDBData()` in the Go SDK re-exports go-qrdb's KV, batch, transaction, MVCC, collection and index clients, including the guarded writes (compare-and-swap, compare-and-delete, put-if-absent). On the embedded node, KV, batch, transaction, MVCC and index calls resolve through the same per-database registry as the gRPC and Unix data RPCs, so an embedded write and a network read of the same key meet in one keyspace (#1652).
