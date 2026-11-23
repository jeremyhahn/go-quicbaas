### Fixed

- MCP: each tool registry dispatches the `xkms.*` tools to its own services. The dispatch table was shared process-wide and bound to the first registry that used it, so a second server or tenant registry in the same process reached the first one's CA and other services, and the first fill was not safe under concurrent calls (#1692).
