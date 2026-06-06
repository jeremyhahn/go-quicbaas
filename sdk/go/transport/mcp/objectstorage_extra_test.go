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

// objectstorage_extra_test.go covers the ObjectStorage MCP handler's extended
// methods (delete/head/copy/replicate/migrate/archive/restore/verify/policy/
// backend/tenant/batch) at the handler level, plus the dispatch error paths.

package mcp

import (
	"testing"

	os "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func newOSHandler(t *testing.T) *objectStorageHandler {
	t.Helper()
	tr, err := New(&transport.Config{Address: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return newObjectStorageHandler(tr)
}

// roundTrip transforms a request, then decodes a canned result map, verifying
// neither step errors and returning the decoded value for assertion.
func roundTrip(t *testing.T, h *objectStorageHandler, method string, req interface{}, result map[string]interface{}) interface{} {
	t.Helper()
	args, err := h.transformRequest(method, req)
	if err != nil {
		t.Fatalf("transformRequest(%s): %v", method, err)
	}
	if args == nil {
		t.Fatalf("transformRequest(%s) returned nil args", method)
	}
	out, err := h.transformResponse(method, result)
	if err != nil {
		t.Fatalf("transformResponse(%s): %v", method, err)
	}
	return out
}

func TestObjectStorageDeleteHead(t *testing.T) {
	h := newOSHandler(t)

	del := roundTrip(t, h, "DeleteObject", &os.DeleteObjectRequest{Key: "k", TenantID: "tn"},
		map[string]interface{}{"key": "k", "success": true, "size": float64(5), "timestamp": float64(100)})
	dr := del.(*os.DeleteObjectResponse)
	if !dr.Success || dr.Key != "k" {
		t.Errorf("delete mismatch: %+v", dr)
	}

	head := roundTrip(t, h, "HeadObject", &os.HeadObjectRequest{Key: "k", TenantID: "tn"},
		map[string]interface{}{"key": "k", "exists": true, "size": float64(5),
			"checksum": "deadbeef", "replica_count": float64(3)})
	hr := head.(*os.HeadObjectResponse)
	if !hr.Exists || hr.ReplicaCount != 3 {
		t.Errorf("head mismatch: %+v", hr)
	}
}

func TestObjectStorageListByTagAndTenant(t *testing.T) {
	h := newOSHandler(t)

	tag := roundTrip(t, h, "ListByTag",
		&os.ListByTagRequest{TagName: "env", TagValue: "prod", TenantID: "tn", Cursor: "c", Limit: 5},
		map[string]interface{}{
			"objects":     []interface{}{map[string]interface{}{"key": "a", "size": float64(1)}},
			"next_cursor": "n", "has_more": true, "total_count": float64(1),
		})
	tr := tag.(*os.ListByTagResponse)
	if len(tr.Objects) != 1 || !tr.HasMore {
		t.Errorf("list by tag mismatch: %+v", tr)
	}

	ten := roundTrip(t, h, "ListByTenant",
		&os.ListByTenantRequest{TenantID: "tn", Cursor: "c", Limit: 5, IncludeTags: true},
		map[string]interface{}{
			"objects":     []interface{}{map[string]interface{}{"key": "b", "size": float64(2)}},
			"total_count": float64(1),
		})
	tnr := ten.(*os.ListByTenantResponse)
	if len(tnr.Objects) != 1 || tnr.Objects[0].Key != "b" {
		t.Errorf("list by tenant mismatch: %+v", tnr)
	}
}

func TestObjectStorageCopyReplicateMigrate(t *testing.T) {
	h := newOSHandler(t)

	cp := roundTrip(t, h, "CopyObject",
		&os.CopyObjectRequest{SourceKey: "s", DestinationKey: "d", TenantID: "tn",
			TargetBackend: "b", OverwriteExisting: true},
		map[string]interface{}{"key": "d", "object_id": float64(1), "checksum": "ab", "version": float64(2)})
	cr := cp.(*os.CopyObjectResponse)
	if cr.Key != "d" || cr.ObjectID != 1 {
		t.Errorf("copy mismatch: %+v", cr)
	}

	rep := roundTrip(t, h, "ReplicateObject",
		&os.ReplicateObjectRequest{Key: "k", Targets: []string{"s3", "gcs"},
			TenantID: "tn", Synchronous: true, Priority: 5},
		map[string]interface{}{"key": "k", "status": "ok",
			"results": map[string]interface{}{
				"s3": map[string]interface{}{"success": true, "duration": float64(10)},
			}})
	rr := rep.(*os.ReplicateObjectResponse)
	if rr.Results["s3"] == nil || !rr.Results["s3"].Success {
		t.Errorf("replicate mismatch: %+v", rr)
	}

	mig := roundTrip(t, h, "MigrateObject",
		&os.MigrateObjectRequest{Key: "k", TargetBackend: "b", DeleteFromSource: true, TenantID: "tn"},
		map[string]interface{}{"key": "k", "source_backend": "old", "target_backend": "b",
			"success": true, "deleted_from_source": true})
	mr := mig.(*os.MigrateObjectResponse)
	if !mr.Success || !mr.DeletedFromSource {
		t.Errorf("migrate mismatch: %+v", mr)
	}
}

func TestObjectStorageArchiveRestoreVerify(t *testing.T) {
	h := newOSHandler(t)

	arc := roundTrip(t, h, "ArchiveObject",
		&os.ArchiveObjectRequest{Key: "k", TargetStore: "glacier", DeleteFromPrimary: true, TenantID: "tn"},
		map[string]interface{}{"key": "k", "archive_store": "glacier", "success": true,
			"deleted_from_primary": true})
	ar := arc.(*os.ArchiveObjectResponse)
	if !ar.Success || ar.ArchiveStore != "glacier" {
		t.Errorf("archive mismatch: %+v", ar)
	}

	res := roundTrip(t, h, "RestoreObject",
		&os.RestoreObjectRequest{Key: "k", TargetBackend: "b", TenantID: "tn"},
		map[string]interface{}{"key": "k", "backend": "b", "success": true})
	rr := res.(*os.RestoreObjectResponse)
	if !rr.Success {
		t.Errorf("restore mismatch: %+v", rr)
	}

	ver := roundTrip(t, h, "VerifyObject",
		&os.VerifyObjectRequest{Key: "k", Stores: []string{"s3"}, TenantID: "tn", RepairMismatches: true},
		map[string]interface{}{"key": "k", "valid": true, "repaired": false,
			"results": map[string]interface{}{
				"s3": map[string]interface{}{"valid": true, "checksum_match": true, "size_match": true},
			}})
	vr := ver.(*os.VerifyObjectResponse)
	if !vr.Valid || vr.Results["s3"] == nil || !vr.Results["s3"].ChecksumMatch {
		t.Errorf("verify mismatch: %+v", vr)
	}
}

func TestObjectStorageSetGetPolicy(t *testing.T) {
	h := newOSHandler(t)

	setReq := &os.SetObjectPolicyRequest{
		Key:      "k",
		TenantID: "tn",
		ReplicationPolicy: &os.ReplicationPolicy{
			Enabled: true, MinReplicas: 2, Mode: os.ReplicationMode("async"),
			Priority: 5, TargetStores: []string{"s3"},
		},
		ArchivePolicy: &os.ArchivePolicy{
			Enabled: true, ArchiveAfterDays: 30, TargetStore: "glacier", DeleteFromPrimary: true,
		},
		RetentionPolicy: &os.RetentionPolicy{Enabled: true, RetentionDays: 90},
	}
	set := roundTrip(t, h, "SetObjectPolicy", setReq,
		map[string]interface{}{"key": "k", "success": true, "version": float64(1)})
	sr := set.(*os.SetObjectPolicyResponse)
	if !sr.Success {
		t.Errorf("set policy mismatch: %+v", sr)
	}

	get := roundTrip(t, h, "GetObjectPolicy",
		&os.GetObjectPolicyRequest{Key: "k", TenantID: "tn"},
		map[string]interface{}{
			"key": "k",
			"replication": map[string]interface{}{
				"enabled": true, "min_replicas": float64(2), "mode": "async",
				"priority": float64(5), "target_stores": []interface{}{"s3"},
			},
			"archive": map[string]interface{}{
				"enabled": true, "after_days": float64(30), "target_store": "glacier",
				"delete_from_primary": true,
			},
			"retention": map[string]interface{}{"enabled": true, "days": float64(90)},
		})
	gr := get.(*os.GetObjectPolicyResponse)
	if gr.ReplicationPolicy == nil || gr.ArchivePolicy == nil || gr.RetentionPolicy == nil {
		t.Fatalf("get policy missing sub-policies: %+v", gr)
	}
	if gr.ArchivePolicy.ArchiveAfterDays != 30 || gr.RetentionPolicy.RetentionDays != 90 {
		t.Errorf("policy values mismatch: %+v", gr)
	}
}

func TestObjectStorageBackends(t *testing.T) {
	h := newOSHandler(t)

	lb := roundTrip(t, h, "ListBackends",
		&os.ListBackendsRequest{IncludeHealth: true, FilterByType: "s3"},
		map[string]interface{}{
			"backends": []interface{}{
				map[string]interface{}{
					"name": "primary", "type": "s3", "enabled": true, "default": true,
					"object_count": float64(10), "total_size": float64(1000),
					"health": map[string]interface{}{"healthy": true, "latency": float64(5)},
				},
			},
		})
	lbr := lb.(*os.ListBackendsResponse)
	if len(lbr.Backends) != 1 || lbr.Backends[0].Health == nil || !lbr.Backends[0].Health.Healthy {
		t.Errorf("list backends mismatch: %+v", lbr)
	}

	bh := roundTrip(t, h, "GetBackendHealth",
		&os.GetBackendHealthRequest{Name: "primary"},
		map[string]interface{}{"name": "primary", "healthy": true, "latency": float64(3), "status": "up"})
	bhr := bh.(*os.GetBackendHealthResponse)
	if !bhr.Healthy || bhr.Status != "up" {
		t.Errorf("backend health mismatch: %+v", bhr)
	}
}

func TestObjectStorageTenant(t *testing.T) {
	h := newOSHandler(t)

	usage := roundTrip(t, h, "GetTenantUsage",
		&os.GetTenantUsageRequest{TenantID: "tn"},
		map[string]interface{}{"tenant_id": "tn", "object_count": float64(5),
			"total_size": float64(500), "active_count": float64(4), "archived_count": float64(1)})
	ur := usage.(*os.GetTenantUsageResponse)
	if ur.ObjectCount != 5 || ur.ArchivedCount != 1 {
		t.Errorf("usage mismatch: %+v", ur)
	}

	quotas := roundTrip(t, h, "GetTenantQuotas",
		&os.GetTenantQuotasRequest{TenantID: "tn"},
		map[string]interface{}{"tenant_id": "tn", "max_storage_bytes": float64(1000),
			"max_object_size": float64(100), "max_object_count": float64(50),
			"used_storage_bytes": float64(200), "used_object_count": float64(10)})
	qr := quotas.(*os.GetTenantQuotasResponse)
	if qr.MaxStorageBytes != 1000 || qr.UsedObjectCount != 10 {
		t.Errorf("quotas mismatch: %+v", qr)
	}

	set := roundTrip(t, h, "SetTenantQuotas",
		&os.SetTenantQuotasRequest{TenantID: "tn", MaxStorageBytes: 2000,
			MaxObjectSize: 200, MaxObjectCount: 100},
		map[string]interface{}{"tenant_id": "tn", "updated_at": float64(123)})
	str := set.(*os.SetTenantQuotasResponse)
	if str.TenantID != "tn" || str.UpdatedAt != 123 {
		t.Errorf("set quotas mismatch: %+v", str)
	}
}

func TestObjectStorageBatchGetDelete(t *testing.T) {
	h := newOSHandler(t)

	get := roundTrip(t, h, "BatchGetObjects",
		&os.BatchGetObjectsRequest{Keys: []string{"a", "b"}, IncludeMetadata: true, TenantID: "tn"},
		map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"key": "a", "success": true,
					"value": "aGVsbG8=", "content_type": "text/plain"},
				map[string]interface{}{"key": "b", "success": false},
			},
		})
	gr := get.(*os.BatchGetObjectsResponse)
	if len(gr.Objects) != 1 || string(gr.Objects[0].Value) != "hello" {
		t.Errorf("batch get objects mismatch: %+v", gr.Objects)
	}
	if len(gr.NotFound) != 1 || gr.NotFound[0] != "b" {
		t.Errorf("batch get notfound mismatch: %+v", gr.NotFound)
	}

	del := roundTrip(t, h, "BatchDeleteObjects",
		&os.BatchDeleteObjectsRequest{Keys: []string{"a", "b"}, TenantID: "tn"},
		map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"key": "a", "success": true},
				map[string]interface{}{"key": "b", "success": false, "error": "locked"},
			},
		})
	dr := del.(*os.BatchDeleteObjectsResponse)
	if len(dr.DeletedKeys) != 1 || len(dr.FailedKeys) != 1 || dr.Errors["b"] != "locked" {
		t.Errorf("batch delete mismatch: %+v", dr)
	}
}

