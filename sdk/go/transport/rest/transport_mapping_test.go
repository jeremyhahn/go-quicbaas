// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"net/http"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newMappingTransport builds a transport suitable for exercising the pure
// service/method -> endpoint mapping functions. No network is required.
func newMappingTransport(t *testing.T) *Transport {
	t.Helper()
	tr, err := New(&transport.Config{
		Address: "localhost:8080",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error constructing transport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// TestMapToEndpointAllServices exercises every mapXxxMethod branch reachable
// through mapToEndpoint, including the per-service default fallbacks. These
// functions are pure, so we assert the returned (path, httpMethod) pair.
func TestMapToEndpointAllServices(t *testing.T) {
	tr := newMappingTransport(t)

	cases := []struct {
		name           string
		service        string
		method         string
		expectedPath   string
		expectedMethod string
	}{
		// Batch service
		{"Batch/Put", "Batch", "Put", "/api/v1/batch/put", http.MethodPost},
		{"Batch/Get", "Batch", "Get", "/api/v1/batch/get", http.MethodPost},
		{"Batch/Delete", "Batch", "Delete", "/api/v1/batch/delete", http.MethodPost},
		{"Batch/default", "Batch", "Flush", "/api/v1/batch/flush", http.MethodPost},

		// Transaction service
		{"Transaction/Execute", "Transaction", "Execute", "/api/v1/transaction/execute", http.MethodPost},
		{"Transaction/Begin", "Transaction", "Begin", "/api/v1/transaction/begin", http.MethodPost},
		{"Transaction/Commit", "Transaction", "Commit", "/api/v1/transaction/commit", http.MethodPost},
		{"Transaction/Rollback", "Transaction", "Rollback", "/api/v1/transaction/rollback", http.MethodPost},
		{"Transaction/default", "Transaction", "Status", "/api/v1/transaction/status", http.MethodPost},

		// Query service
		{"Query/Query", "Query", "Query", "/api/v1/query", http.MethodPost},
		{"Query/ScanIndex", "Query", "ScanIndex", "/api/v1/query/scan", http.MethodPost},
		{"Query/Range", "Query", "Range", "/api/v1/query/range", http.MethodPost},
		{"Query/default", "Query", "Count", "/api/v1/query/count", http.MethodPost},

		// Cluster extra branches not covered elsewhere
		{"Cluster/CreateShard", "Cluster", "CreateShard", "/api/v1/cluster/shards", http.MethodPost},
		{"Cluster/Rebalance", "Cluster", "Rebalance", "/api/v1/cluster/rebalance", http.MethodPost},
		{"Cluster/GetRebalanceStatus", "Cluster", "GetRebalanceStatus", "/api/v1/cluster/rebalance/status", http.MethodGet},
		{"Cluster/GetRebalanceProgress", "Cluster", "GetRebalanceProgress", "/api/v1/cluster/rebalance/status", http.MethodGet},

		// KMS branches not in the existing mapping table
		{"KMS/ExportKey", "KMS", "ExportKey", "/api/v1/kms/keys/{name}/export", http.MethodPost},
		{"KMS/RevokeKey", "KMS", "RevokeKey", "/api/v1/kms/keys/{name}/revoke", http.MethodPost},
		{"KMS/UnwrapKey", "KMS", "UnwrapKey", "/api/v1/kms/keys/{name}/unwrap", http.MethodPost},
		{"KMS/GetKeyMetadata", "KMS", "GetKeyMetadata", "/api/v1/kms/keys/{name}/metadata", http.MethodGet},
		{"KMS/UpdateKeyMetadata", "KMS", "UpdateKeyMetadata", "/api/v1/kms/keys/{name}/metadata", http.MethodPut},
		{"KMS/UpdateKeyStatus", "KMS", "UpdateKeyStatus", "/api/v1/kms/keys/{name}/status", http.MethodPut},
		{"KMS/ListKeyVersions", "KMS", "ListKeyVersions", "/api/v1/kms/keys/{name}/versions", http.MethodGet},
		{"KMS/SetKeyPolicy", "KMS", "SetKeyPolicy", "/api/v1/kms/keys/{name}/policy", http.MethodPut},
		{"KMS/GetKeyPolicy", "KMS", "GetKeyPolicy", "/api/v1/kms/keys/{name}/policy", http.MethodGet},
		{"KMS/SetKeySafety", "KMS", "SetKeySafety", "/api/v1/kms/keys/{name}/safety", http.MethodPut},
		{"KMS/ClearKeySafety", "KMS", "ClearKeySafety", "/api/v1/kms/keys/{name}/safety", http.MethodDelete},
		{"KMS/SafetyCheck", "KMS", "SafetyCheck", "/api/v1/kms/safety/check", http.MethodPost},
		{"KMS/SafetyReport", "KMS", "SafetyReport", "/api/v1/kms/safety/report", http.MethodGet},
		{"KMS/GetSupportedAlgorithms", "KMS", "GetSupportedAlgorithms", "/api/v1/kms/algorithms", http.MethodGet},
		{"KMS/BackendStatus", "KMS", "BackendStatus", "/api/v1/kms/backend/status", http.MethodGet},
		{"KMS/BackendTest", "KMS", "BackendTest", "/api/v1/kms/backend/test", http.MethodPost},
		{"KMS/CreateDEK", "KMS", "CreateDEK", "/api/v1/kms/deks", http.MethodPost},
		{"KMS/GetDEK", "KMS", "GetDEK", "/api/v1/kms/deks/{name}", http.MethodGet},
		{"KMS/ListDEKs", "KMS", "ListDEKs", "/api/v1/kms/deks", http.MethodGet},
		{"KMS/RotateDEK", "KMS", "RotateDEK", "/api/v1/kms/deks/{name}/rotate", http.MethodPost},
		{"KMS/DeleteDEK", "KMS", "DeleteDEK", "/api/v1/kms/deks/{name}", http.MethodDelete},
		{"KMS/GetDEKStatus", "KMS", "GetDEKStatus", "/api/v1/kms/deks/{name}/status", http.MethodGet},
		{"KMS/SetDefaultDEK", "KMS", "SetDefaultDEK", "/api/v1/kms/deks/{name}/default", http.MethodPost},
		{"KMS/SetDEKSafety", "KMS", "SetDEKSafety", "/api/v1/kms/deks/{name}/safety", http.MethodPut},
		{"KMS/ClearDEKSafety", "KMS", "ClearDEKSafety", "/api/v1/kms/deks/{name}/safety", http.MethodDelete},
		{"KMS/StartReencryption", "KMS", "StartReencryption", "/api/v1/kms/reencryption/start", http.MethodPost},
		{"KMS/GetReencryptionProgress", "KMS", "GetReencryptionProgress", "/api/v1/kms/reencryption/progress", http.MethodGet},

		// Secrets default branch
		{"Secrets/default", "Secrets", "Audit", "/api/v1/secrets/audit", http.MethodPost},

		// Crypto default branch
		{"Crypto/default", "Crypto", "Rewrap", "/api/v1/crypto/rewrap", http.MethodPost},

		// Timeseries extra branches
		{"Timeseries/WritePoint", "Timeseries", "WritePoint", "/api/v1/timeseries/write", http.MethodPost},
		{"Timeseries/WriteBatch", "Timeseries", "WriteBatch", "/api/v1/timeseries/write/batch", http.MethodPost},
		{"Timeseries/DeleteSeries", "Timeseries", "DeleteSeries", "/api/v1/timeseries/delete", http.MethodPost},
		{"Timeseries/Aggregate", "Timeseries", "Aggregate", "/api/v1/timeseries/aggregate", http.MethodPost},
		{"Timeseries/GetRetentionPolicy", "Timeseries", "GetRetentionPolicy", "/api/v1/timeseries/retention", http.MethodGet},
		{"Timeseries/SetRetentionPolicy", "Timeseries", "SetRetentionPolicy", "/api/v1/timeseries/retention", http.MethodPut},
		{"Timeseries/ListMetrics", "Timeseries", "ListMetrics", "/api/v1/timeseries/metrics", http.MethodGet},
		{"TimeSeries/alias", "TimeSeries", "Query", "/api/v1/timeseries/query", http.MethodPost},

		// RBAC service
		{"RBAC/CreateRole", "RBAC", "CreateRole", "/api/v1/rbac/roles", http.MethodPost},
		{"RBAC/GetRole", "RBAC", "GetRole", "/api/v1/rbac/roles/{name}", http.MethodGet},
		{"RBAC/UpdateRole", "RBAC", "UpdateRole", "/api/v1/rbac/roles/{name}", http.MethodPut},
		{"RBAC/DeleteRole", "RBAC", "DeleteRole", "/api/v1/rbac/roles/{name}", http.MethodDelete},
		{"RBAC/ListRoles", "RBAC", "ListRoles", "/api/v1/rbac/roles", http.MethodGet},
		{"RBAC/AssignRole", "RBAC", "AssignRole", "/api/v1/rbac/users/{username}/roles", http.MethodPost},
		{"RBAC/RevokeRole", "RBAC", "RevokeRole", "/api/v1/rbac/users/{username}/roles/{role}", http.MethodDelete},
		{"RBAC/GetUserRoles", "RBAC", "GetUserRoles", "/api/v1/rbac/users/{username}/roles", http.MethodGet},
		{"RBAC/CheckPermission", "RBAC", "CheckPermission", "/api/v1/rbac/permissions/check", http.MethodPost},
		{"RBAC/ListPermissions", "RBAC", "ListPermissions", "/api/v1/rbac/permissions", http.MethodGet},
		{"RBAC/default", "RBAC", "Sync", "/api/v1/rbac/sync", http.MethodPost},

		// Backup service
		{"Backup/Create", "Backup", "Create", "/api/v1/backups", http.MethodPost},
		{"Backup/List", "Backup", "List", "/api/v1/backups", http.MethodGet},
		{"Backup/Get", "Backup", "Get", "/api/v1/backups/{id}", http.MethodGet},
		{"Backup/Restore", "Backup", "Restore", "/api/v1/backups/{id}/restore", http.MethodPost},
		{"Backup/Delete", "Backup", "Delete", "/api/v1/backups/{id}", http.MethodDelete},
		{"Backup/Verify", "Backup", "Verify", "/api/v1/backups/{id}/verify", http.MethodPost},
		{"Backup/Status", "Backup", "Status", "/api/v1/backups/{id}/status", http.MethodGet},
		{"Backup/Schedule", "Backup", "Schedule", "/api/v1/backups/schedule", http.MethodPost},
		{"Backup/ListSchedules", "Backup", "ListSchedules", "/api/v1/backups/schedules", http.MethodGet},
		{"Backup/DeleteSchedule", "Backup", "DeleteSchedule", "/api/v1/backups/schedules/{id}", http.MethodDelete},
		{"Backup/default", "Backup", "Prune", "/api/v1/backups/prune", http.MethodPost},

		// Auth service
		{"Auth/Login", "Auth", "Login", "/api/v1/auth/login", http.MethodPost},
		{"Auth/Logout", "Auth", "Logout", "/api/v1/auth/logout", http.MethodPost},
		{"Auth/RefreshToken", "Auth", "RefreshToken", "/api/v1/auth/refresh", http.MethodPost},
		{"Auth/ValidateToken", "Auth", "ValidateToken", "/api/v1/auth/validate", http.MethodPost},
		{"Auth/ChangePassword", "Auth", "ChangePassword", "/api/v1/auth/password", http.MethodPut},
		{"Auth/ResetPassword", "Auth", "ResetPassword", "/api/v1/auth/password/reset", http.MethodPost},
		{"Auth/GetSession", "Auth", "GetSession", "/api/v1/auth/session", http.MethodGet},
		{"Auth/ListSessions", "Auth", "ListSessions", "/api/v1/auth/sessions", http.MethodGet},
		{"Auth/RevokeSession", "Auth", "RevokeSession", "/api/v1/auth/sessions/{id}", http.MethodDelete},
		{"Auth/default", "Auth", "Whoami", "/api/v1/auth/whoami", http.MethodPost},

		// Cache service
		{"Cache/GetStats", "Cache", "GetStats", "/api/v1/cache/stats", http.MethodGet},
		{"Cache/Clear", "Cache", "Clear", "/api/v1/cache/clear", http.MethodPost},
		{"Cache/GetConfig", "Cache", "GetConfig", "/api/v1/cache/config", http.MethodGet},
		{"Cache/UpdateConfig", "Cache", "UpdateConfig", "/api/v1/cache/config", http.MethodPut},
		{"Cache/Invalidate", "Cache", "Invalidate", "/api/v1/cache/invalidate", http.MethodPost},
		{"Cache/Warm", "Cache", "Warm", "/api/v1/cache/warm", http.MethodPost},
		{"Cache/default", "Cache", "Flush", "/api/v1/cache/flush", http.MethodPost},

		// Operations service
		{"Operations/MigrateData", "Operations", "MigrateData", "/api/v1/operations/migrate", http.MethodPost},
		{"Operations/GetMigrationProgress", "Operations", "GetMigrationProgress", "/api/v1/operations/migrate/progress", http.MethodGet},
		{"Operations/CancelMigration", "Operations", "CancelMigration", "/api/v1/operations/migrate/cancel", http.MethodPost},
		{"Operations/ReplaceNode", "Operations", "ReplaceNode", "/api/v1/operations/node/replace", http.MethodPost},
		{"Operations/UpgradeNode", "Operations", "UpgradeNode", "/api/v1/operations/node/upgrade", http.MethodPost},
		{"Operations/GetUpgradeStatus", "Operations", "GetUpgradeStatus", "/api/v1/operations/node/upgrade/status", http.MethodGet},
		{"Operations/StartMaintenance", "Operations", "StartMaintenance", "/api/v1/operations/maintenance/start", http.MethodPost},
		{"Operations/EndMaintenance", "Operations", "EndMaintenance", "/api/v1/operations/maintenance/end", http.MethodPost},
		{"Operations/GetMaintenanceStatus", "Operations", "GetMaintenanceStatus", "/api/v1/operations/maintenance/status", http.MethodGet},
		{"Operations/TriggerCompaction", "Operations", "TriggerCompaction", "/api/v1/operations/compaction", http.MethodPost},
		{"Operations/GetCompactionStatus", "Operations", "GetCompactionStatus", "/api/v1/operations/compaction/status", http.MethodGet},
		{"Operations/default", "Operations", "Snapshot", "/api/v1/operations/snapshot", http.MethodPost},

		// Embedding service
		{"Embedding/Generate", "Embedding", "Generate", "/api/v1/embedding/generate", http.MethodPost},
		{"Embedding/GenerateEmbedding", "Embedding", "GenerateEmbedding", "/api/v1/embedding/generate", http.MethodPost},
		{"Embedding/BatchGenerate", "Embedding", "BatchGenerate", "/api/v1/embedding/batch", http.MethodPost},
		{"Embedding/BatchGenerateEmbeddings", "Embedding", "BatchGenerateEmbeddings", "/api/v1/embedding/batch", http.MethodPost},
		{"Embedding/Search", "Embedding", "Search", "/api/v1/embedding/search", http.MethodPost},
		{"Embedding/Upsert", "Embedding", "Upsert", "/api/v1/embedding/upsert", http.MethodPost},
		{"Embedding/Delete", "Embedding", "Delete", "/api/v1/embedding/delete", http.MethodPost},
		{"Embedding/Get", "Embedding", "Get", "/api/v1/embedding/get", http.MethodPost},
		{"Embedding/ListProviders", "Embedding", "ListProviders", "/api/v1/embedding/providers", http.MethodGet},
		{"Embedding/GetProviderInfo", "Embedding", "GetProviderInfo", "/api/v1/embedding/providers/{provider}", http.MethodGet},
		{"Embedding/SetDefaultProvider", "Embedding", "SetDefaultProvider", "/api/v1/embedding/providers/default", http.MethodPut},
		{"Embedding/ListModels", "Embedding", "ListModels", "/api/v1/embedding/providers", http.MethodGet},
		{"Embedding/GetModel", "Embedding", "GetModel", "/api/v1/embedding/providers/{provider}", http.MethodGet},
		{"Embedding/SetDefaultModel", "Embedding", "SetDefaultModel", "/api/v1/embedding/providers/default", http.MethodPut},
		{"Embedding/GetCacheStats", "Embedding", "GetCacheStats", "/api/v1/embedding/cache/stats", http.MethodGet},
		{"Embedding/ClearCache", "Embedding", "ClearCache", "/api/v1/embedding/cache", http.MethodDelete},
		{"Embedding/default", "Embedding", "Reindex", "/api/v1/embedding/reindex", http.MethodPost},

		// ObjectStorageAdmin service
		{"ObjectStorageAdmin/CreateBackend", "ObjectStorageAdmin", "CreateBackend", "/api/v1/storage/admin/backends", http.MethodPost},
		{"ObjectStorageAdmin/UpdateBackend", "ObjectStorageAdmin", "UpdateBackend", "/api/v1/storage/admin/backends/{id}", http.MethodPut},
		{"ObjectStorageAdmin/DeleteBackend", "ObjectStorageAdmin", "DeleteBackend", "/api/v1/storage/admin/backends/{id}", http.MethodDelete},
		{"ObjectStorageAdmin/SetBackendStatus", "ObjectStorageAdmin", "SetBackendStatus", "/api/v1/storage/admin/backends/{id}/status", http.MethodPost},
		{"ObjectStorageAdmin/GetStorageStats", "ObjectStorageAdmin", "GetStorageStats", "/api/v1/storage/admin/stats", http.MethodGet},
		{"ObjectStorageAdmin/RunGarbageCollection", "ObjectStorageAdmin", "RunGarbageCollection", "/api/v1/storage/admin/gc", http.MethodPost},
		{"ObjectStorageAdmin/GetGarbageCollectionProgress", "ObjectStorageAdmin", "GetGarbageCollectionProgress", "/api/v1/storage/admin/gc/progress", http.MethodGet},
		{"ObjectStorageAdmin/RepairObject", "ObjectStorageAdmin", "RepairObject", "/api/v1/storage/admin/repair", http.MethodPost},
		{"ObjectStorageAdmin/CompactBackend", "ObjectStorageAdmin", "CompactBackend", "/api/v1/storage/admin/backends/{id}/compact", http.MethodPost},
		{"ObjectStorageAdmin/default", "ObjectStorageAdmin", "Rebalance", "/api/v1/storage/admin/rebalance", http.MethodPost},

		// Scheduler service
		{"Scheduler/CreateJob", "Scheduler", "CreateJob", "/api/v1/scheduler/jobs", http.MethodPost},
		{"Scheduler/GetJob", "Scheduler", "GetJob", "/api/v1/scheduler/jobs/{id}", http.MethodGet},
		{"Scheduler/UpdateJob", "Scheduler", "UpdateJob", "/api/v1/scheduler/jobs/{id}", http.MethodPut},
		{"Scheduler/DeleteJob", "Scheduler", "DeleteJob", "/api/v1/scheduler/jobs/{id}", http.MethodDelete},
		{"Scheduler/ListJobs", "Scheduler", "ListJobs", "/api/v1/scheduler/jobs", http.MethodGet},
		{"Scheduler/TriggerJob", "Scheduler", "TriggerJob", "/api/v1/scheduler/jobs/{id}/trigger", http.MethodPost},
		{"Scheduler/PauseJob", "Scheduler", "PauseJob", "/api/v1/scheduler/jobs/{id}/pause", http.MethodPost},
		{"Scheduler/ResumeJob", "Scheduler", "ResumeJob", "/api/v1/scheduler/jobs/{id}/resume", http.MethodPost},
		{"Scheduler/CancelJob", "Scheduler", "CancelJob", "/api/v1/scheduler/jobs/{id}/cancel", http.MethodPost},
		{"Scheduler/GetJobHistory", "Scheduler", "GetJobHistory", "/api/v1/scheduler/jobs/{id}/history", http.MethodGet},
		{"Scheduler/GetStatus", "Scheduler", "GetStatus", "/api/v1/scheduler/status", http.MethodGet},
		{"Scheduler/default", "Scheduler", "Reload", "/api/v1/scheduler/reload", http.MethodPost},

		// RAG service
		{"RAG/Query", "RAG", "Query", "/api/v1/rag/query", http.MethodPost},
		{"RAG/BuildContext", "RAG", "BuildContext", "/api/v1/rag/context", http.MethodPost},
		{"RAG/default", "RAG", "Index", "/api/v1/rag/index", http.MethodPost},

		// Certificate service
		{"Certificate/IssueCertificate", "Certificate", "IssueCertificate", "/api/v1/certificate/issue", http.MethodPost},
		{"Certificate/SignCSR", "Certificate", "SignCSR", "/api/v1/certificate/sign-csr", http.MethodPost},
		{"Certificate/RevokeCertificate", "Certificate", "RevokeCertificate", "/api/v1/certificate/revoke", http.MethodPost},
		{"Certificate/VerifyCertificate", "Certificate", "VerifyCertificate", "/api/v1/certificate/verify", http.MethodPost},
		{"Certificate/GetCACertificate", "Certificate", "GetCACertificate", "/api/v1/certificate/ca", http.MethodGet},
		{"Certificate/GetTLSConfig", "Certificate", "GetTLSConfig", "/api/v1/certificate/tls-config", http.MethodPost},
		{"Certificate/CAInfo", "Certificate", "CAInfo", "/api/v1/certificate/ca/info", http.MethodGet},
		{"Certificate/GetCABundle", "Certificate", "GetCABundle", "/api/v1/certificate/ca/bundle", http.MethodGet},
		{"Certificate/ListCertificates", "Certificate", "ListCertificates", "/api/v1/certificate/ca/list", http.MethodGet},
		{"Certificate/default", "Certificate", "GenerateCRL", "/api/v1/certificate/generatecrl", http.MethodPost},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, httpMethod := tr.mapToEndpoint(tc.service, tc.method)
			if path != tc.expectedPath {
				t.Errorf("path: expected %q, got %q", tc.expectedPath, path)
			}
			if httpMethod != tc.expectedMethod {
				t.Errorf("method: expected %q, got %q", tc.expectedMethod, httpMethod)
			}
		})
	}
}
