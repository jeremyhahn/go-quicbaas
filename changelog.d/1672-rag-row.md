### Added

- **go-qrdb's RAG client in `sdk/go` (go-quicbaas#1672).** `quicbaas.QRDBRAG(c)`
  returns go-qrdb's `RAGClient` as `QRDBRAGClient`, with its request and
  response types as `QRDBRAG*` aliases, in `qrdb_vector` builds. It calls
  go-qrdb's `/v1/rag/*` routes, which a QuicBaaS node does not serve, so
  point it at a go-qrdb node. `Client.RAG()` is still QuicBaaS's own pipeline.
- **One RAG service on every transport (go-quicbaas#1611).** REST, MCP, gRPC
  and the embedded transport call one `pkg/rag/ragservice`, so they return
  the same results and error codes. The node now serves the gRPC
  `quicbaas.rag.v1.RAGService`, and in `rag` builds the embedded transport
  serves `quicbaas.RAG/Query` and `quicbaas.RAG/BuildContext`.

### Fixed

- Hybrid ranking now weights each list by `VectorWeight` and `KeywordWeight`.
  Before, both lists counted equally. Results with equal scores now come
  back in ID order every time.
- Context assembly sets `Truncated` when it drops results past
  `MaxResultsPerChunk`.
- `make test-rag` passes the build tags, so the rag-tagged watcher, REST,
  MCP, gRPC and embedded RAG tests run (go-quicbaas#2067).
