### Fixed

- MCP: the `xkms.ca.*` tools (`getBundle`, `getCert`, `signCSR`, `issueCert`, `revokeCert`, `generateCRL`, `isRevoked`) serve the server's go-xkms CA, the same adapter as the REST `/api/v1/xkms/ca/*` routes, instead of always answering "operation not supported" (#1330).
