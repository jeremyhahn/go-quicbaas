package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// errPrimaryTestBackendNotFound is returned by the recording backend lookup
// for a name that is not registered.
var errPrimaryTestBackendNotFound = errors.New("backend not found")

// primaryBackendLookups records every backend name the service asks for.
type primaryBackendLookups struct {
	mu    sync.Mutex
	names []string
}

func (l *primaryBackendLookups) record(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, name)
}

func (l *primaryBackendLookups) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.names...)
}

// newPrimaryBackendTestService wires the object storage service to the real
// config resolver, built the way the server builds it: the cluster defaults
// carry the primary backend resolved from osCfg.
func newPrimaryBackendTestService(t *testing.T, osCfg *config.ObjectStorageConfig) (ObjectStorageService, *primaryBackendLookups) {
	t.Helper()

	primary, err := osCfg.ResolvePrimaryBackend()
	require.NoError(t, err)

	storages := make(map[string]*objstoreMockStorage, len(osCfg.Backends))
	for name := range osCfg.Backends {
		storages[name] = newObjstoreMockStorage()
	}
	lookups := &primaryBackendLookups{}
	backendMgr := newObjstoreMockBackendManager()
	backendMgr.getBackendFn = func(name string) (common.Storage, error) {
		lookups.record(name)
		storage, ok := storages[name]
		if !ok {
			return nil, errPrimaryTestBackendNotFound
		}
		return storage, nil
	}
	for name, storage := range storages {
		backendMgr.AddBackend(name, storage, &catalog.BackendConfig{Type: "memory", Enabled: true})
	}

	defaults := config.DefaultObjectStoreDefaults()
	defaults.PrimaryBackend = primary
	resolver := config.NewObjectStoreResolver(&config.ObjectStoreFullConfig{
		Backends: osCfg.Backends,
		Defaults: *defaults,
	}, nil)

	svc, err := NewObjectStorageService(newObjstoreMockCatalogManager(), backendMgr, resolver, nil, newObjstoreMockLogger())
	require.NoError(t, err)
	return svc, lookups
}

func primaryTestConfig(primary string, names ...string) *config.ObjectStorageConfig {
	backends := make(map[string]*config.StorageBackendConfig, len(names))
	for _, name := range names {
		backends[name] = &config.StorageBackendConfig{Type: "memory"}
	}
	return &config.ObjectStorageConfig{Enabled: true, Backends: backends, PrimaryBackend: primary}
}

func TestPutObject_NoTargetReachesPrimary(t *testing.T) {
	svc, lookups := newPrimaryBackendTestService(t, primaryTestConfig("cold", "hot", "cold"))
	ctx := tenantctx.WithSystem(context.Background())

	resp, err := svc.PutObject(ctx, &PutObjectRequest{Key: "primary:put", Value: []byte("data")})
	require.NoError(t, err)
	assert.Equal(t, "cold", resp.Backend)
	assert.NotContains(t, lookups.snapshot(), "")
}

func TestPutObject_SingleDefaultBackendIsPrimary(t *testing.T) {
	svc, lookups := newPrimaryBackendTestService(t, primaryTestConfig("", "default"))
	ctx := tenantctx.WithSystem(context.Background())

	resp, err := svc.PutObject(ctx, &PutObjectRequest{Key: "primary:default", Value: []byte("data")})
	require.NoError(t, err)
	assert.Equal(t, "default", resp.Backend)
	assert.NotContains(t, lookups.snapshot(), "")
}

func TestPutObject_ExplicitTargetWinsOverPrimary(t *testing.T) {
	svc, _ := newPrimaryBackendTestService(t, primaryTestConfig("cold", "hot", "cold"))
	ctx := tenantctx.WithSystem(context.Background())

	resp, err := svc.PutObject(ctx, &PutObjectRequest{Key: "primary:explicit", Value: []byte("data"), TargetBackend: "hot"})
	require.NoError(t, err)
	assert.Equal(t, "hot", resp.Backend)
}

func TestPutObject_UnknownTargetRefused(t *testing.T) {
	svc, _ := newPrimaryBackendTestService(t, primaryTestConfig("cold", "hot", "cold"))
	ctx := tenantctx.WithSystem(context.Background())

	resp, err := svc.PutObject(ctx, &PutObjectRequest{Key: "primary:unknown", Value: []byte("data"), TargetBackend: "missing"})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestCopyObject_NoTargetReachesPrimary(t *testing.T) {
	svc, lookups := newPrimaryBackendTestService(t, primaryTestConfig("cold", "hot", "cold"))
	ctx := tenantctx.WithSystem(context.Background())

	_, err := svc.PutObject(ctx, &PutObjectRequest{Key: "primary:src", Value: []byte("data"), TargetBackend: "hot"})
	require.NoError(t, err)

	resp, err := svc.CopyObject(ctx, &CopyObjectRequest{SourceKey: "primary:src", DestinationKey: "primary:dst"})
	require.NoError(t, err)
	assert.Equal(t, "cold", resp.Backend)
	assert.NotContains(t, lookups.snapshot(), "")
}

func TestCopyObject_ExplicitTargetWinsOverPrimary(t *testing.T) {
	svc, _ := newPrimaryBackendTestService(t, primaryTestConfig("cold", "hot", "cold"))
	ctx := tenantctx.WithSystem(context.Background())

	_, err := svc.PutObject(ctx, &PutObjectRequest{Key: "primary:src", Value: []byte("data")})
	require.NoError(t, err)

	resp, err := svc.CopyObject(ctx, &CopyObjectRequest{SourceKey: "primary:src", DestinationKey: "primary:dst", TargetBackend: "hot"})
	require.NoError(t, err)
	assert.Equal(t, "hot", resp.Backend)
}

func TestCopyObject_UnknownTargetRefused(t *testing.T) {
	svc, _ := newPrimaryBackendTestService(t, primaryTestConfig("cold", "hot", "cold"))
	ctx := tenantctx.WithSystem(context.Background())

	_, err := svc.PutObject(ctx, &PutObjectRequest{Key: "primary:src", Value: []byte("data")})
	require.NoError(t, err)

	resp, err := svc.CopyObject(ctx, &CopyObjectRequest{SourceKey: "primary:src", DestinationKey: "primary:dst", TargetBackend: "missing"})
	require.Error(t, err)
	assert.Nil(t, resp)
}
