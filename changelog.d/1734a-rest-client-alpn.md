### Fixed

- The Go SDK REST and MCP transports now offer only `http/1.1` through ALPN, on a clone of the caller's TLS config, and the go-qrdb sub-clients get a clone too. Before, the HTTP/2 transport of the go-qrdb sub-client added `h2` to the shared config in place, the SDK REST transport then offered `h2` it cannot speak, and a server that speaks HTTP/2 answered with a SETTINGS frame the client reported as a malformed HTTP/1.x response (#1734).
