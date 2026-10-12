### Fixed

- **The TypeScript SDK's REST client no longer rounds 64-bit ids.** Every
  REST response body is parsed with `parseJSONPreservingLargeIntegers`, which
  now decides by member name: an integer past 2^53 under `id`, `ids`, `*_id`,
  `*_ids`, `*Id`, `*Ids`, `*ID` or `*IDs` arrives as its exact decimal
  string, and an integer under any other key (a `time.UnixNano` timestamp, a
  size, a counter) stays a `number`. A bare top-level integer is kept exact.
  `RAGDocument.id` (new `RAGDocumentID`) and `PlacementNodeInfo.lease_id` are
  now `number | string`, and the RAG document methods accept either; before
  this, deleting or reindexing a RAG document whose hashed id exceeded 2^53
  addressed an id the server never issued. The rule is documented in
  `docs/multiprotocol-api/api.md`.
