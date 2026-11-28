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
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
)

// isoPut stores value under key as the given tenant.
func isoPut(t *testing.T, svc ObjectStorageService, tenantID, key, value string, tags map[string]string) {
	t.Helper()
	_, err := svc.PutObject(callerOf(tenantID), &PutObjectRequest{
		Key: key, Value: []byte(value), Tags: tags,
	})
	require.NoError(t, err, "%s puts %s", tenantID, key)
}

// isoGetValue reads key as the given tenant and returns its value.
func isoGetValue(t *testing.T, svc ObjectStorageService, tenantID, key string) []byte {
	t.Helper()
	got, err := svc.GetObject(callerOf(tenantID), &GetObjectRequest{Key: key})
	require.NoError(t, err, "%s reads %s", tenantID, key)
	return got.Value
}

func isoKeys(objects []*ObjectSummary) []string {
	keys := make([]string, 0, len(objects))
	for _, o := range objects {
		keys = append(keys, o.Key)
	}
	sort.Strings(keys)
	return keys
}

func TestObjectStorage_TenantIsolation_SameKeyTwoTenants(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)

	isoPut(t, svc, "tenant-a", "shared.txt", "alpha", nil)
	isoPut(t, svc, "tenant-b", "shared.txt", "bravo", nil)

	assert.Equal(t, []byte("alpha"), isoGetValue(t, svc, "tenant-a", "shared.txt"), "tenant-a reads its own object")
	assert.Equal(t, []byte("bravo"), isoGetValue(t, svc, "tenant-b", "shared.txt"), "tenant-b's put did not overwrite tenant-a's object")

	catalogMgr.mu.RLock()
	_, rawA := catalogMgr.entries["tenant/tenant-a/shared.txt"]
	_, rawB := catalogMgr.entries["tenant/tenant-b/shared.txt"]
	_, bare := catalogMgr.entries["shared.txt"]
	catalogMgr.mu.RUnlock()
	assert.True(t, rawA, "tenant-a's entry lives in its namespace")
	assert.True(t, rawB, "tenant-b's entry lives in its namespace")
	assert.False(t, bare, "no entry is stored under the bare key")

	_, err := svc.DeleteObject(callerOf("tenant-a"), &DeleteObjectRequest{Key: "shared.txt"})
	require.NoError(t, err, "tenant-a deletes its own object")

	_, err = svc.GetObject(callerOf("tenant-a"), &GetObjectRequest{Key: "shared.txt"})
	requireNotFound(t, err, "tenant-a's object is gone")
	assert.Equal(t, []byte("bravo"), isoGetValue(t, svc, "tenant-b", "shared.txt"), "tenant-a's delete left tenant-b's object")
}

func TestObjectStorage_TenantIsolation_CrossTenantReadListDeleteFail(t *testing.T) {
	svc, _, backendMgr, _ := createObjstoreTestService(t)
	backendMgr.AddBackend("cold", newObjstoreMockStorage(), &catalog.BackendConfig{
		Type: "local", Enabled: true, Tier: catalog.TierCold,
	})

	const key = "b/secret.txt"
	isoPut(t, svc, "tenant-b", key, "tenant-b data", map[string]string{"team": "x"})

	a := callerOf("tenant-a")
	verbs := map[string]func() error{
		"Get": func() error {
			_, err := svc.GetObject(a, &GetObjectRequest{Key: key})
			return err
		},
		"Delete": func() error {
			_, err := svc.DeleteObject(a, &DeleteObjectRequest{Key: key})
			return err
		},
		"Copy (as source)": func() error {
			_, err := svc.CopyObject(a, &CopyObjectRequest{SourceKey: key, DestinationKey: "a/stolen.txt"})
			return err
		},
		"Replicate": func() error {
			_, err := svc.ReplicateObject(a, &ReplicateObjectRequest{Key: key, Targets: []string{"cold"}, Synchronous: true})
			return err
		},
		"Migrate": func() error {
			_, err := svc.MigrateObject(a, &MigrateObjectRequest{Key: key, TargetBackend: "cold"})
			return err
		},
		"Archive": func() error {
			_, err := svc.ArchiveObject(a, &ArchiveObjectRequest{Key: key, TargetStore: "cold"})
			return err
		},
		"Restore": func() error {
			_, err := svc.RestoreObject(a, &RestoreObjectRequest{Key: key, TargetBackend: "local"})
			return err
		},
		"Verify": func() error {
			_, err := svc.VerifyObject(a, &VerifyObjectRequest{Key: key})
			return err
		},
		"GetObjectPolicy": func() error {
			_, err := svc.GetObjectPolicy(a, &GetObjectPolicyRequest{Key: key})
			return err
		},
		"SetObjectPolicy": func() error {
			_, err := svc.SetObjectPolicy(a, &SetObjectPolicyRequest{
				Key:             key,
				RetentionPolicy: &catalog.RetentionPolicy{},
			})
			return err
		},
	}
	for name, call := range verbs {
		requireNotFound(t, call(), "tenant-a "+name+" of tenant-b's key")
	}

	head, err := svc.HeadObject(a, &HeadObjectRequest{Key: key})
	require.NoError(t, err)
	assert.False(t, head.Exists, "tenant-a's Head does not see tenant-b's key")

	for _, prefix := range []string{"", "b/"} {
		list, err := svc.ListObjects(a, &ListObjectsRequest{Prefix: prefix})
		require.NoError(t, err)
		assert.Empty(t, list.Objects, "tenant-a's ListObjects with prefix %q", prefix)
	}

	byTag, err := svc.ListByTag(a, &ListByTagRequest{TagName: "team", TagValue: "x"})
	require.NoError(t, err)
	assert.Empty(t, byTag.Objects, "tenant-a's ListByTag")

	byTenant, err := svc.ListByTenant(a, &ListByTenantRequest{TenantID: "tenant-a"})
	require.NoError(t, err)
	assert.Empty(t, byTenant.Objects, "tenant-a's ListByTenant")

	assert.Equal(t, []byte("tenant-b data"), isoGetValue(t, svc, "tenant-b", key), "tenant-b still reads its object")
}

