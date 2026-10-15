### Added

- **gRPC interop leg (go-quicbaas#203).** `pkg/api/grpc/interop_connect_test.go`
  serves the gRPC facade behind the production interceptor chain, authorization
  included, over TLS on loopback, and drives it with `connectrpc.com/connect`
  in gRPC mode, a wire implementation that shares no code with grpc-go. It
  asserts TLS 1.3 with `h2`, an authenticated call, Unauthenticated and
  PermissionDenied refusals, Unimplemented for unknown methods, and an OK
  trailer at the end of a server stream. The gRPC row of the interop claims
  audit now names these tests.
