### Added

- **`Security().Auth().UserByCertificate` and `UserByCertThumbprint` are
  proven on every transport (go-quicbaas#376).** One case table (found,
  unbound, another tenant's user, caller with no tenant) runs on the
  embedded, gRPC and Unix transports in the unit tier, and on REST, gRPC,
  QUIC, Unix and MCP against a live daemon in the integration tier. An
  unbound certificate or another tenant's user is the typed `NotFound` on
  each.
