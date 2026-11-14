### Fixed

- KMS `ImportKey` and `UnwrapKey` pass the request's `Exportable` flag to
  go-xkms, so an imported or unwrapped key requested as exportable can be
  exported, and one that is not is refused with go-xkms's typed
  `ErrBackendKeyNotExportable`. Before, the flag reached only the quicbaas
  metadata and go-xkms recorded every imported key as not exportable. The
  REST, gRPC and MCP handlers that import through `KMSManager.ImportKey`
  pass the flag too (#1779).
