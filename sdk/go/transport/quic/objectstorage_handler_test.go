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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
)

// Note on wire-shape fidelity: the QUIC server encodes pkg/service.*Response
// objectstorage types, which declare their exported fields without JSON tags
// (PascalCase) — identical to the SDK objectstorage.*Response types. The bytes
// produced by EncodePayload(serverType) and EncodePayload(sdkType) are therefore
// byte-for-byte equal, so these tests emit the SDK type to reproduce the exact
// wire payload the server would write, then assert the decoder recovers it.

func TestIsObjectStorageService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.ObjectStorage", true},
		{"ObjectStorage", true},
		// Admin is a distinct service with its own opcodes; must not match.
		{"quicbaas.ObjectStorageAdmin", false},
		{"quicbaas.KV", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isObjectStorageService(tt.svc), tt.svc)
	}
}

func TestObjectStorageMethodToOpcode(t *testing.T) {
	cases := []struct {
		method string
		want   byte
	}{
		{"PutObject", OpOSPutObject},
		{"GetObject", OpOSGetObject},
		{"DeleteObject", OpOSDeleteObject},
		{"HeadObject", OpOSHeadObject},
		{"ListObjects", OpOSListObjects},
		{"ListByTag", OpOSListByTag},
		{"ListByTenant", OpOSListByTenant},
		{"CopyObject", OpOSCopyObject},
		{"ReplicateObject", OpOSReplicateObject},
		{"MigrateObject", OpOSMigrateObject},
		{"ArchiveObject", OpOSArchiveObject},
		{"RestoreObject", OpOSRestoreObject},
		{"VerifyObject", OpOSVerifyObject},
		{"SetObjectPolicy", OpOSUpdateObjectMetadata},
		{"GetObjectPolicy", OpOSGetLifecyclePolicy},
	}
	for _, c := range cases {
		op, ok := MethodToOpcode("quicbaas.ObjectStorage", c.method)
		require.True(t, ok, "method %s should resolve", c.method)
		assert.Equal(t, c.want, op, c.method)
	}
	// Opcodes must fall in the server's ObjectStorage range (0x28-0x3B).
	assert.Equal(t, byte(0x28), OpOSPutObject)
	assert.Equal(t, byte(0x3B), OpOSGetLifecyclePolicy)
}

// TestDecodeObjectStorageResponse_Put proves a PutObjectResponse (untagged
// PascalCase fields, including a base64-encoded checksum) round-trips.
func TestDecodeObjectStorageResponse_Put(t *testing.T) {
	want := &objectstorage.PutObjectResponse{
		Key:       "ns:doc",
		ObjectID:  42,
		Checksum:  []byte{0xde, 0xad, 0xbe, 0xef},
		Size:      1024,
		Backend:   "local",
		Version:   2,
		CreatedAt: 1700000000,
	}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeObjectStorageResponse("PutObject", payload)
	require.NoError(t, err)
	resp, ok := got.(*objectstorage.PutObjectResponse)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, "ns:doc", resp.Key)
	assert.Equal(t, uint64(42), resp.ObjectID)
	assert.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, resp.Checksum)
	assert.Equal(t, int64(1024), resp.Size)
	assert.Equal(t, "local", resp.Backend)
	assert.Equal(t, uint64(2), resp.Version)
	assert.Equal(t, int64(1700000000), resp.CreatedAt)
}

// TestDecodeObjectStorageResponse_Get verifies the object data (Value []byte) and
// tags round-trip through the base64 JSON encoding.
func TestDecodeObjectStorageResponse_Get(t *testing.T) {
	want := &objectstorage.GetObjectResponse{
		Key:         "ns:doc",
		Value:       []byte("hello world"),
		ContentType: "text/plain",
		Size:        11,
		Checksum:    []byte{0x01, 0x02},
		Version:     1,
		Tags:        map[string]string{"env": "prod"},
		TenantID:    "tenant-1",
		Backend:     "s3",
	}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeObjectStorageResponse("GetObject", payload)
	require.NoError(t, err)
	resp, ok := got.(*objectstorage.GetObjectResponse)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, []byte("hello world"), resp.Value)
	assert.Equal(t, "text/plain", resp.ContentType)
	assert.Equal(t, "prod", resp.Tags["env"])
	assert.Equal(t, "tenant-1", resp.TenantID)
}

