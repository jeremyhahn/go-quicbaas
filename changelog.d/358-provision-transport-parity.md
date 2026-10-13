### Fixed

- **The MCP transport refuses an IAM request it cannot encode
  (go-quicbaas#358).** An IAM request that could not be encoded as JSON used
  to reach the tool with no arguments, and the call returned nothing and no
  error. It now fails as invalid input before any I/O. Tenant provisioning is
  tested in `cert` and `invite` mode on the REST, QUIC (over a real HTTP/3
  listener), MCP and Unix SDK transports.
