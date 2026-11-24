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

package service

import (
	"sort"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isoPut stores value under key as the given tenant.
func isoPut(t *testing.T, svc ObjectStorageService, tenantID, key, value string, tags map[string]string) {
	t.Helper()
	_, err := svc.PutObject(callerOf(tenantID), &PutObjectRequest{
		Key: key, Value: []byte(value), Tags: tags,
	})
	require.NoError(t, err, "%s puts %s", tenantID, key)
}

func isoKeys(objects []*ObjectSummary) []string {
	keys := make([]string, 0, len(objects))
	for _, o := range objects {
		keys = append(keys, o.Key)
	}
	sort.Strings(keys)
	return keys
}

func TestObjectStorage_SameKeyTwoTenants(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)

	isoPut(t, svc, "tenant-a", "shared/report.txt", "alpha", nil)
	isoPut(t, svc, "tenant-b", "shared/report.txt", "bravo", nil)

	gotA, err := svc.GetObject(callerOf("tenant-a"), &GetObjectRequest{Key: "shared/report.txt"})
	require.NoError(t, err)
	assert.Equal(t, []byte("alpha"), gotA.Value, "tenant-a reads its own object")

	gotB, err := svc.GetObject(callerOf("tenant-b"), &GetObjectRequest{Key: "shared/report.txt"})
	require.NoError(t, err)
	assert.Equal(t, []byte("bravo"), gotB.Value, "tenant-b's put did not overwrite tenant-a's object")

	catalogMgr.mu.RLock()
	_, rawA := catalogMgr.entries["tenant/tenant-a/shared/report.txt"]
	_, rawB := catalogMgr.entries["tenant/tenant-b/shared/report.txt"]
	_, bare := catalogMgr.entries["shared/report.txt"]
	catalogMgr.mu.RUnlock()
	assert.True(t, rawA, "tenant-a's entry lives in its namespace")
	assert.True(t, rawB, "tenant-b's entry lives in its namespace")
	assert.False(t, bare, "no entry is stored under the bare key")
}

func TestObjectStorage_CrossTenantReadListDeleteFail(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	isoPut(t, svc, "tenant-b", "b/only.txt", "tenant-b data", map[string]string{"team": "x"})

	_, err := svc.GetObject(callerOf("tenant-a"), &GetObjectRequest{Key: "b/only.txt"})
	requireNotFound(t, err, "tenant-a reads tenant-b's key")

	head, err := svc.HeadObject(callerOf("tenant-a"), &HeadObjectRequest{Key: "b/only.txt"})
	require.NoError(t, err)
	assert.False(t, head.Exists, "tenant-a does not see tenant-b's key exist")

	_, err = svc.DeleteObject(callerOf("tenant-a"), &DeleteObjectRequest{Key: "b/only.txt"})
	requireNotFound(t, err, "tenant-a deletes tenant-b's key")

	list, err := svc.ListObjects(callerOf("tenant-a"), &ListObjectsRequest{})
	require.NoError(t, err)
	assert.Empty(t, list.Objects, "tenant-a lists nothing of tenant-b's")

	byTag, err := svc.ListByTag(callerOf("tenant-a"), &ListByTagRequest{TagName: "team", TagValue: "x"})
	require.NoError(t, err)
	assert.Empty(t, byTag.Objects, "tenant-a's tag listing excludes tenant-b's objects")

	got, err := svc.GetObject(callerOf("tenant-b"), &GetObjectRequest{Key: "b/only.txt"})
	require.NoError(t, err, "tenant-b's object survived tenant-a's delete")
	assert.Equal(t, []byte("tenant-b data"), got.Value)
}

