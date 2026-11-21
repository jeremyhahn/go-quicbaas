### Added

- The embedded SDK transport serves `Security().TrustedCA()`: the re-exported
  go-xkms `CAService` reaches the same go-xkms CA the REST
  `/api/v1/xkms/ca/*` routes and the MCP `xkms.ca.*` tools serve (#1330).
