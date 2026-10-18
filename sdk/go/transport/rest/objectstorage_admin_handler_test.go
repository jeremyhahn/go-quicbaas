// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
)

// objectStorageAdminCase is one verb: the route pkg/api/rest/routes.go
// serves for it, the service reply the handler writes and the typed reply
// the SDK must decode from it.
type objectStorageAdminCase struct {
	method     string
	req        interface{}
	httpMethod string
	path       string
	query      string
	status     int
	reply      interface{}
	want       interface{}
}

func objectStorageAdminCases() []objectStorageAdminCase {
	return []objectStorageAdminCase{
		{
			method: "CreateBackend",
			req: &osadmin.CreateBackendRequest{Name: "b1", Type: osadmin.BackendTypeS3, Tier: osadmin.StorageTierHot,
				Enabled: true, Config: map[string]string{"bucket": "x"}},
			httpMethod: http.MethodPost, path: "/api/v1/admin/object-backends", status: http.StatusCreated,
			reply: service.CreateBackendResponse{Name: "b1", CreatedAt: 7},
			want:  &osadmin.CreateBackendResponse{Name: "b1", CreatedAt: 7},
		},
		{
			method:     "UpdateBackend",
			req:        &osadmin.UpdateBackendRequest{Name: "b1", Tier: osadmin.StorageTierCold},
			httpMethod: http.MethodPut, path: "/api/v1/admin/object-backends/b1", status: http.StatusOK,
			reply: service.UpdateBackendResponse{Name: "b1", UpdatedAt: 8},
			want:  &osadmin.UpdateBackendResponse{Name: "b1", UpdatedAt: 8},
		},
		{
			method:     "DeleteBackend",
			req:        &osadmin.DeleteBackendRequest{Name: "b1", MigrateTo: "b2", Force: true},
			httpMethod: http.MethodDelete, path: "/api/v1/admin/object-backends/b1", query: "force=true&migrate_to=b2",
			status: http.StatusOK,
			reply:  service.DeleteBackendResponse{Name: "b1", Deleted: true, MigratedObjects: 3},
			want:   &osadmin.DeleteBackendResponse{Name: "b1", Deleted: true, MigratedObjects: 3},
		},
		{
			method:     "SetBackendStatus",
			req:        &osadmin.SetBackendStatusRequest{Name: "b1", Enabled: true},
			httpMethod: http.MethodPut, path: "/api/v1/admin/object-backends/b1/status", status: http.StatusOK,
			reply: service.SetBackendStatusResponse{Name: "b1", Enabled: true, UpdatedAt: 9},
			want:  &osadmin.SetBackendStatusResponse{Name: "b1", Enabled: true, UpdatedAt: 9},
		},
		{
			method:     "GetStorageStats",
			req:        &osadmin.GetStorageStatsRequest{IncludeBackends: true},
			httpMethod: http.MethodGet, path: "/api/v1/admin/storage/stats", query: "backends=true", status: http.StatusOK,
			reply: service.GetStorageStatsResponse{TotalBytes: 100, TotalBackends: 1,
				BackendStats: []*service.BackendInfo{{Name: "b1", Type: "gcs", Tier: "warm", TotalSize: 100}}},
			want: &osadmin.GetStorageStatsResponse{TotalBytes: 100, TotalBackends: 1,
				BackendStats: []*osadmin.BackendInfo{{Name: "b1", Type: osadmin.BackendTypeGCS,
					Tier: osadmin.StorageTierWarm, UsedBytes: 100}}},
		},
		{
			method:     "RunGarbageCollection",
			req:        &osadmin.RunGarbageCollectionRequest{DryRun: true},
			httpMethod: http.MethodPost, path: "/api/v1/admin/storage/gc", status: http.StatusAccepted,
			reply: service.RunGarbageCollectionResponse{OperationID: "gc-1", StartedAt: 11},
			want:  &osadmin.RunGarbageCollectionResponse{OperationID: "gc-1", StartedAt: 11},
		},
		{
			method:     "GetGarbageCollectionProgress",
			req:        &osadmin.GetGarbageCollectionProgressRequest{OperationID: "gc-1"},
			httpMethod: http.MethodGet, path: "/api/v1/admin/storage/gc/progress", query: "operation_id=gc-1",
			status: http.StatusOK,
			reply:  service.GetGarbageCollectionProgressResponse{OperationID: "gc-1", Status: "running", ObjectsScanned: 10},
			want: &osadmin.GetGarbageCollectionProgressResponse{OperationID: "gc-1",
				Status: osadmin.GarbageCollectionStatus("running"), ObjectsScanned: 10},
		},
		{
			method:     "RepairObject",
			req:        &osadmin.RepairObjectRequest{Key: "k1"},
			httpMethod: http.MethodPost, path: "/api/v1/admin/objects/repair", status: http.StatusOK,
			reply: service.RepairObjectResponse{Success: true, RepairedLocations: []string{"b1"}},
			want:  &osadmin.RepairObjectResponse{Success: true, RepairedLocations: []string{"b1"}},
		},
		{
			method:     "CompactBackend",
			req:        &osadmin.CompactBackendRequest{Name: "b1"},
			httpMethod: http.MethodPost, path: "/api/v1/admin/object-backends/b1/compact", status: http.StatusAccepted,
			reply: service.CompactBackendResponse{Name: "b1", OperationID: "c-1", StartedAt: 12},
			want:  &osadmin.CompactBackendResponse{Name: "b1", OperationID: "c-1", StartedAt: 12},
		},
	}
}

func TestObjectStorageAdminRESTRoutes(t *testing.T) {
	for _, tc := range objectStorageAdminCases() {
		t.Run(tc.method, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.httpMethod || r.URL.Path != tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("request = %s %s?%s, want %s %s?%s",
						r.Method, r.URL.Path, r.URL.RawQuery, tc.httpMethod, tc.path, tc.query)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if err := json.NewEncoder(w).Encode(tc.reply); err != nil {
					t.Errorf("encode reply: %v", err)
				}
			}))
			defer server.Close()
			tr := newTestTransport(t, server)
			defer tr.Close()

			got, err := tr.Request(context.Background(), "quicbaas.ObjectStorageAdmin", tc.method, tc.req)
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("reply = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestObjectStorageAdminRESTServerError(t *testing.T) {
	for _, tc := range objectStorageAdminCases() {
		t.Run(tc.method, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				if _, err := w.Write([]byte(`{"error":"conflict","code":"conflict"}`)); err != nil {
					t.Errorf("write: %v", err)
				}
			}))
			defer server.Close()
			tr := newTestTransport(t, server)
			defer tr.Close()

			if _, err := tr.Request(context.Background(), "quicbaas.ObjectStorageAdmin", tc.method, tc.req); err == nil {
				t.Fatal("server error status: want error")
			}
		})
	}
}

func TestObjectStorageAdminRESTRejectsBadRequests(t *testing.T) {
	tr := &Transport{}
	if _, err := tr.handleObjectStorageAdminRequest(context.Background(), "Rebalance", nil); err == nil {
		t.Fatal("unserved verb: want error")
	}
	if _, err := tr.handleObjectStorageAdminRequest(context.Background(), "CreateBackend", &osadmin.CompactBackendRequest{}); err == nil {
		t.Fatal("wrong request type: want error")
	}
}
