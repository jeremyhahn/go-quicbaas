### Fixed

- KMS GenerateKey, RotateKey and DeleteKey over the SDK MCP transport now call `quicbaas_kms_generate_key`, `quicbaas_kms_rotate_key` and `quicbaas_kms_delete_key` and return the KMS client's typed `GenerateKeyResponse` and `RotateKeyResponse` (DeleteKey returns nil) instead of failing with "invalid response type". A tool result without its `key_id`, or a RotateKey result without a non-zero `new_version`, is a typed Internal error; a request without a key ID, or a GenerateKey request whose algorithm or usage has no tool name, is refused with InvalidInput before any call (#1621).
