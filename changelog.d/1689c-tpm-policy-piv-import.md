### Added

- **`Security().TPMPolicy()` returns the go-xkms TPM-policy client
  (go-quicbaas#1689).** Named TPM policies, their approvals and the objects
  bound to them are reached through the quicbaas SDK on REST, QUIC, gRPC,
  Unix, MCP and embedded, in go-xkms's own types. On MCP and embedded every
  verb returns a typed Unimplemented error naming the method, because the
  MCP server registers no TPM-policy tools and the embedded node has no
  TPM-policy service.
- **sdk/go/xkms re-exports the go-xkms PIV key import (go-quicbaas#1689).**
  `ImportPIVKey`, its request and response types, `PIVKeyImporter` and
  `ErrPIVImportNotSupported` are available beside the other PIV verbs.
