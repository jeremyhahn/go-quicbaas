// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/osadminconv"
)

// objectStorageAdminServiceName is the service key the ObjectStorageAdmin
// SDK client sends.
const objectStorageAdminServiceName = "quicbaas.ObjectStorageAdmin"

func init() {
	unaryHandlers[objectStorageAdminServiceName+"/CreateBackend"] = objectStorageAdminHandler("CreateBackend",
		osadminconv.CreateBackendRequest, service.ObjectStorageAdminService.CreateBackend, osadminconv.CreateBackendResponse)
	unaryHandlers[objectStorageAdminServiceName+"/UpdateBackend"] = objectStorageAdminHandler("UpdateBackend",
		osadminconv.UpdateBackendRequest, service.ObjectStorageAdminService.UpdateBackend, osadminconv.UpdateBackendResponse)
	unaryHandlers[objectStorageAdminServiceName+"/DeleteBackend"] = objectStorageAdminHandler("DeleteBackend",
		osadminconv.DeleteBackendRequest, service.ObjectStorageAdminService.DeleteBackend, osadminconv.DeleteBackendResponse)
	unaryHandlers[objectStorageAdminServiceName+"/SetBackendStatus"] = objectStorageAdminHandler("SetBackendStatus",
		osadminconv.SetBackendStatusRequest, service.ObjectStorageAdminService.SetBackendStatus, osadminconv.SetBackendStatusResponse)
	unaryHandlers[objectStorageAdminServiceName+"/GetStorageStats"] = objectStorageAdminHandler("GetStorageStats",
		osadminconv.GetStorageStatsRequest, service.ObjectStorageAdminService.GetStorageStats, osadminconv.GetStorageStatsResponse)
	unaryHandlers[objectStorageAdminServiceName+"/RunGarbageCollection"] = objectStorageAdminHandler("RunGarbageCollection",
		osadminconv.RunGarbageCollectionRequest, service.ObjectStorageAdminService.RunGarbageCollection, osadminconv.RunGarbageCollectionResponse)
	unaryHandlers[objectStorageAdminServiceName+"/GetGarbageCollectionProgress"] = objectStorageAdminHandler("GetGarbageCollectionProgress",
		osadminconv.GetGarbageCollectionProgressRequest, service.ObjectStorageAdminService.GetGarbageCollectionProgress, osadminconv.GetGarbageCollectionProgressResponse)
	unaryHandlers[objectStorageAdminServiceName+"/RepairObject"] = objectStorageAdminHandler("RepairObject",
		osadminconv.RepairObjectRequest, service.ObjectStorageAdminService.RepairObject, osadminconv.RepairObjectResponse)
	unaryHandlers[objectStorageAdminServiceName+"/CompactBackend"] = objectStorageAdminHandler("CompactBackend",
		osadminconv.CompactBackendRequest, service.ObjectStorageAdminService.CompactBackend, osadminconv.CompactBackendResponse)
}

// objectStorageAdminHandler builds the embedded handler for one
// ObjectStorageAdmin verb: it accepts the SDK client's request type,
// calls the service method the gRPC and REST servers call, and returns
// the SDK response type the client decodes.
func objectStorageAdminHandler[SDKReq, SvcReq, SvcResp, SDKResp any](
	method string,
	toService func(*SDKReq) *SvcReq,
	call func(service.ObjectStorageAdminService, context.Context, *SvcReq) (*SvcResp, error),
	fromService func(*SvcResp) *SDKResp,
) func(context.Context, service.QuicBaaS, interface{}) (interface{}, error) {
	return func(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
		adminSvc := svc.ObjectStorageAdminService()
		if adminSvc == nil {
			return nil, NewServiceNotConfiguredError("ObjectStorageAdmin")
		}
		sdkReq, ok := req.(*SDKReq)
		if !ok || sdkReq == nil {
			var want *SDKReq
			return nil, NewInvalidRequestTypeError(objectStorageAdminServiceName, method, typeNameOf(want), typeNameOf(req))
		}
		resp, err := call(adminSvc, ctx, toService(sdkReq))
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, nil
		}
		return fromService(resp), nil
	}
}
