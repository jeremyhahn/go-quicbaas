// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/osadminconv"
)

// objectStorageAdminService is the service name the objectstorageadmin SDK
// client sends. The server's proto service is
// quicbaas.objectstorage.v1.ObjectStorageAdmin; both route to this handler.
const objectStorageAdminService = "quicbaas.ObjectStorageAdmin"

// Proto enum value names are the SDK enum names upper-cased behind these
// prefixes (BACKEND_TYPE_S3, STORAGE_TIER_HOT).
const (
	backendTypeProtoPrefix = "BACKEND_TYPE_"
	storageTierProtoPrefix = "STORAGE_TIER_"
)

// isObjectStorageAdminService reports whether svc names the ObjectStorageAdmin
// service.
func isObjectStorageAdminService(svc string) bool {
	return svc == objectStorageAdminService || svc == pb.ObjectStorageAdmin_ServiceDesc.ServiceName
}

// objectStorageAdminHandler relays the objectstorageadmin SDK client's plain
// Go requests through the generated ObjectStorageAdmin proto service and
// decodes the replies back into the SDK response types.
type objectStorageAdminHandler struct {
	conn *grpc.ClientConn
}

// newObjectStorageAdminHandler returns a handler over conn.
func newObjectStorageAdminHandler(conn *grpc.ClientConn) *objectStorageAdminHandler {
	return &objectStorageAdminHandler{conn: conn}
}

// objectStorageAdminMethod handles one ObjectStorageAdmin verb.
type objectStorageAdminMethod func(context.Context, pb.ObjectStorageAdminClient, interface{}) (interface{}, error)

// objectStorageAdminMethods maps each SDK method name to its handler.
var objectStorageAdminMethods = map[string]objectStorageAdminMethod{
	"CreateBackend":                osAdminCreateBackend,
	"UpdateBackend":                osAdminUpdateBackend,
	"DeleteBackend":                osAdminDeleteBackend,
	"SetBackendStatus":             osAdminSetBackendStatus,
	"GetStorageStats":              osAdminGetStorageStats,
	"RunGarbageCollection":         osAdminRunGarbageCollection,
	"GetGarbageCollectionProgress": osAdminGetGarbageCollectionProgress,
	"RepairObject":                 osAdminRepairObject,
	"CompactBackend":               osAdminCompactBackend,
}

// handleRequest dispatches method to its verb handler.
func (h *objectStorageAdminHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	fn, ok := objectStorageAdminMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("grpc.objectstorageadmin."+method,
			"method not exposed by ObjectStorageAdmin gRPC proto")
	}
	return fn(ctx, pb.NewObjectStorageAdminClient(h.conn), req)
}

// osAdminInvalidRequest is the error for a request of the wrong type.
func osAdminInvalidRequest(method, want string) error {
	return errors.NewInvalidInput("grpc.objectstorageadmin."+method,
		"invalid request type, expected *objectstorageadmin."+want)
}

func osAdminCreateBackend(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.CreateBackendRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("CreateBackend", "CreateBackendRequest")
	}
	resp, err := client.CreateBackend(ctx, &pb.CreateBackendRequest{
		Name:    r.Name,
		Type:    osAdminBackendTypeToProto(r.Type),
		Tier:    osAdminStorageTierToProto(r.Tier),
		Config:  r.Config,
		Enabled: r.Enabled,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.CreateBackend")
	}
	return &osadmin.CreateBackendResponse{
		Name:      resp.GetName(),
		CreatedAt: timestampToUnixNano(resp.GetCreatedAt()),
	}, nil
}

func osAdminUpdateBackend(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.UpdateBackendRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("UpdateBackend", "UpdateBackendRequest")
	}
	resp, err := client.UpdateBackend(ctx, &pb.UpdateBackendRequest{
		Name:   r.Name,
		Config: r.Config,
		Tier:   osAdminStorageTierToProto(r.Tier),
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.UpdateBackend")
	}
	return &osadmin.UpdateBackendResponse{
		Name:      resp.GetName(),
		UpdatedAt: timestampToUnixNano(resp.GetUpdatedAt()),
	}, nil
}

