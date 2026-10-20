### Fixed

- The MCP ceremony `qrdb_ceremony_enroll_external_officer` tool now carries `share_bytes` base64-encoded, as its schema documents, so a binary Shamir share reaches the service unchanged instead of having its non-UTF-8 bytes replaced by U+FFFD; the SDK MCP transport encodes the share, the server decodes it, refuses invalid base64 with the typed invalid-input `CeremonyFieldInvalidError` (MCP code invalid input) before the service call, and wipes the decoded share afterwards (#1635).
