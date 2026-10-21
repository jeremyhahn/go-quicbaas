### Fixed

- The Go SDK serves the nine `quicbaas.ObjectStorageAdmin` verbs (CreateBackend, UpdateBackend, DeleteBackend, SetBackendStatus, GetStorageStats, RunGarbageCollection, GetGarbageCollectionProgress, RepairObject and CompactBackend) over the gRPC transport, which used to refuse them as unsupported. The QUIC transport serves them through its REST-over-HTTP/3 routes and the Unix transport through the gRPC handler over the socket. Tests on each transport check every request and response field, the typed errors for a server error, and the refusal of an unknown verb (#1551).
