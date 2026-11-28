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

package service

import (
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
)

// createRealCatalogObjstoreService builds the object storage service on a real
// catalog manager over the in-memory KV, so the catalog's key validation runs
// on every tenant-scoped key the service writes. The mock catalog used by the
// other service tests accepts any key and cannot catch a key the catalog
// rejects.
func createRealCatalogObjstoreService(t *testing.T) (ObjectStorageService, catalog.CatalogManager) {
	t.Helper()

	storage, err := catalog.NewKVStorage(catalog.NewMemoryKV())
	require.NoError(t, err)
	realCatalog, err := catalog.NewCatalogManager(&catalog.ManagerConfig{Storage: storage})
	require.NoError(t, err)
	t.Cleanup(func() { _ = realCatalog.Close() })

	backendMgr := newObjstoreMockBackendManager()
	backendMgr.AddBackend("local", newObjstoreMockStorage(), &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
		Default: true,
		Tier:    catalog.TierHot,
	})

	svc, err := NewObjectStorageService(realCatalog, backendMgr, newObjstoreMockConfigResolver(), nil, newObjstoreMockLogger())
	require.NoError(t, err)
	require.NotNil(t, svc)
	return svc, realCatalog
}

func TestObjectStorage_TenantOnRealCatalog_PutGetListDelete(t *testing.T) {
	svc, realCatalog := createRealCatalogObjstoreService(t)

	isoPut(t, svc, "tenant-a", "docs:shared.txt", "a's value", nil)
	isoPut(t, svc, "tenant-b", "docs:shared.txt", "b's value", nil)

	assert.Equal(t, []byte("a's value"), isoGetValue(t, svc, "tenant-a", "docs:shared.txt"))
	assert.Equal(t, []byte("b's value"), isoGetValue(t, svc, "tenant-b", "docs:shared.txt"))

	list, err := svc.ListObjects(callerOf("tenant-a"), &ListObjectsRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"docs:shared.txt"}, isoKeys(list.Objects), "tenant-a lists its own key, unprefixed")

	entry, err := realCatalog.Get(callerOf(qrdbsdk.SystemTenantID), "tenant/tenant-a/docs:shared.txt")
	require.NoError(t, err, "the catalog holds tenant-a's entry under its stored key")
	assert.Equal(t, "tenant-a", entry.TenantID)

	_, err = svc.DeleteObject(callerOf("tenant-a"), &DeleteObjectRequest{Key: "docs:shared.txt"})
	require.NoError(t, err)
	assert.Equal(t, []byte("b's value"), isoGetValue(t, svc, "tenant-b", "docs:shared.txt"),
		"tenant-a's delete leaves tenant-b's object")
}

func TestObjectStorage_TenantOnRealCatalog_ScopedKeyParses(t *testing.T) {
	_, err := catalog.ParseKey(osTenantScopedKey("tenant-premium", "premium:data/important.bin"))
	assert.NoError(t, err)
}
