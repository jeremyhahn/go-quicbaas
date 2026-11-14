### Added

- The MCP server serves go-xkms's `TCGCAService` as the `xkms.ca.tcg.*` tools
  (`issueEKCert`, `issueAKCert`, `signCSR`, `enroll`; certs:generate), backed
  by the same go-xkms CA as `xkms.ca.*`. The SDK's MCP transport sends
  `Security().TCGCA()` to them (#1330).
