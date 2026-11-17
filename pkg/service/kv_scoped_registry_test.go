package service

import (
	"context"
	"sync"
	"testing"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fakeScopedKV is a tenant-scoped KVServicer that records the keys the kv
// service sends it. Methods the kv service does not use panic via the nil
// embedded interface.
type fakeScopedKV struct {
	qrdbservice.KVServicer

	mu                        sync.Mutex
	putKey, getKey, deleteKey string
	scanPrefix                string
	calls                     int
}

func (f *fakeScopedKV) Put(_ context.Context, key string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putKey = key
	f.calls++
	return nil
}

func (f *fakeScopedKV) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getKey = key
	f.calls++
	return []byte(`{}`), nil
}

func (f *fakeScopedKV) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteKey = key
	f.calls++
	return nil
}

func (f *fakeScopedKV) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scanPrefix = prefix
	f.calls++
	return map[string][]byte{}, nil
}

// fakeRegistry resolves every scope to one fakeScopedKV and records the
// tenants and databases it was asked for.
type fakeRegistry struct {
	qrdbservice.Registry

	kv      *fakeScopedKV
	mu      sync.Mutex
	tenants []string
	dbs     []string
}

func (r *fakeRegistry) KVService(tenant, db string) (qrdbservice.KVServicer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tenants = append(r.tenants, tenant)
	r.dbs = append(r.dbs, db)
	return r.kv, nil
}

func withQRDBServiceRegistryProvider(provider func() qrdbservice.Registry) func(*serviceBuilder) {
	return func(b *serviceBuilder) {
		b.WithQRDBServiceRegistry(provider)
	}
}

// runKVOps drives Put, Get, Delete and List for User:42 under ctx and
// returns each operation's error in that order.
func runKVOps(t *testing.T, svc QuicBaaS, ctx context.Context) []error {
	t.Helper()
	_, putErr := svc.Put(ctx, &PutRequest{EntityType: "User", ID: 42, Data: []byte(`{}`)})
	_, getErr := svc.Get(ctx, &GetRequest{EntityType: "User", ID: 42})
	_, delErr := svc.Delete(ctx, &DeleteRequest{EntityType: "User", ID: 42})
	stream := make(chan *ListResponse, 16)
	listErr := svc.List(ctx, &ListRequest{EntityType: "User"}, stream)
	for range stream { //nolint:revive // drain the closed stream
	}
	return []error{putErr, getErr, delErr, listErr}
}

func TestKVRegistry_NoProvider_UsesSharedClient(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	shared := newSvcBoostMockQRDBProviderWithGet([]byte(`{}`), nil)
	svc := createTestService(t, withQRDBClientProvider(shared))
	defer svc.Close()

	for _, err := range runKVOps(t, svc, ctx) {
		require.NoError(t, err)
	}
	assert.Equal(t, "__t:acme-corp:User:42", shared.capturedPutKey())
	assert.Equal(t, "__t:acme-corp:User:42", shared.capturedGetKey())
	assert.Equal(t, "__t:acme-corp:User:42", shared.capturedDeleteKey())
	assert.NotEmpty(t, shared.capturedScanKey())
}

func TestKVRegistry_ProviderSet_AllOpsReachScopedKV(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")

	// The shared-client path gives the reference keys.
	ref := newSvcBoostMockQRDBProviderWithGet([]byte(`{}`), nil)
	refSvc := createTestService(t, withQRDBClientProvider(ref))
	defer refSvc.Close()
	for _, err := range runKVOps(t, refSvc, ctx) {
		require.NoError(t, err)
	}

	shared := newSvcBoostMockQRDBProviderWithGet([]byte(`{}`), nil)
	reg := &fakeRegistry{kv: &fakeScopedKV{}}
	providerCalls := 0
	svc := createTestService(t,
		withQRDBClientProvider(shared),
		withQRDBServiceRegistryProvider(func() qrdbservice.Registry {
			providerCalls++
			return reg
		}))
	defer svc.Close()

	for _, err := range runKVOps(t, svc, ctx) {
		require.NoError(t, err)
	}

	assert.Equal(t, 4, reg.kv.calls, "Put, Get, Delete and List must each reach the scoped KVServicer")
	assert.Equal(t, ref.capturedPutKey(), reg.kv.putKey)
	assert.Equal(t, ref.capturedGetKey(), reg.kv.getKey)
	assert.Equal(t, ref.capturedDeleteKey(), reg.kv.deleteKey)
	assert.Equal(t, ref.capturedScanKey(), reg.kv.scanPrefix)
	assert.Equal(t, []string{"acme-corp", "acme-corp", "acme-corp", "acme-corp"}, reg.tenants)
	assert.Equal(t, []string{"", "", "", ""}, reg.dbs)
	assert.Equal(t, 1, providerCalls, "the first non-nil registry is cached")

	assert.Empty(t, shared.capturedPutKey(), "the shared client must not be written")
	assert.Empty(t, shared.capturedGetKey())
	assert.Empty(t, shared.capturedDeleteKey())
	assert.Empty(t, shared.capturedScanKey())
}

func TestKVRegistry_ProviderReturnsNil_FailsClosed(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	shared := newSvcBoostMockQRDBProviderWithGet([]byte(`{}`), nil)
	reg := &fakeRegistry{kv: &fakeScopedKV{}}
	var sealed = true
	providerCalls := 0
	svc := createTestService(t,
		withQRDBClientProvider(shared),
		withQRDBServiceRegistryProvider(func() qrdbservice.Registry {
			providerCalls++
			if sealed {
				return nil
			}
			return reg
		}))
	defer svc.Close()

	for i, err := range runKVOps(t, svc, ctx) {
		require.Error(t, err, "operation %d", i)
		assert.Equal(t, errors.ErrUnavailable, errors.Code(err), "operation %d: %v", i, err)
	}
	assert.Equal(t, 4, providerCalls, "a nil registry is not cached")
	assert.Empty(t, shared.capturedPutKey(), "a sealed registry must not fall back to the shared client")
	assert.Empty(t, shared.capturedGetKey())
	assert.Empty(t, shared.capturedDeleteKey())
	assert.Empty(t, shared.capturedScanKey())
	assert.Zero(t, reg.kv.calls)

	// Once the provider has a registry (unsealed), operations reach it.
	sealed = false
	for _, err := range runKVOps(t, svc, ctx) {
		require.NoError(t, err)
	}
	assert.Equal(t, 4, reg.kv.calls)
	assert.Empty(t, shared.capturedPutKey())
}
