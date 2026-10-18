### Fixed

- **The TypeScript SDK's MCP transport serves barrier and ceremony.**
  `McpBarrierClient` and `McpCeremonyClient` call the server's
  `qrdb_barrier_*` and `qrdb_ceremony_*` MCP tools, with the tool names and
  argument keys of the Go SDK's MCP transport. A tenant `enableEncryption`
  calls `qrdb_tenant_barrier_enable_encryption`. Requests that carry an SO PIN
  refuse a plaintext `http://` endpoint with `SOPINRequiresTLSError`, and a
  tool error reply surfaces as `McpError`. The MCP tools return no share or
  recovery-key material, so `initializeShamir`, `rekey` and
  `generateRecoveryKeys` return empty `shares` / `keys` over MCP.
- `ceremony.claimShare` still throws `McpNotImplementedError`: its tools need
  an officer signature that its request cannot carry.
