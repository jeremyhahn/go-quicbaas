### Added

- **One RAG service on every transport (go-quicbaas#1611).** REST, MCP, gRPC
  and the embedded transport call one `pkg/rag/ragservice`, so they return
  the same results and error codes. The node now serves the gRPC
  `quicbaas.rag.v1.RAGService`, and in `rag` builds the embedded transport
  serves `quicbaas.RAG/Query` and `quicbaas.RAG/BuildContext`.
- The Go SDK re-exports go-qrdb's RAG service as `QRDBRAGClient` through
  `quicbaas.QRDBRAG(client)` on every transport, carrying the platform
  credentials (go-quicbaas#1672).

### Fixed

- Hybrid ranking now weights each list by `VectorWeight` and `KeywordWeight`.
  Before, both lists counted equally. Results with equal scores now come
  back in ID order every time.
- Context assembly sets `Truncated` when it drops results past
  `MaxResultsPerChunk`.
- The Go SDK's gRPC `RAG().Query` and `RAG().BuildContext` call the node's
  `quicbaas.rag.v1.RAGService` with its protobuf messages. Before, they sent
  JSON to `/quicbaas.RAG/*`, a service the node does not serve.
- An explicit `min_score` of 0 turns the similarity filter off again on
  every transport. Only an omitted `min_score` defaults to 0.7.
- `make test-rag` passes the build tags, so the rag-tagged watcher, REST,
  MCP, gRPC and embedded RAG tests run (go-quicbaas#2067).
- The node's go-qrdb RAG service removes a document's vectors as well as
  its chunks on delete, and reports a storage failure instead of success.
  Re-ingesting a document as fewer chunks removes the old ones, so a query
  no longer returns replaced text.
- The Go SDK's REST and QUIC `QRDBRAGClient.RAGDeleteDocument` sends the
  document id and namespace as query parameters, where go-qrdb reads them.
- Over MCP, `QRDBRAGClient.RAGIngestDocument` refuses a `ChunkMinSize`,
  which go-qrdb's MCP tool cannot carry, instead of dropping it.
