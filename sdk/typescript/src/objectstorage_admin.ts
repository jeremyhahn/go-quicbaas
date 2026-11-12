// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// ---------------------------------------------------------------------------
// Object storage administration domain types.
//
// Mirrors pkg/api/rest/handlers_objectstorage_admin.go
// (/api/v1/admin/object-backends/*, /api/v1/admin/storage/*,
// /api/v1/admin/objects/repair) and pkg/service/objectstorage_admin_types.go.
//
// Wire note: the request bodies use snake_case keys, but the replies are the
// untagged Go structs of objectstorage_admin_types.go, so their JSON keys are
// the Go field names in PascalCase (Name, CreatedAt, OperationID, ...). The
// Go SDK decodes them the same way. Every time is an int64 unix-nanosecond
// count, delivered as a number.
//
// The backend name and the GC operation_id are encoded with
// encodeURIComponent, so a name containing '/' stays one path segment.
// ---------------------------------------------------------------------------

/** ObjectStorageAdminCreateBackendRequest registers a new object storage backend. */
export interface ObjectStorageAdminCreateBackendRequest {
  name: string;
  type: string;
  tier: string;
  config?: Record<string, string>;
  enabled: boolean;
}

/** ObjectStorageAdminCreateBackendResponse confirms a backend was created. */
export interface ObjectStorageAdminCreateBackendResponse {
  Name: string;
  CreatedAt: number;
}

/** ObjectStorageAdminUpdateBackendRequest changes a backend's config or tier. */
export interface ObjectStorageAdminUpdateBackendRequest {
  config?: Record<string, string>;
  tier?: string;
}

/** ObjectStorageAdminUpdateBackendResponse confirms a backend was updated. */
export interface ObjectStorageAdminUpdateBackendResponse {
  Name: string;
  UpdatedAt: number;
}

/** ObjectStorageAdminDeleteBackendRequest removes a backend, optionally migrating its objects. */
export interface ObjectStorageAdminDeleteBackendRequest {
  migrate_to?: string;
  force?: boolean;
}

/** ObjectStorageAdminDeleteBackendResponse confirms a backend was deleted. */
export interface ObjectStorageAdminDeleteBackendResponse {
  Name: string;
  Deleted: boolean;
  MigratedObjects: number;
}

/** ObjectStorageAdminBackendStatusResponse confirms a backend's enabled state. */
export interface ObjectStorageAdminBackendStatusResponse {
  Name: string;
  Enabled: boolean;
  UpdatedAt: number;
}

/** ObjectStorageAdminCompactBackendRequest starts a backend compaction. */
export interface ObjectStorageAdminCompactBackendRequest {
  aggressive?: boolean;
}

/** ObjectStorageAdminCompactBackendResponse identifies a started compaction. */
export interface ObjectStorageAdminCompactBackendResponse {
  Name: string;
  OperationID: string;
  StartedAt: number;
}

/** ObjectStorageAdminBackendHealth is a backend's last health check. */
export interface ObjectStorageAdminBackendHealth {
  Healthy: boolean;
  Latency: number;
  LastCheck: number;
  Error: string;
}

/** ObjectStorageAdminBackendInfo describes one backend in the storage stats. */
export interface ObjectStorageAdminBackendInfo {
  Name: string;
  Type: string;
  Enabled: boolean;
  Default: boolean;
  Tier: string;
  Region: string;
  ObjectCount: number;
  TotalSize: number;
  Health: ObjectStorageAdminBackendHealth | null;
}

/** ObjectStorageAdminTenantStats is one tenant's storage usage by tier. */
export interface ObjectStorageAdminTenantStats {
  TenantID: string;
  TotalBytes: number;
  ObjectCount: number;
  HotBytes: number;
  WarmBytes: number;
  ColdBytes: number;
  ArchiveBytes: number;
}

/** ObjectStorageAdminStatsOptions selects the per-backend and per-tenant breakdowns. */
export interface ObjectStorageAdminStatsOptions {
  backends?: boolean;
  tenants?: boolean;
}

