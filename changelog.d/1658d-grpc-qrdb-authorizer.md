### Security

- **gRPC and Unix-socket requests are decided by go-qrdb's authorizer** (#1658).
  The token's scopes still cap what a caller may do, and go-qrdb's RBAC and
  ABAC policies now decide each call as they do on REST, QUIC and MCP, so a
  role grant revoked in go-qrdb takes effect before the token expires.