func osAdminDeleteBackend(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.DeleteBackendRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("DeleteBackend", "DeleteBackendRequest")
	}
	resp, err := client.DeleteBackend(ctx, &pb.DeleteBackendRequest{
		Name:      r.Name,
		MigrateTo: r.MigrateTo,
		Force:     r.Force,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.DeleteBackend")
	}
	return &osadmin.DeleteBackendResponse{
		Name:            resp.GetName(),
		Deleted:         resp.GetDeleted(),
		MigratedObjects: resp.GetMigratedObjects(),
	}, nil
}

func osAdminSetBackendStatus(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.SetBackendStatusRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("SetBackendStatus", "SetBackendStatusRequest")
	}
	resp, err := client.SetBackendStatus(ctx, &pb.SetBackendStatusRequest{
		Name:    r.Name,
		Enabled: r.Enabled,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.SetBackendStatus")
	}
	return &osadmin.SetBackendStatusResponse{
		Name:      resp.GetName(),
		Enabled:   resp.GetEnabled(),
		UpdatedAt: timestampToUnixNano(resp.GetUpdatedAt()),
	}, nil
}

func osAdminGetStorageStats(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.GetStorageStatsRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("GetStorageStats", "GetStorageStatsRequest")
	}
	resp, err := client.GetStorageStats(ctx, &pb.GetStorageStatsRequest{
		IncludeBackends: r.IncludeBackends,
		IncludeTenants:  r.IncludeTenants,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.GetStorageStats")
	}

	var backends []*osadmin.BackendInfo
	for _, b := range resp.GetBackendStats() {
		backends = append(backends, osAdminBackendInfoFromProto(b))
	}
	var tenants []*osadmin.TenantUsage
	for _, u := range resp.GetTenantStats() {
		tenants = append(tenants, osAdminTenantUsageFromProto(u))
	}
	return &osadmin.GetStorageStatsResponse{
		TotalBytes:      resp.GetTotalBytes(),
		TotalObjects:    resp.GetTotalObjects(),
		TotalBackends:   resp.GetTotalBackends(),
		HealthyBackends: resp.GetHealthyBackends(),
		BackendStats:    backends,
		TenantStats:     tenants,
		CalculatedAt:    timestampToUnixNano(resp.GetCalculatedAt()),
	}, nil
}

func osAdminRunGarbageCollection(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.RunGarbageCollectionRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("RunGarbageCollection", "RunGarbageCollectionRequest")
	}
	resp, err := client.RunGarbageCollection(ctx, &pb.RunGarbageCollectionRequest{
		Backends:   r.Backends,
		DryRun:     r.DryRun,
		Aggressive: r.Aggressive,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.RunGarbageCollection")
	}
	return &osadmin.RunGarbageCollectionResponse{
		OperationID: resp.GetOperationId(),
		StartedAt:   timestampToUnixNano(resp.GetStartedAt()),
	}, nil
}

func osAdminGetGarbageCollectionProgress(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.GetGarbageCollectionProgressRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("GetGarbageCollectionProgress", "GetGarbageCollectionProgressRequest")
	}
	resp, err := client.GetGarbageCollectionProgress(ctx, &pb.GetGarbageCollectionProgressRequest{
		OperationId: r.OperationID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.GetGarbageCollectionProgress")
	}
	return &osadmin.GetGarbageCollectionProgressResponse{
		OperationID:     resp.GetOperationId(),
		Status:          osadmin.GarbageCollectionStatus(resp.GetStatus()),
		ObjectsScanned:  resp.GetObjectsScanned(),
		ObjectsDeleted:  resp.GetObjectsDeleted(),
		BytesReclaimed:  resp.GetBytesReclaimed(),
		PercentComplete: resp.GetPercentComplete(),
		Error:           resp.GetError(),
		CompletedAt:     timestampToUnixNano(resp.GetCompletedAt()),
	}, nil
}

func osAdminRepairObject(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.RepairObjectRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("RepairObject", "RepairObjectRequest")
	}
	resp, err := client.RepairObject(ctx, &pb.RepairObjectRequest{
		Key:            r.Key,
		SourceLocation: r.SourceLocation,
		TimeoutMs:      r.TimeoutMs,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.RepairObject")
	}
	return &osadmin.RepairObjectResponse{
		Key:               resp.GetKey(),
		Success:           resp.GetSuccess(),
		RepairedLocations: resp.GetRepairedLocations(),
		Error:             resp.GetError(),
	}, nil
}

