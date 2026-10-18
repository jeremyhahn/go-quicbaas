### Fixed

- The SDK's ObjectStorageAdmin client reaches the served routes (#1551): the embedded transport routes all nine verbs to the ObjectStorageAdmin service, the REST transport calls the `/api/v1/admin/object-backends`, `/api/v1/admin/storage` and `/api/v1/admin/objects/repair` routes with typed replies, and the MCP transport uses the server's `quicbaas_object_admin_*` tool names.
