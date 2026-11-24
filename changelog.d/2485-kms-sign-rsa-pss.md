### Added

- KMS Sign takes an optional RSA-PSS padding: `padding` (`PKCS1V15` or `PSS`), `pss_salt_length` (0 means the hash length) and `mgf1_hash` (equal to the signing hash), over gRPC, REST, MCP, the CLI (`--padding`, `--pss-salt-length`), the Go SDK (a `crypto.Signer` given `*rsa.PSSOptions` now returns a PSS signature) and the TypeScript SDK; RSA keys only, and the default stays PKCS#1 v1.5 (#2485).
