### Fixed

- **The SDK gRPC transport serves Query Execute (go-quicbaas#1612).**
  `client/query` Execute maps onto the served Query stream RPC and collects
  every batch. The RPC matches one exact index value, so another operator, a
  Limit or a Cursor returns a typed unimplemented error instead of being
  dropped.
