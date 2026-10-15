### Changed

- **The Go SDK's test files carry no `//nolint` directives
  (go-quicbaas#559).** Nil-context guard tests pass a nil `context.Context`
  variable rather than the `nil` literal staticcheck flags, the QUIC
  transport tests verify the loopback server certificate instead of
  skipping verification, and the gRPC cost-settings test checks its type
  assertion.
