### Fixed

- **The integration suites now authenticate their data-plane calls
  (go-quicbaas#1317).** The api/grpc, api/rest batch and shard-header, and
  batch suites dialled with server trust only and sent no bearer. The server
  correctly refused every entity and batch call as unauthenticated within
  milliseconds. The suites now act as a provisioned tenant user whose only
  role holds `kv:get`, `kv:put`, `kv:delete`, `kv:list`, `kv:range` and
  `batch:execute`, carried as a bearer on gRPC and REST. No server
  authorization check changed. A unit test in `pkg/api/grpc` pins that role
  to the interceptor's method mapping: it must admit every data-plane RPC and
  still be denied an admin RPC.
