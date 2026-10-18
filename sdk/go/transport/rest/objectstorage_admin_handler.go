// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package rest

import (
	"context"
	"net/http"
	"net/url"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/osadminconv"
)

const (
	objectStorageAdminBackendsPath = "/api/v1/admin/object-backends"
	objectStorageAdminStoragePath  = "/api/v1/admin/storage"
)

func isObjectStorageAdminService(service string) bool {
	return trimRESTServicePrefix(service) == "ObjectStorageAdmin"
}

// objectStorageAdminRequestHandlers routes each ObjectStorageAdmin verb to
// the route pkg/api/rest registers for it.
var objectStorageAdminRequestHandlers = map[string]func(*Transport, context.Context, interface{}) (interface{}, error){
	"CreateBackend":                handleObjectStorageAdminCreateBackend,
	"UpdateBackend":                handleObjectStorageAdminUpdateBackend,
	"DeleteBackend":                handleObjectStorageAdminDeleteBackend,
	"SetBackendStatus":             handleObjectStorageAdminSetBackendStatus,
	"GetStorageStats":              handleObjectStorageAdminGetStorageStats,
	"RunGarbageCollection":         handleObjectStorageAdminRunGarbageCollection,
	"GetGarbageCollectionProgress": handleObjectStorageAdminGetGarbageCollectionProgress,
	"RepairObject":                 handleObjectStorageAdminRepairObject,
	"CompactBackend":               handleObjectStorageAdminCompactBackend,
}

func (t *Transport) handleObjectStorageAdminRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	handler, ok := objectStorageAdminRequestHandlers[method]
	if !ok {
		return nil, errors.New(errors.CodeUnimplemented, "rest.objectstorageadmin."+method,
			"ObjectStorageAdmin method is not served by the REST API")
	}
	return handler(t, ctx, req)
}

func objectStorageAdminInvalidRequest(method string) error {
	return errors.NewInvalidInput("rest.objectstorageadmin."+method, "request is required")
}

func objectStorageAdminBackendPath(name string) string {
	return objectStorageAdminBackendsPath + "/" + url.PathEscape(name)
}

func handleObjectStorageAdminCreateBackend(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.CreateBackendRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("CreateBackend")
	}
	body := struct {
		Name    string            `json:"name"`
		Type    string            `json:"type"`
		Tier    string            `json:"tier"`
		Config  map[string]string `json:"config,omitempty"`
		Enabled bool              `json:"enabled"`
	}{r.Name, osadminconv.BackendTypeName(r.Type), osadminconv.StorageTierName(r.Tier), r.Config, r.Enabled}
	var resp service.CreateBackendResponse
	if err := t.doJSONRequest(ctx, http.MethodPost, objectStorageAdminBackendsPath, body, &resp); err != nil {
		return nil, err
	}
	return osadminconv.CreateBackendResponse(&resp), nil
}

func handleObjectStorageAdminUpdateBackend(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.UpdateBackendRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("UpdateBackend")
	}
	body := struct {
		Config map[string]string `json:"config,omitempty"`
		Tier   string            `json:"tier,omitempty"`
	}{r.Config, osadminconv.StorageTierName(r.Tier)}
	var resp service.UpdateBackendResponse
	if err := t.doJSONRequest(ctx, http.MethodPut, objectStorageAdminBackendPath(r.Name), body, &resp); err != nil {
		return nil, err
	}
	return osadminconv.UpdateBackendResponse(&resp), nil
}

func handleObjectStorageAdminDeleteBackend(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.DeleteBackendRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("DeleteBackend")
	}
	endpoint := objectStorageAdminBackendPath(r.Name)
	q := url.Values{}
	if r.MigrateTo != "" {
		q.Set("migrate_to", r.MigrateTo)
	}
	if r.Force {
		q.Set("force", "true")
	}
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	var resp service.DeleteBackendResponse
	if err := t.doJSONRequest(ctx, http.MethodDelete, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return osadminconv.DeleteBackendResponse(&resp), nil
}

func handleObjectStorageAdminSetBackendStatus(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.SetBackendStatusRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("SetBackendStatus")
	}
	body := struct {
		Enabled bool `json:"enabled"`
	}{r.Enabled}
	var resp service.SetBackendStatusResponse
	if err := t.doJSONRequest(ctx, http.MethodPut, objectStorageAdminBackendPath(r.Name)+"/status", body, &resp); err != nil {
		return nil, err
	}
	return osadminconv.SetBackendStatusResponse(&resp), nil
}

func handleObjectStorageAdminGetStorageStats(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.GetStorageStatsRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("GetStorageStats")
	}
	endpoint := objectStorageAdminStoragePath + "/stats"
	q := url.Values{}
	if r.IncludeBackends {
		q.Set("backends", "true")
	}
	if r.IncludeTenants {
		q.Set("tenants", "true")
	}
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	var resp service.GetStorageStatsResponse
	if err := t.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return osadminconv.GetStorageStatsResponse(&resp), nil
}

func handleObjectStorageAdminRunGarbageCollection(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.RunGarbageCollectionRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("RunGarbageCollection")
	}
	body := struct {
		Backends   []string `json:"backends,omitempty"`
		DryRun     bool     `json:"dry_run,omitempty"`
		Aggressive bool     `json:"aggressive,omitempty"`
	}{r.Backends, r.DryRun, r.Aggressive}
	var resp service.RunGarbageCollectionResponse
	if err := t.doJSONRequest(ctx, http.MethodPost, objectStorageAdminStoragePath+"/gc", body, &resp); err != nil {
		return nil, err
	}
	return osadminconv.RunGarbageCollectionResponse(&resp), nil
}

func handleObjectStorageAdminGetGarbageCollectionProgress(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.GetGarbageCollectionProgressRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("GetGarbageCollectionProgress")
	}
	endpoint := objectStorageAdminStoragePath + "/gc/progress?" + url.Values{"operation_id": {r.OperationID}}.Encode()
	var resp service.GetGarbageCollectionProgressResponse
	if err := t.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return osadminconv.GetGarbageCollectionProgressResponse(&resp), nil
}

func handleObjectStorageAdminRepairObject(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.RepairObjectRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("RepairObject")
	}
	body := struct {
		Key            string `json:"key"`
		SourceLocation string `json:"source_location,omitempty"`
		TimeoutMs      int64  `json:"timeout_ms,omitempty"`
	}{r.Key, r.SourceLocation, r.TimeoutMs}
	var resp service.RepairObjectResponse
	if err := t.doJSONRequest(ctx, http.MethodPost, "/api/v1/admin/objects/repair", body, &resp); err != nil {
		return nil, err
	}
	return osadminconv.RepairObjectResponse(&resp), nil
}

func handleObjectStorageAdminCompactBackend(t *Transport, ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*osadmin.CompactBackendRequest)
	if !ok || r == nil {
		return nil, objectStorageAdminInvalidRequest("CompactBackend")
	}
	body := struct {
		Aggressive bool `json:"aggressive,omitempty"`
	}{r.Aggressive}
	var resp service.CompactBackendResponse
	if err := t.doJSONRequest(ctx, http.MethodPost, objectStorageAdminBackendPath(r.Name)+"/compact", body, &resp); err != nil {
		return nil, err
	}
	return osadminconv.CompactBackendResponse(&resp), nil
}
