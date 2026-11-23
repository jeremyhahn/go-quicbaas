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

package ociregistry

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newObjectStorageService builds the object-storage service over a
// Pebble catalog and one go-objstore memory backend.
func newObjectStorageService(t *testing.T) service.ObjectStorageService {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "warn", Format: "json", Output: "stdout", Component: "ociregistry-objstore"})
	require.NoError(t, err)
	logAdapter := logger.NewAdapter(log)

	catalogStorage, err := catalog.NewPebbleStorage(&catalog.PebbleStorageConfig{
		Path:       filepath.Join(t.TempDir(), "catalog"),
		CacheSize:  8 * 1024 * 1024,
		SyncWrites: true,
	})
	require.NoError(t, err)
	catalogManager, err := catalog.NewCatalogManager(&catalog.ManagerConfig{Storage: catalogStorage, Logger: logAdapter})
	require.NoError(t, err)

	backendManager, err := catalog.NewBackendManager(catalog.DefaultBackendManagerConfig())
	require.NoError(t, err)
	require.NoError(t, backendManager.RegisterBackend("primary", &catalog.BackendConfig{
		Type: "memory", Enabled: true, Default: true, Tier: catalog.TierHot,
	}))
	resolver := config.NewObjectStoreResolver(&config.ObjectStoreFullConfig{
		Defaults: config.ObjectStoreDefaults{PrimaryBackend: "primary", Compression: "none"},
	}, nil)

	workerManager := catalog.NewWorkerManager(logAdapter)
	objects, err := service.NewObjectStorageService(catalogManager, backendManager, resolver, workerManager, logAdapter)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, workerManager.Close())
		assert.NoError(t, backendManager.Close())
		assert.NoError(t, catalogManager.Close())
	})
	return objects
}

// newObjectStoreRegistry is a Registry whose storage is the
// object-storage service.
func newObjectStoreRegistry(t *testing.T) (*Registry, service.ObjectStorageService) {
	t.Helper()
	objects := newObjectStorageService(t)
	storage, err := NewObjectStorage(objects, nil)
	require.NoError(t, err)
	reg, err := NewRegistry(validTestConfig(), testLogger(), WithStorage(storage))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close() })
	return reg, objects
}

// TestObjectStorage_RegistryRoundTrip pushes blobs and manifests through
// the registry into the object-storage service and reads them back:
// blobs (an empty one included), manifests by tag and by digest, tags,
// repositories, referrers and deletion.
func TestObjectStorage_RegistryRoundTrip(t *testing.T) {
	reg, objects := newObjectStoreRegistry(t)
	ctx := context.Background()

	layer := []byte("layer bytes")
	layerDigest := sha256Digest(layer)
	require.NoError(t, reg.PutBlob(ctx, layerDigest, layer))
	emptyDigest := sha256Digest(nil)
	require.NoError(t, reg.PutBlob(ctx, emptyDigest, []byte{}))

	got, err := reg.GetBlob(ctx, layerDigest)
	require.NoError(t, err)
	assert.Equal(t, layer, got)
	got, err = reg.GetBlob(ctx, emptyDigest)
	require.NoError(t, err)
	assert.Empty(t, got)
	ok, err := reg.BlobExists(ctx, sha256Digest([]byte("absent")))
	require.NoError(t, err)
	assert.False(t, ok)
	_, err = reg.GetBlob(ctx, sha256Digest([]byte("absent")))
	assert.ErrorAs(t, err, &BlobNotFoundError{})

	// The blob is an object in the object-storage service.
	obj, err := objects.GetObject(tenantctx.WithSystem(ctx), &service.GetObjectRequest{Key: objectKey(entityTypeBlob, "registry/blobs/"+normalizeDigest(layerDigest))})
	require.NoError(t, err)
	assert.Equal(t, layer, obj.Value)

	image := []byte(`{"schemaVersion":2,"mediaType":"` + manifestV1 + `",` +
		`"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + emptyDigest + `","size":0},` +
		`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + layerDigest + `","size":11}]}`)
	subject, err := reg.PutManifest(ctx, "apps/web", "v1", manifestV1, image)
	require.NoError(t, err)
	_, err = reg.PutManifest(ctx, "apps/web", "v2", manifestV1, image)
	require.NoError(t, err)
	sig := []byte(`{"schemaVersion":2,"mediaType":"` + manifestV1 + `","artifactType":"application/vnd.example.signature",` +
		`"config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"` + emptyDigest + `","size":0},"layers":[],` +
		`"subject":{"mediaType":"` + manifestV1 + `","digest":"` + subject + `","size":1}}`)
	sigDigest, err := reg.PutManifest(ctx, "apps/web", sha256Digest(sig), manifestV1, sig)
	require.NoError(t, err)

	for _, ref := range []string{"v1", subject} {
		mt, body, err := reg.GetManifest(ctx, "apps/web", ref)
		require.NoError(t, err, ref)
		assert.Equal(t, manifestV1, mt)
		assert.Contains(t, string(body), layerDigest)
	}
	tags, err := reg.ListTags(ctx, "apps/web")
	require.NoError(t, err)
	assert.Equal(t, []string{"v1", "v2"}, tags)
	repos, err := reg.ListRepositories(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"apps/web"}, repos)
	refs, err := reg.ListReferrers(ctx, "apps/web", subject, "")
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, sigDigest, refs[0].Digest)

	require.NoError(t, reg.DeleteManifest(ctx, "apps/web", "v2"))
	_, _, err = reg.GetManifest(ctx, "apps/web", "v2")
	assert.ErrorAs(t, err, &ManifestNotFoundError{})
	tags, err = reg.ListTags(ctx, "apps/web")
	require.NoError(t, err)
	assert.Equal(t, []string{"v1"}, tags)
	// Deleting again is not an error.
	require.NoError(t, reg.DeleteManifest(ctx, "apps/web", "v2"))
}

