### Fixed

- The SDK MCP transport now calls `quicbaas_kms_key_versions` for KMS ListKeyVersions and `quicbaas_kms_key_status_update` for UpdateKeyStatus. It sends the status as `"enabled"` or `"disabled"` and refuses any other status with a typed InvalidInput error before the call. ListKeyVersions, UpdateKeyStatus, GetKeyMetadata, GetSupportedAlgorithms and BackendStatus now return the KMS client's typed responses instead of failing with "invalid response type" (#1621).
