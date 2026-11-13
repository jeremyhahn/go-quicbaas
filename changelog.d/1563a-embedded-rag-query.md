### Fixed

- The embedded transport routes `quicbaas.Query/Execute` through the same
  `QuicBaaS.Query` call the REST and gRPC Query handlers make. The query service
  matches one index value by equality, without a cursor, so another operator, a
  cursor, or a value that is not a string or bytes is refused with a typed
  `UnsupportedQueryFieldError` (#1563).