func TestDecodeObjectStorageResponse_TypedMethods(t *testing.T) {
	del := &objectstorage.DeleteObjectResponse{Key: "k", Success: true, Size: 5, Timestamp: 1}
	head := &objectstorage.HeadObjectResponse{Key: "k", Exists: true, Size: 9, ContentType: "bin"}
	cp := &objectstorage.CopyObjectResponse{Key: "dst", ObjectID: 7, Size: 3, Backend: "local"}
	mig := &objectstorage.MigrateObjectResponse{Key: "k", SourceBackend: "a", TargetBackend: "b", Success: true}
	arch := &objectstorage.ArchiveObjectResponse{Key: "k", ArchiveStore: "cold", Success: true}
	rest := &objectstorage.RestoreObjectResponse{Key: "k", Backend: "hot", Success: true}
	verify := &objectstorage.VerifyObjectResponse{Key: "k", Valid: true, Repaired: false}
	repl := &objectstorage.ReplicateObjectResponse{Key: "k", Status: "ok"}

	cases := []struct {
		method string
		in     interface{}
		assert func(t *testing.T, got interface{})
	}{
		{"DeleteObject", del, func(t *testing.T, got interface{}) {
			r := got.(*objectstorage.DeleteObjectResponse)
			assert.True(t, r.Success)
			assert.Equal(t, int64(5), r.Size)
		}},
		{"HeadObject", head, func(t *testing.T, got interface{}) {
			r := got.(*objectstorage.HeadObjectResponse)
			assert.True(t, r.Exists)
			assert.Equal(t, int64(9), r.Size)
		}},
		{"CopyObject", cp, func(t *testing.T, got interface{}) {
			r := got.(*objectstorage.CopyObjectResponse)
			assert.Equal(t, "dst", r.Key)
			assert.Equal(t, uint64(7), r.ObjectID)
		}},
		{"MigrateObject", mig, func(t *testing.T, got interface{}) {
			r := got.(*objectstorage.MigrateObjectResponse)
			assert.Equal(t, "a", r.SourceBackend)
			assert.True(t, r.Success)
		}},
		{"ArchiveObject", arch, func(t *testing.T, got interface{}) {
			r := got.(*objectstorage.ArchiveObjectResponse)
			assert.Equal(t, "cold", r.ArchiveStore)
		}},
		{"RestoreObject", rest, func(t *testing.T, got interface{}) {
			r := got.(*objectstorage.RestoreObjectResponse)
			assert.Equal(t, "hot", r.Backend)
		}},
		{"VerifyObject", verify, func(t *testing.T, got interface{}) {
			r := got.(*objectstorage.VerifyObjectResponse)
			assert.True(t, r.Valid)
		}},
		{"ReplicateObject", repl, func(t *testing.T, got interface{}) {
			r := got.(*objectstorage.ReplicateObjectResponse)
			assert.Equal(t, "ok", r.Status)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			payload, err := EncodePayload(tc.in)
			require.NoError(t, err)
			got, err := decodeObjectStorageResponse(tc.method, payload)
			require.NoError(t, err)
			tc.assert(t, got)
		})
	}
}

