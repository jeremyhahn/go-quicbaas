### Fixed

- KMS ImportKey, RevokeKey and UpdateKeyMetadata over the SDK MCP transport now call `quicbaas_kms_import_key`, `quicbaas_kms_revoke_key` and `quicbaas_kms_update_metadata` with every field the tool accepts, and return the KMS client's typed `ImportKeyResponse` and `KeyMetadataResponse` instead of a raw map. Key material is sent base64-encoded. A request with no key ID, or an import with no name or key material, is refused with InvalidInput before any call; a tool result with no `key_id` is a typed Internal error (#1621).
