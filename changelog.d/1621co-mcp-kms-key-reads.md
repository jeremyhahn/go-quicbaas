### Fixed

- KMS GetKey, ListKeys and GetPublicKey over the SDK MCP transport now return the KMS client's typed `KeyInfo`, `ListKeysResponse` and `GetPublicKeyResponse` instead of failing with "invalid response type". GetPublicKey requires a valid base64 `public_key` in the tool result and returns a typed Internal error when it is missing, empty or malformed (#1621).
