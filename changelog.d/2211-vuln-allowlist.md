### Changed

- `make vuln` fails on a reachable govulncheck advisory unless `scripts/vuln-allowlist.txt` lists its ID with a reason, prints each allowed advisory with its reason, and fails an entry that matches no reachable finding as stale (#2211).
- The list's one entry is GO-2026-6911, the mcp-go v0.45.0 SSRF and credential leak via an unvalidated Location header, which has no fixed release; it is removed when mcp-go ships a fix (#2211).