// TestObjectStoreClient_ListPages walks a listing across pages.
func TestObjectStoreClient_ListPages(t *testing.T) {
	client, err := NewObjectStoreClient(newObjectStorageService(t))
	require.NoError(t, err)
	ctx := context.Background()
	for _, k := range []string{"ns/a", "ns/b", "ns/c", "other/d"} {
		require.NoError(t, client.Put(ctx, entityTypeTag, k, []byte(k), 0))
	}
	require.NoError(t, client.Put(ctx, entityTypeBlob, "ns/blob", []byte("x"), 0))

	var all []string
	cursor := ""
	for {
		keys, next, more, err := client.List(ctx, entityTypeTag, "ns/", 2, cursor)
		require.NoError(t, err)
		all = append(all, keys...)
		if !more || next == "" {
			break
		}
		cursor = next
	}
	assert.ElementsMatch(t, []string{"ns/a", "ns/b", "ns/c"}, all)
}

// TestObjectPathEscape checks that the escape makes valid object paths,
// inverts, keeps prefixes and never maps two keys to one path.
func TestObjectPathEscape(t *testing.T) {
	keys := []string{
		"registry/manifests/apps/web:v1",
		"registry/manifests/apps/web:sha256:" + repeat64("a"),
		"registry/blobs/sha256/" + repeat64("b"),
		"registry/tags/my_app:v_1",
		"a_c", "a:", "a_u", "a_",
	}
	seen := map[string]string{}
	for _, k := range keys {
		p := escapeObjectPath(k)
		assert.True(t, catalog.IsValidPath(p), "%q -> %q is not a valid object path", k, p)
		back, ok := unescapeObjectPath(p)
		assert.True(t, ok, k)
		assert.Equal(t, k, back)
		if other, dup := seen[p]; dup {
			t.Errorf("%q and %q both escape to %q", k, other, p)
		}
		seen[p] = k
		assert.True(t, strings.HasPrefix(p, escapeObjectPath(k[:len(k)/2])), "prefix of %q", k)
	}
	for _, bad := range []string{"a_", "a_x", "_"} {
		_, ok := unescapeObjectPath(bad)
		assert.False(t, ok, bad)
	}
}

// TestObjectStoreClient_Errors covers the client's refusals and the
// object store's failures.
func TestObjectStoreClient_Errors(t *testing.T) {
	_, err := NewObjectStoreClient(nil)
	assert.ErrorAs(t, err, &NilClientError{})
	_, err = NewObjectStorage(nil, nil)
	assert.ErrorAs(t, err, &NilClientError{})

	client, err := NewObjectStoreClient(newObjectStorageService(t))
	require.NoError(t, err)
	err = client.Put(context.Background(), entityTypeBlob, "k", []byte("v"), time.Minute)
	assert.ErrorAs(t, err, &ObjectStoreTTLError{})

	boom := errors.New("object store down")
	failing, err := NewObjectStoreClient(failingObjectStore{err: boom})
	require.NoError(t, err)
	_, _, err = failing.Get(context.Background(), entityTypeBlob, "k")
	assert.ErrorIs(t, err, boom)
	assert.ErrorIs(t, failing.Delete(context.Background(), entityTypeBlob, "k"), boom)
	_, _, _, err = failing.List(context.Background(), entityTypeBlob, "", 1, "")
	assert.ErrorIs(t, err, boom)
}

