package quic

// The QUIC transport serves quicbaas.ObjectStorageAdmin through the embedded
// REST transport over HTTP/3. These tests drive the objectstorageadmin
// client through a real HTTP/3 server that stands in for the
// /api/v1/admin routes pkg/api/rest registers: every request field must
// reach the route (method, path, query and JSON body), every response field
// must reach the caller, an error status must surface as a typed SDK error,
// and an unknown verb must be refused (#1551).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

type osAdminQUICCase struct {
	verb       string
	call       func(context.Context, osadmin.Client) (interface{}, error)
	httpMethod string
	path       string
	query      string
	body       map[string]interface{}
	status     int
	reply      interface{}
	want       interface{}
}

func osAdminQUICCases() []osAdminQUICCase {
	return []osAdminQUICCase{
		{
			verb: "CreateBackend",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.CreateBackend(ctx, &osadmin.CreateBackendRequest{
					Name: "primary", Type: osadmin.BackendTypeMinio, Tier: osadmin.StorageTierWarm,
					Config: map[string]string{"bucket": "objects"}, Enabled: true,
				})
			},
			httpMethod: http.MethodPost, path: "/api/v1/admin/object-backends",
			body: map[string]interface{}{
				"name": "primary", "type": "minio", "tier": "warm",
				"config": map[string]interface{}{"bucket": "objects"}, "enabled": true,
			},
			status: http.StatusCreated,
			reply:  service.CreateBackendResponse{Name: "primary", CreatedAt: 101},
			want:   &osadmin.CreateBackendResponse{Name: "primary", CreatedAt: 101},
		},
		{
			verb: "UpdateBackend",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.UpdateBackend(ctx, &osadmin.UpdateBackendRequest{
					Name: "primary", Config: map[string]string{"region": "us-east-2"}, Tier: osadmin.StorageTierCold,
				})
			},
			httpMethod: http.MethodPut, path: "/api/v1/admin/object-backends/primary",
			body: map[string]interface{}{
				"config": map[string]interface{}{"region": "us-east-2"}, "tier": "cold",
			},
			status: http.StatusOK,
			reply:  service.UpdateBackendResponse{Name: "primary", UpdatedAt: 102},
			want:   &osadmin.UpdateBackendResponse{Name: "primary", UpdatedAt: 102},
		},
		{
			verb: "DeleteBackend",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.DeleteBackend(ctx, &osadmin.DeleteBackendRequest{Name: "primary", MigrateTo: "secondary", Force: true})
			},
			httpMethod: http.MethodDelete, path: "/api/v1/admin/object-backends/primary",
			query:  "force=true&migrate_to=secondary",
			status: http.StatusOK,
			reply:  service.DeleteBackendResponse{Name: "primary", Deleted: true, MigratedObjects: 5},
			want:   &osadmin.DeleteBackendResponse{Name: "primary", Deleted: true, MigratedObjects: 5},
		},
		{
			verb: "SetBackendStatus",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.SetBackendStatus(ctx, &osadmin.SetBackendStatusRequest{Name: "primary", Enabled: true})
			},
			httpMethod: http.MethodPut, path: "/api/v1/admin/object-backends/primary/status",
			body:   map[string]interface{}{"enabled": true},
			status: http.StatusOK,
			reply:  service.SetBackendStatusResponse{Name: "primary", Enabled: true, UpdatedAt: 103},
			want:   &osadmin.SetBackendStatusResponse{Name: "primary", Enabled: true, UpdatedAt: 103},
		},
		{
			verb: "GetStorageStats",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.GetStorageStats(ctx, &osadmin.GetStorageStatsRequest{IncludeBackends: true, IncludeTenants: true})
			},
			httpMethod: http.MethodGet, path: "/api/v1/admin/storage/stats", query: "backends=true&tenants=true",
			status: http.StatusOK,
			reply: service.GetStorageStatsResponse{
				TotalBytes: 4096, TotalObjects: 40, TotalBackends: 2, HealthyBackends: 1, CalculatedAt: 104,
				BackendStats: []*service.BackendInfo{{
					Name: "primary", Type: "s3", Enabled: true, Tier: "hot", Region: "eu-west-1",
					ObjectCount: 30, TotalSize: 3072, Health: &service.BackendHealthInfo{Healthy: true},
				}},
				TenantStats: []*service.TenantStorageStats{{
					TenantID: "acme", TotalBytes: 1024, ObjectCount: 10,
					HotBytes: 400, WarmBytes: 300, ColdBytes: 200, ArchiveBytes: 124,
				}},
			},
			want: &osadmin.GetStorageStatsResponse{
				TotalBytes: 4096, TotalObjects: 40, TotalBackends: 2, HealthyBackends: 1, CalculatedAt: 104,
				BackendStats: []*osadmin.BackendInfo{{
					Name: "primary", Type: osadmin.BackendTypeS3, Enabled: true, Healthy: true,
					Tier: osadmin.StorageTierHot, Region: "eu-west-1", ObjectCount: 30, UsedBytes: 3072,
				}},
				TenantStats: []*osadmin.TenantUsage{{
					TenantID: "acme", TotalBytes: 1024, ObjectCount: 10,
					HotBytes: 400, WarmBytes: 300, ColdBytes: 200, ArchiveBytes: 124,
				}},
			},
		},
		{
			verb: "RunGarbageCollection",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.RunGarbageCollection(ctx, &osadmin.RunGarbageCollectionRequest{
					Backends: []string{"primary", "secondary"}, DryRun: true, Aggressive: true,
				})
			},
			httpMethod: http.MethodPost, path: "/api/v1/admin/storage/gc",
			body: map[string]interface{}{
				"backends": []interface{}{"primary", "secondary"}, "dry_run": true, "aggressive": true,
			},
			status: http.StatusAccepted,
			reply:  service.RunGarbageCollectionResponse{OperationID: "gc-2", StartedAt: 105},
			want:   &osadmin.RunGarbageCollectionResponse{OperationID: "gc-2", StartedAt: 105},
		},
		{
			verb: "GetGarbageCollectionProgress",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.GetGarbageCollectionProgress(ctx, &osadmin.GetGarbageCollectionProgressRequest{OperationID: "gc-2"})
			},
			httpMethod: http.MethodGet, path: "/api/v1/admin/storage/gc/progress", query: "operation_id=gc-2",
			status: http.StatusOK,
			reply: service.GetGarbageCollectionProgressResponse{
				OperationID: "gc-2", Status: "failed", ObjectsScanned: 50, ObjectsDeleted: 4,
				BytesReclaimed: 512, PercentComplete: 37.5, Error: "disk offline", CompletedAt: 106,
			},
			want: &osadmin.GetGarbageCollectionProgressResponse{
				OperationID: "gc-2", Status: osadmin.GCStatusFailed, ObjectsScanned: 50, ObjectsDeleted: 4,
				BytesReclaimed: 512, PercentComplete: 37.5, Error: "disk offline", CompletedAt: 106,
			},
		},
		{
			verb: "RepairObject",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.RepairObject(ctx, &osadmin.RepairObjectRequest{
					Key: "tenants/acme/blob", SourceLocation: "node-1", TimeoutMs: 15000,
				})
			},
			httpMethod: http.MethodPost, path: "/api/v1/admin/objects/repair",
			body: map[string]interface{}{
				"key": "tenants/acme/blob", "source_location": "node-1", "timeout_ms": float64(15000),
			},
			status: http.StatusOK,
			reply: service.RepairObjectResponse{
				Key: "tenants/acme/blob", Success: true, RepairedLocations: []string{"node-2"}, Error: "partial",
			},
			want: &osadmin.RepairObjectResponse{
				Key: "tenants/acme/blob", Success: true, RepairedLocations: []string{"node-2"}, Error: "partial",
			},
		},
		{
			verb: "CompactBackend",
			call: func(ctx context.Context, c osadmin.Client) (interface{}, error) {
				return c.CompactBackend(ctx, &osadmin.CompactBackendRequest{Name: "primary", Aggressive: true})
			},
			httpMethod: http.MethodPost, path: "/api/v1/admin/object-backends/primary/compact",
			body:   map[string]interface{}{"aggressive": true},
			status: http.StatusAccepted,
			reply:  service.CompactBackendResponse{Name: "primary", OperationID: "compact-2", StartedAt: 107},
			want:   &osadmin.CompactBackendResponse{Name: "primary", OperationID: "compact-2", StartedAt: 107},
		},
	}
}

