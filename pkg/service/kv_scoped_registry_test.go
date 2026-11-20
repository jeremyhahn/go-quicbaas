package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	qrdblock "github.com/jeremyhahn/go-qrdb/pkg/lock"
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
	for resp := range stream {
		_ = resp
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

// memScope is one (tenant, db) keyspace that a memRegistry's KV and batch
// servicers share, so a batch write is visible to a later KV read.
type memScope struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *memScope) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data))
	for k := range s.data {
		out = append(out, k)
	}
	return out
}

// memKV is a KVServicer over a memScope.
type memKV struct {
	qrdbservice.KVServicer
	scope *memScope
}

func (m *memKV) Put(_ context.Context, key string, value []byte) error {
	m.scope.mu.Lock()
	defer m.scope.mu.Unlock()
	m.scope.data[key] = append([]byte(nil), value...)
	return nil
}

func (m *memKV) Get(_ context.Context, key string) ([]byte, error) {
	m.scope.mu.Lock()
	defer m.scope.mu.Unlock()
	v, ok := m.scope.data[key]
	if !ok {
		return nil, &qrdbservice.KeyNotFoundError{Key: key}
	}
	return v, nil
}

func (m *memKV) Delete(_ context.Context, key string) error {
	m.scope.mu.Lock()
	defer m.scope.mu.Unlock()
	delete(m.scope.data, key)
	return nil
}

func (m *memKV) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	m.scope.mu.Lock()
	defer m.scope.mu.Unlock()
	out := make(map[string][]byte)
	for k, v := range m.scope.data {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out, nil
}

// memBatch is a BatchServicer over a memScope.
type memBatch struct {
	qrdbservice.BatchServicer
	scope *memScope
}

func (m *memBatch) Execute(_ context.Context, ops []qrdbservice.Op) ([]qrdbservice.OpResponse, error) {
	m.scope.mu.Lock()
	defer m.scope.mu.Unlock()
	for _, op := range ops {
		switch op.Type {
		case qrdblock.OpPut:
			m.scope.data[string(op.Key)] = append([]byte(nil), op.Value...)
		case qrdblock.OpDelete:
			delete(m.scope.data, string(op.Key))
		}
	}
	return make([]qrdbservice.OpResponse, len(ops)), nil
}

// memRegistry resolves every scope to one memScope.
type memRegistry struct {
	qrdbservice.Registry
	scope *memScope
}

func newMemRegistry() *memRegistry {
	return &memRegistry{scope: &memScope{data: map[string][]byte{}}}
}

func (r *memRegistry) KVService(_, _ string) (qrdbservice.KVServicer, error) {
	return &memKV{scope: r.scope}, nil
}

func (r *memRegistry) BatchService(_, _ string) (qrdbservice.BatchServicer, error) {
	return &memBatch{scope: r.scope}, nil
}

// newMemRegistryService builds a service whose registry provider returns
// whatever *current points at when it is called.
func newMemRegistryService(t *testing.T, current *qrdbservice.Registry) QuicBaaS {
	t.Helper()
	shared := newSvcBoostMockQRDBProviderWithGet([]byte(`{}`), nil)
	var mu sync.Mutex
	svc := createTestService(t,
		withQRDBClientProvider(shared),
		withQRDBServiceRegistryProvider(func() qrdbservice.Registry {
			mu.Lock()
			defer mu.Unlock()
			return *current
		}))
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func TestKVRegistry_BatchPutThenGetSameScope(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	var reg qrdbservice.Registry = newMemRegistry()
	svc := newMemRegistryService(t, &reg)

	_, err := svc.BatchPut(ctx, &BatchPutRequest{Requests: []*PutRequest{
		{EntityType: "User", ID: 42, Data: []byte(`{"name":"a"}`)},
	}})
	require.NoError(t, err)

	resp, err := svc.Get(ctx, &GetRequest{EntityType: "User", ID: 42})
	require.NoError(t, err, "an entity written by BatchPut must be readable by Get in the same scope")
	require.NotNil(t, resp)
}

func TestKVRegistry_PutThenBatchDeleteRemoves(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	var reg qrdbservice.Registry = newMemRegistry()
	svc := newMemRegistryService(t, &reg)

	_, err := svc.Put(ctx, &PutRequest{EntityType: "User", ID: 42, Data: []byte(`{}`)})
	require.NoError(t, err)
	_, err = svc.BatchDelete(ctx, &BatchDeleteRequest{Requests: []*DeleteRequest{
		{EntityType: "User", ID: 42},
	}})
	require.NoError(t, err)

	_, err = svc.Get(ctx, &GetRequest{EntityType: "User", ID: 42})
	require.Error(t, err, "BatchDelete must remove the entity Put wrote")
	assert.Equal(t, errors.ErrNotFound, errors.Code(err), "%v", err)
}

func TestKVRegistry_ListEntityTypesSeesScopedWrites(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	var reg qrdbservice.Registry = newMemRegistry()
	svc := newMemRegistryService(t, &reg)

	_, err := svc.Put(ctx, &PutRequest{EntityType: "Gadget", ID: 7, Data: []byte(`{}`)})
	require.NoError(t, err)

	resp, err := svc.ListEntityTypes(ctx, &ListEntityTypesRequest{})
	require.NoError(t, err)
	assert.Contains(t, resp.EntityTypes, "Gadget")
}