func TestDecodeObjectStorageResponse_ListObjects(t *testing.T) {
	want := &objectstorage.ListObjectsResponse{
		Objects: []*objectstorage.ObjectSummary{
			{Key: "a", Size: 10, ContentType: "text"},
			{Key: "b", Size: 20, Status: "active"},
		},
		NextCursor: "cursor-2",
		HasMore:    true,
		TotalCount: 2,
	}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeObjectStorageResponse("ListObjects", payload)
	require.NoError(t, err)
	resp, ok := got.(*objectstorage.ListObjectsResponse)
	require.True(t, ok, "got %T", got)
	require.Len(t, resp.Objects, 2)
	assert.Equal(t, "a", resp.Objects[0].Key)
	assert.Equal(t, int64(20), resp.Objects[1].Size)
	assert.Equal(t, "cursor-2", resp.NextCursor)
	assert.True(t, resp.HasMore)

	// Empty payload returns nil (the client substitutes an empty response).
	got, err = decodeObjectStorageResponse("ListObjects", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestDecodeObjectStorageResponse_GetObjectPolicyTopLevel proves the top-level
// fields of GetObjectPolicyResponse round-trip. The nested policy structs are
// not tag-compatible with the server's catalog policy types (documented as an
// uncertain mapping in the package report) and are not asserted here.
func TestDecodeObjectStorageResponse_GetObjectPolicyTopLevel(t *testing.T) {
	want := &objectstorage.GetObjectPolicyResponse{
		Key:       "ns:doc",
		Timestamp: 1700000000,
	}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeObjectStorageResponse("GetObjectPolicy", payload)
	require.NoError(t, err)
	resp, ok := got.(*objectstorage.GetObjectPolicyResponse)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, "ns:doc", resp.Key)
	assert.Equal(t, int64(1700000000), resp.Timestamp)
}

// TestDecodeObjectStorageResponse_SetObjectPolicy round-trips the top-level
// fields of SetObjectPolicyResponse.
func TestDecodeObjectStorageResponse_SetObjectPolicy(t *testing.T) {
	want := &objectstorage.SetObjectPolicyResponse{Key: "k", Success: true, Version: 3, Timestamp: 9}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeObjectStorageResponse("SetObjectPolicy", payload)
	require.NoError(t, err)
	resp := got.(*objectstorage.SetObjectPolicyResponse)
	assert.Equal(t, "k", resp.Key)
	assert.True(t, resp.Success)
	assert.Equal(t, uint64(3), resp.Version)
}

func TestDecodeObjectStorageResponse_EmptyPayload(t *testing.T) {
	got, err := decodeObjectStorageResponse("PutObject", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeObjectStorageResponse_UnknownMethod(t *testing.T) {
	_, err := decodeObjectStorageResponse("Bogus", []byte(`{"foo":"bar"}`))
	require.Error(t, err)
}

func TestDecodeObjectStorageResponse_InvalidJSON(t *testing.T) {
	_, err := decodeObjectStorageResponse("PutObject", []byte(`{not json`))
	require.Error(t, err)
}

func TestDecodeObjectStorageResponse_ListByTagAndTenant(t *testing.T) {
	tag := &objectstorage.ListByTagResponse{
		Objects:    []*objectstorage.ObjectSummary{{Key: "t1"}},
		TotalCount: 1,
	}
	tagPayload, err := EncodePayload(tag)
	require.NoError(t, err)
	gotTag, err := decodeObjectStorageResponse("ListByTag", tagPayload)
	require.NoError(t, err)
	require.Len(t, gotTag.(*objectstorage.ListByTagResponse).Objects, 1)

	tenant := &objectstorage.ListByTenantResponse{
		Objects:    []*objectstorage.ObjectSummary{{Key: "u1"}, {Key: "u2"}},
		TotalCount: 2,
	}
	tenantPayload, err := EncodePayload(tenant)
	require.NoError(t, err)
	gotTenant, err := decodeObjectStorageResponse("ListByTenant", tenantPayload)
	require.NoError(t, err)
	require.Len(t, gotTenant.(*objectstorage.ListByTenantResponse).Objects, 2)

	// Both default to an empty (non-nil) slice when the server omits objects.
	emptyTag, err := decodeObjectStorageResponse("ListByTag", []byte(`{}`))
	require.NoError(t, err)
	assert.NotNil(t, emptyTag.(*objectstorage.ListByTagResponse).Objects)
	emptyTenant, err := decodeObjectStorageResponse("ListByTenant", []byte(`{}`))
	require.NoError(t, err)
	assert.NotNil(t, emptyTenant.(*objectstorage.ListByTenantResponse).Objects)
}

func TestDecodeObjectStorageResponse_AllInvalidJSON(t *testing.T) {
	for _, m := range []string{
		"GetObject", "DeleteObject", "HeadObject", "ListByTag", "ListByTenant",
		"CopyObject", "ReplicateObject", "MigrateObject", "ArchiveObject",
		"RestoreObject", "VerifyObject", "SetObjectPolicy", "GetObjectPolicy",
	} {
		_, err := decodeObjectStorageResponse(m, []byte(`{bad`))
		require.Error(t, err, m)
	}
}