/** ObjectStorageAdminStatsResponse summarises object storage usage. */
export interface ObjectStorageAdminStatsResponse {
  TotalBytes: number;
  TotalObjects: number;
  TotalBackends: number;
  HealthyBackends: number;
  BackendStats: ObjectStorageAdminBackendInfo[] | null;
  TenantStats: ObjectStorageAdminTenantStats[] | null;
  CalculatedAt: number;
}

/** ObjectStorageAdminGCRequest starts a garbage collection run. */
export interface ObjectStorageAdminGCRequest {
  backends?: string[];
  dry_run?: boolean;
  aggressive?: boolean;
}

/** ObjectStorageAdminGCResponse identifies a started garbage collection run. */
export interface ObjectStorageAdminGCResponse {
  OperationID: string;
  StartedAt: number;
}

/** ObjectStorageAdminGCStatus is the state of a garbage collection run. */
export type ObjectStorageAdminGCStatus = 'running' | 'completed' | 'failed';

/** ObjectStorageAdminGCProgressResponse reports a garbage collection run's progress. */
export interface ObjectStorageAdminGCProgressResponse {
  OperationID: string;
  Status: ObjectStorageAdminGCStatus;
  ObjectsScanned: number;
  ObjectsDeleted: number;
  BytesReclaimed: number;
  PercentComplete: number;
  Error: string;
  CompletedAt: number;
}

/** ObjectStorageAdminRepairRequest repairs one object's replicas. */
export interface ObjectStorageAdminRepairRequest {
  key: string;
  source_location?: string;
  timeout_ms?: number;
}

/** ObjectStorageAdminRepairResponse reports an object repair's outcome. */
export interface ObjectStorageAdminRepairResponse {
  Key: string;
  Success: boolean;
  RepairedLocations: string[] | null;
  Error: string;
}

// ---------------------------------------------------------------------------
// ObjectStorageAdminClient interface
// ---------------------------------------------------------------------------

/**
 * ObjectStorageAdminClient manages object storage backends, reports storage
 * statistics, and runs garbage collection and object repair. Maps to the
 * /api/v1/admin/object-backends/*, /api/v1/admin/storage/* and
 * /api/v1/admin/objects/repair REST endpoints.
 */
export interface ObjectStorageAdminClient {
  /** createBackend registers a backend. POST /api/v1/admin/object-backends */
  createBackend(req: ObjectStorageAdminCreateBackendRequest): Promise<ObjectStorageAdminCreateBackendResponse>;

  /** updateBackend changes a backend's config or tier. PUT /api/v1/admin/object-backends/{name} */
  updateBackend(
    name: string,
    req: ObjectStorageAdminUpdateBackendRequest,
  ): Promise<ObjectStorageAdminUpdateBackendResponse>;

  /** deleteBackend removes a backend. DELETE /api/v1/admin/object-backends/{name} */
  deleteBackend(
    name: string,
    req?: ObjectStorageAdminDeleteBackendRequest,
  ): Promise<ObjectStorageAdminDeleteBackendResponse>;

  /** setBackendStatus enables or disables a backend. PUT /api/v1/admin/object-backends/{name}/status */
  setBackendStatus(name: string, enabled: boolean): Promise<ObjectStorageAdminBackendStatusResponse>;

  /** compactBackend starts a backend compaction. POST /api/v1/admin/object-backends/{name}/compact */
  compactBackend(
    name: string,
    req?: ObjectStorageAdminCompactBackendRequest,
  ): Promise<ObjectStorageAdminCompactBackendResponse>;

  /** storageStats summarises storage usage. GET /api/v1/admin/storage/stats */
  storageStats(opts?: ObjectStorageAdminStatsOptions): Promise<ObjectStorageAdminStatsResponse>;

  /** runGarbageCollection starts a GC run. POST /api/v1/admin/storage/gc */
  runGarbageCollection(req?: ObjectStorageAdminGCRequest): Promise<ObjectStorageAdminGCResponse>;

  /** garbageCollectionProgress reports a GC run's progress. GET /api/v1/admin/storage/gc/progress */
  garbageCollectionProgress(operationID: string): Promise<ObjectStorageAdminGCProgressResponse>;

  /** repairObject repairs one object's replicas. POST /api/v1/admin/objects/repair */
  repairObject(req: ObjectStorageAdminRepairRequest): Promise<ObjectStorageAdminRepairResponse>;
}