// TestObjectStoreClient_BackendKeyNotFound covers the object store
// wrapping a backend key miss in a storage-coded error: the client and
// the registry storage read it as a missing object, while any other
// storage failure still surfaces.
func TestObjectStoreClient_BackendKeyNotFound(t *testing.T) {
	ctx := context.Background()
	miss := api.NewStorageOperationError("objectstorage.GetObject", "read from backend",
		api.NewStorageKeyNotFoundError("MemoryStorage.Get", "k"))

	client, err := NewObjectStoreClient(failingObjectStore{err: miss})
	require.NoError(t, err)
	value, found, err := client.Get(ctx, entityTypeBlob, "k")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, value)
	assert.NoError(t, client.Delete(ctx, entityTypeBlob, "k"))

	storage, err := NewObjectStorage(failingObjectStore{err: miss}, nil)
	require.NoError(t, err)
	_, err = storage.GetManifest(ctx, "apps/web:v2")
	assert.ErrorAs(t, err, &ManifestNotFoundError{})
	digest := sha256Digest([]byte("x"))
	_, err = storage.GetBlob(ctx, digest)
	assert.ErrorAs(t, err, &BlobNotFoundError{})
	exists, err := storage.ManifestExists(ctx, "apps/web:v2")
	require.NoError(t, err)
	assert.False(t, exists)
	exists, err = storage.BlobExists(ctx, digest)
	require.NoError(t, err)
	assert.False(t, exists)

	disk := errors.New("disk")
	broken := api.NewStorageOperationError("objectstorage.GetObject", "read from backend", disk)
	failing, err := NewObjectStoreClient(failingObjectStore{err: broken})
	require.NoError(t, err)
	_, found, err = failing.Get(ctx, entityTypeBlob, "k")
	assert.ErrorIs(t, err, disk)
	assert.False(t, found)

	brokenStorage, err := NewObjectStorage(failingObjectStore{err: broken}, nil)
	require.NoError(t, err)
	_, err = brokenStorage.GetManifest(ctx, "apps/web:v2")
	assert.ErrorAs(t, err, &ErrStorageError{})
	assert.False(t, errors.As(err, &ManifestNotFoundError{}))
}

// TestObjectStoreClient_CatalogFailureIsNotAbsence covers the object
// store reporting every catalog lookup failure as not-found: only a
// catalog entry-not-found cause is a missing object.
func TestObjectStoreClient_CatalogFailureIsNotAbsence(t *testing.T) {
	ctx := context.Background()
	corrupt := errors.New("corrupt entry")
	broken := api.NewNotFoundWithCauseError("objectstorage.GetObject", "object",
		catalog.NewDecodingError("k", corrupt))

	client, err := NewObjectStoreClient(failingObjectStore{err: broken})
	require.NoError(t, err)
	_, found, err := client.Get(ctx, entityTypeManifest, "k")
	assert.ErrorIs(t, err, corrupt)
	assert.False(t, found)
	assert.ErrorIs(t, client.Delete(ctx, entityTypeManifest, "k"), corrupt)

	storage, err := NewObjectStorage(failingObjectStore{err: broken}, nil)
	require.NoError(t, err)
	exists, err := storage.ManifestExists(ctx, "apps/web:v2")
	assert.ErrorAs(t, err, &ErrStorageError{})
	assert.False(t, exists)

	miss := api.NewNotFoundWithCauseError("objectstorage.GetObject", "object",
		catalog.NewEntryNotFoundError("k"))
	missing, err := NewObjectStoreClient(failingObjectStore{err: miss})
	require.NoError(t, err)
	value, found, err := missing.Get(ctx, entityTypeManifest, "k")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, value)
	assert.NoError(t, missing.Delete(ctx, entityTypeManifest, "k"))
}

type failingObjectStore struct{ err error }

func (f failingObjectStore) PutObject(context.Context, *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	return nil, f.err
}

func (f failingObjectStore) GetObject(context.Context, *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	return nil, f.err
}

func (f failingObjectStore) DeleteObject(context.Context, *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	return nil, f.err
}

func (f failingObjectStore) ListObjects(context.Context, *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	return nil, f.err
}
