### Security

- **Tenant DEK calls over MCP no longer send key-protected data or the bearer in plaintext or to a redirect target (#1653).** `Security().TenantDEK()` over MCP refuses every keyring verb on an `http://` address before the session starts, holds every hop to https and the configured host, and follows no redirect for encrypt, decrypt and rewrap. A refusal is a non-retryable `CodeForbidden` error wrapping `*mcp.DEKHopError`.