// newOSAdminQUICClient serves handler over HTTP/3 and returns an
// objectstorageadmin client on a QUIC transport dialled to it.
func newOSAdminQUICClient(t *testing.T, handler http.Handler) (osadmin.Client, *Transport) {
	t.Helper()
	addr, shutdown := startTestHTTP3Server(t, handler)
	t.Cleanup(shutdown)
	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	t.Cleanup(func() { tr.Close() })
	return osadmin.NewClient(tr), tr
}

// decodeOSAdminBody returns the JSON request body as a map, or nil when the
// request carries no body.
func decodeOSAdminBody(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read body: %v", err)
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Errorf("decode body %q: %v", raw, err)
	}
	return body
}

func TestObjectStorageAdminOverHTTP3_AllVerbs(t *testing.T) {
	for _, tc := range osAdminQUICCases() {
		t.Run(tc.verb, func(t *testing.T) {
			client, _ := newOSAdminQUICClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.httpMethod, r.Method, "method")
				assert.Equal(t, tc.path, r.URL.Path, "path")
				assert.Equal(t, tc.query, r.URL.RawQuery, "query")
				assert.Equal(t, tc.body, decodeOSAdminBody(t, r), "body")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if err := json.NewEncoder(w).Encode(tc.reply); err != nil {
					t.Errorf("encode reply: %v", err)
				}
			}))

			got, err := tc.call(context.Background(), client)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestObjectStorageAdminOverHTTP3_ServerErrors(t *testing.T) {
	failures := []struct {
		name   string
		status int
		check  func(error) bool
	}{
		{"NotFound", http.StatusNotFound, sdkerrors.IsNotFound},
		{"BadRequest", http.StatusBadRequest, sdkerrors.IsInvalidInput},
	}
	for _, failure := range failures {
		client, _ := newOSAdminQUICClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(failure.status)
			if _, err := w.Write([]byte(`{"error":"refused"}`)); err != nil {
				t.Errorf("write: %v", err)
			}
		}))
		for _, tc := range osAdminQUICCases() {
			t.Run(tc.verb+"/"+failure.name, func(t *testing.T) {
				_, err := tc.call(context.Background(), client)
				require.Error(t, err)
				assert.True(t, failure.check(err), "got %v, want a typed %s error", err, failure.name)
			})
		}
	}
}

func TestObjectStorageAdminOverHTTP3_RefusesUnknownVerb(t *testing.T) {
	_, tr := newOSAdminQUICClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unknown verb reached the server: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}))
	_, err := tr.Request(context.Background(), "quicbaas.ObjectStorageAdmin", "Rebalance",
		&osadmin.CompactBackendRequest{Name: "primary"})
	assert.True(t, sdkerrors.IsUnimplemented(err), "got %v, want a typed unimplemented error", err)
}
