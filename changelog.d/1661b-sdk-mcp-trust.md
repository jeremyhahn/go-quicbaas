### Fixed

- Go SDK MCP transport: `Security().Trust()` AutoImportAIA and the cross-signed root pool now work over MCP, sending the PEM bundle the server's trust tool takes and decoding its PEM roots (#1661).
