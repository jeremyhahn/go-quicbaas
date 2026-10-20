### Fixed

- The MCP `quicbaas_kms_import_key` tool now base64-decodes `key_material`, as its schema documents, before importing it through the KMS service or the KMS manager fallback. It used to store the base64 text itself as the key. A missing, empty or invalid-base64 `key_material` is refused with a typed InvalidInput error before any KMS call (#1634).