func osAdminCompactBackend(ctx context.Context, client pb.ObjectStorageAdminClient, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.CompactBackendRequest)
	if !ok || r == nil {
		return nil, osAdminInvalidRequest("CompactBackend", "CompactBackendRequest")
	}
	resp, err := client.CompactBackend(ctx, &pb.CompactBackendRequest{
		Name:       r.Name,
		Aggressive: r.Aggressive,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objectstorageadmin.CompactBackend")
	}
	return &osadmin.CompactBackendResponse{
		Name:        resp.GetName(),
		OperationID: resp.GetOperationId(),
		StartedAt:   timestampToUnixNano(resp.GetStartedAt()),
	}, nil
}

// osAdminBackendInfoFromProto converts one per-backend statistics entry.
func osAdminBackendInfoFromProto(b *pb.BackendInfo) *osadmin.BackendInfo {
	return &osadmin.BackendInfo{
		Name:            b.GetName(),
		Type:            osAdminBackendTypeFromProto(b.GetType()),
		Enabled:         b.GetEnabled(),
		Healthy:         b.GetHealthy(),
		Tier:            osAdminStorageTierFromProto(b.GetTier()),
		Region:          b.GetRegion(),
		Endpoint:        b.GetEndpoint(),
		Bucket:          b.GetBucket(),
		CapacityBytes:   b.GetCapacityBytes(),
		UsedBytes:       b.GetUsedBytes(),
		ObjectCount:     b.GetObjectCount(),
		LastHealthCheck: timestampToUnixNano(b.GetLastHealthCheck()),
		Metadata:        b.GetMetadata(),
	}
}

// osAdminTenantUsageFromProto converts one per-tenant usage entry.
func osAdminTenantUsageFromProto(u *pb.ObjectStorageTenantUsage) *osadmin.TenantUsage {
	return &osadmin.TenantUsage{
		TenantID:     u.GetTenantId(),
		TotalBytes:   u.GetTotalBytes(),
		ObjectCount:  u.GetObjectCount(),
		HotBytes:     u.GetHotBytes(),
		WarmBytes:    u.GetWarmBytes(),
		ColdBytes:    u.GetColdBytes(),
		ArchiveBytes: u.GetArchiveBytes(),
		CalculatedAt: timestampToUnixNano(u.GetCalculatedAt()),
	}
}

// osAdminBackendTypeToProto maps an SDK backend type to the proto enum by
// name. An unspecified or unknown type maps to BACKEND_TYPE_UNSPECIFIED.
func osAdminBackendTypeToProto(t osadmin.BackendType) pb.BackendType {
	name := backendTypeProtoPrefix + strings.ToUpper(osadminconv.BackendTypeName(t))
	return pb.BackendType(pb.BackendType_value[name])
}

// osAdminBackendTypeFromProto maps a proto backend type to the SDK enum by
// name. An unknown value maps to BackendTypeUnspecified.
func osAdminBackendTypeFromProto(t pb.BackendType) osadmin.BackendType {
	return osadminconv.ParseBackendType(strings.TrimPrefix(t.String(), backendTypeProtoPrefix))
}

// osAdminStorageTierToProto maps an SDK storage tier to the proto enum by
// name. An unspecified or unknown tier maps to STORAGE_TIER_UNSPECIFIED.
func osAdminStorageTierToProto(t osadmin.StorageTier) pb.StorageTier {
	name := storageTierProtoPrefix + strings.ToUpper(osadminconv.StorageTierName(t))
	return pb.StorageTier(pb.StorageTier_value[name])
}

// osAdminStorageTierFromProto maps a proto storage tier to the SDK enum by
// name. An unknown value maps to StorageTierUnspecified.
func osAdminStorageTierFromProto(t pb.StorageTier) osadmin.StorageTier {
	return osadminconv.ParseStorageTier(strings.TrimPrefix(t.String(), storageTierProtoPrefix))
}