func TestObjectStorageBatchGetMalformedValue(t *testing.T) {
	h := newOSHandler(t)
	_, err := h.transformResponse("BatchGetObjects", map[string]interface{}{
		"results": []interface{}{
			map[string]interface{}{"key": "a", "success": true, "value": "%%%notb64%%%"},
		},
	})
	if err == nil {
		t.Fatal("expected error decoding malformed base64 in batch get")
	}
}

func TestObjectStorageUnsupportedMethod(t *testing.T) {
	h := newOSHandler(t)
	if _, err := h.transformRequest("Bogus", nil); err == nil {
		t.Error("transformRequest of unknown method must error")
	}
	if _, err := h.transformResponse("Bogus", nil); err == nil {
		t.Error("transformResponse of unknown method must error")
	}
}

func TestObjectStorageInvalidRequestTypes(t *testing.T) {
	h := newOSHandler(t)
	methods := []string{
		"DeleteObject", "HeadObject", "ListByTag", "ListByTenant", "CopyObject",
		"ReplicateObject", "MigrateObject", "ArchiveObject", "RestoreObject",
		"VerifyObject", "SetObjectPolicy", "GetObjectPolicy", "ListBackends",
		"GetBackendHealth", "GetTenantUsage", "GetTenantQuotas", "SetTenantQuotas",
		"BatchGetObjects", "BatchDeleteObjects",
	}
	for _, m := range methods {
		if _, err := h.transformRequest(m, "wrong-type"); err == nil {
			t.Errorf("transformRequest(%s) with wrong type must error", m)
		}
	}
}

func TestObjectStorageDecodeErrorsOnScalar(t *testing.T) {
	h := newOSHandler(t)
	// asResultMap rejects a non-map (scalar) result for every decode path.
	methods := []string{
		"DeleteObject", "HeadObject", "ListByTag", "ListByTenant", "CopyObject",
		"ReplicateObject", "MigrateObject", "ArchiveObject", "RestoreObject",
		"VerifyObject", "SetObjectPolicy", "GetObjectPolicy", "ListBackends",
		"GetBackendHealth", "GetTenantUsage", "GetTenantQuotas", "SetTenantQuotas",
		"BatchGetObjects", "BatchDeleteObjects",
	}
	for _, m := range methods {
		if _, err := h.transformResponse(m, "scalar"); err == nil {
			t.Errorf("transformResponse(%s) on scalar must error", m)
		}
	}
}

func TestObjectStorageStringSliceToInterface(t *testing.T) {
	got := stringSliceToInterface([]string{"x", "y"})
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("stringSliceToInterface = %v", got)
	}
	if got := stringSliceToInterface(nil); len(got) != 0 {
		t.Errorf("nil input = %v", got)
	}
}