func TestObjectStorage_KeysAreReturnedUnscoped(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	isoPut(t, svc, "tenant-a", "docs/one.txt", "1", map[string]string{"kind": "doc"})
	isoPut(t, svc, "tenant-a", "docs/two.txt", "2", map[string]string{"kind": "doc"})
	isoPut(t, svc, "tenant-b", "docs/three.txt", "3", map[string]string{"kind": "doc"})

	ctx := callerOf("tenant-a")

	put, err := svc.PutObject(ctx, &PutObjectRequest{Key: "docs/four.txt", Value: []byte("4")})
	require.NoError(t, err)
	assert.Equal(t, "docs/four.txt", put.Key)

	got, err := svc.GetObject(ctx, &GetObjectRequest{Key: "docs/one.txt"})
	require.NoError(t, err)
	assert.Equal(t, "docs/one.txt", got.Key)

	list, err := svc.ListObjects(ctx, &ListObjectsRequest{Prefix: "docs/"})
	require.NoError(t, err)
	assert.Equal(t, []string{"docs/four.txt", "docs/one.txt", "docs/two.txt"}, isoKeys(list.Objects))

	byTag, err := svc.ListByTag(ctx, &ListByTagRequest{TagName: "kind", TagValue: "doc"})
	require.NoError(t, err)
	assert.Equal(t, []string{"docs/one.txt", "docs/two.txt"}, isoKeys(byTag.Objects))

	byTenant, err := svc.ListByTenant(ctx, &ListByTenantRequest{TenantID: "tenant-a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"docs/four.txt", "docs/one.txt", "docs/two.txt"}, isoKeys(byTenant.Objects))
}

func TestObjectStorage_SystemScopeSeesTenantNamespace(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	isoPut(t, svc, "tenant-a", "a/file.txt", "alpha", nil)

	sys := callerOf(qrdbsdk.SystemTenantID)

	list, err := svc.ListObjects(sys, &ListObjectsRequest{Prefix: "tenant/tenant-a/"})
	require.NoError(t, err)
	assert.Equal(t, []string{"tenant/tenant-a/a/file.txt"}, isoKeys(list.Objects),
		"the system scope sees the stored, tenant-scoped key")

	got, err := svc.GetObject(sys, &GetObjectRequest{Key: "tenant/tenant-a/a/file.txt"})
	require.NoError(t, err)
	assert.Equal(t, []byte("alpha"), got.Value)

	_, err = svc.GetObject(sys, &GetObjectRequest{Key: "a/file.txt"})
	requireNotFound(t, err, "the bare key is not a system-scope object")
}

func TestObjectStorage_BatchVerbs(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)

	put, err := svc.BatchPutObjects(callerOf("tenant-a"), &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{
			{Key: "batch/1.txt", Value: []byte("a1")},
			{Key: "batch/2.txt", Value: []byte("a2")},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, put.SuccessCount)
	isoPut(t, svc, "tenant-b", "batch/1.txt", "b1", nil)

	gotB, err := svc.BatchGetObjects(callerOf("tenant-b"), &BatchGetObjectsRequest{
		Keys: []string{"batch/1.txt", "batch/2.txt"},
	})
	require.NoError(t, err)
	require.Len(t, gotB.Results, 2)
	assert.True(t, gotB.Results[0].Found)
	assert.Equal(t, []byte("b1"), gotB.Results[0].Value, "tenant-b gets its own batch/1.txt")
	assert.False(t, gotB.Results[1].Found, "tenant-b does not see tenant-a's batch/2.txt")

	del, err := svc.BatchDeleteObjects(callerOf("tenant-b"), &BatchDeleteObjectsRequest{
		Keys: []string{"batch/1.txt", "batch/2.txt"}, ContinueOnError: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, del.DeletedCount, "tenant-b deletes only its own key")
	assert.Equal(t, 1, del.FailureCount)

	gotA, err := svc.BatchGetObjects(callerOf("tenant-a"), &BatchGetObjectsRequest{
		Keys: []string{"batch/1.txt", "batch/2.txt"},
	})
	require.NoError(t, err)
	require.Len(t, gotA.Results, 2)
	assert.Equal(t, []byte("a1"), gotA.Results[0].Value, "tenant-a's batch/1.txt survived")
	assert.Equal(t, []byte("a2"), gotA.Results[1].Value, "tenant-a's batch/2.txt survived")
}