func TestObjectStorage_TenantIsolation_KeysAreReturnedUnscoped(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	a := callerOf("tenant-a")

	put, err := svc.PutObject(a, &PutObjectRequest{Key: "dir/x.txt", Value: []byte("x")})
	require.NoError(t, err)
	assert.Equal(t, "dir/x.txt", put.Key, "Put")

	head, err := svc.HeadObject(a, &HeadObjectRequest{Key: "dir/x.txt"})
	require.NoError(t, err)
	assert.True(t, head.Exists)
	assert.Equal(t, "dir/x.txt", head.Key, "Head")

	list, err := svc.ListObjects(a, &ListObjectsRequest{Prefix: "dir/"})
	require.NoError(t, err)
	assert.Equal(t, []string{"dir/x.txt"}, isoKeys(list.Objects), "ListObjects")

	cp, err := svc.CopyObject(a, &CopyObjectRequest{SourceKey: "dir/x.txt", DestinationKey: "dir/y.txt"})
	require.NoError(t, err)
	assert.Equal(t, "dir/y.txt", cp.Key, "Copy")

	all, err := svc.ListObjects(a, &ListObjectsRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"dir/x.txt", "dir/y.txt"}, isoKeys(all.Objects))
	for _, k := range isoKeys(all.Objects) {
		assert.False(t, strings.HasPrefix(k, "tenant/"), "key %q is returned unscoped", k)
	}
}

func TestObjectStorage_TenantIsolation_SystemScopeSeesTenantNamespace(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	isoPut(t, svc, "tenant-a", "dir/x.txt", "alpha", nil)

	list, err := svc.ListObjects(callerOf(qrdbsdk.SystemTenantID), &ListObjectsRequest{Prefix: "tenant/tenant-a/"})
	require.NoError(t, err)
	assert.Equal(t, []string{"tenant/tenant-a/dir/x.txt"}, isoKeys(list.Objects),
		"the system scope lists tenant-a's object under its stored key")
}

func TestObjectStorage_TenantIsolation_BatchVerbs(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	isoPut(t, svc, "tenant-b", "b/1.txt", "b1", nil)
	isoPut(t, svc, "tenant-b", "b/2.txt", "b2", nil)

	a := callerOf("tenant-a")
	keys := []string{"b/1.txt", "b/2.txt"}

	got, err := svc.BatchGetObjects(a, &BatchGetObjectsRequest{Keys: keys})
	require.NoError(t, err)
	assert.Equal(t, 0, got.FoundCount, "tenant-a's BatchGet reads none of tenant-b's keys")
	for _, r := range got.Results {
		assert.False(t, r.Found, "tenant-a's BatchGet of %s", r.Key)
		assert.Empty(t, r.Value)
	}

	del, err := svc.BatchDeleteObjects(a, &BatchDeleteObjectsRequest{Keys: keys, ContinueOnError: true})
	require.NoError(t, err)
	assert.Equal(t, 0, del.DeletedCount, "tenant-a's BatchDelete deletes none of tenant-b's keys")
	assert.Equal(t, 2, del.FailureCount)

	assert.Equal(t, []byte("b1"), isoGetValue(t, svc, "tenant-b", "b/1.txt"))
	assert.Equal(t, []byte("b2"), isoGetValue(t, svc, "tenant-b", "b/2.txt"))
}
