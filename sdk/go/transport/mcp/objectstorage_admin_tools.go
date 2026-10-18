// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

// objectStorageAdminToolNames holds the tool names pkg/api/mcp registers
// for the ObjectStorageAdmin verbs (objectstorage_admin_tools.go).
var objectStorageAdminToolNames = map[string]string{
	"CreateBackend":                "quicbaas_object_admin_create_backend",
	"UpdateBackend":                "quicbaas_object_admin_update_backend",
	"DeleteBackend":                "quicbaas_object_admin_delete_backend",
	"SetBackendStatus":             "quicbaas_object_admin_set_backend_status",
	"GetStorageStats":              "quicbaas_object_admin_get_storage_stats",
	"RunGarbageCollection":         "quicbaas_object_admin_run_gc",
	"GetGarbageCollectionProgress": "quicbaas_object_admin_get_gc_progress",
	"RepairObject":                 "quicbaas_object_admin_repair_object",
	"CompactBackend":               "quicbaas_object_admin_compact_backend",
}

// objectStorageAdminToolName returns the server's tool name for method; a
// verb the server does not register keeps the admin prefix so the server
// answers it as an unknown tool.
func objectStorageAdminToolName(method string) string {
	if name, ok := objectStorageAdminToolNames[method]; ok {
		return name
	}
	return "quicbaas_object_admin_" + camelToSnake(method)
}
