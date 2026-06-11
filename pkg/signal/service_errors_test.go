//go:build quicbaas_signal

package signal

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// kvBackendError is a sentinel returned by faultyKVClient when a method is
// configured to fail. It lets the error-path tests assert exact propagation.
var kvBackendError = errors.New("signal-test: kv backend failure")

// faultyKVClient wraps an in-memory map and lets individual operations be
// forced to return kvBackendError. It is used to exercise the error branches
// of the signal service that the happy-path serviceTestKVClient cannot reach.
type faultyKVClient struct {
	data map[string][]byte

	failPut    bool
	failGet    bool
	failDelete bool
	failList   bool
	failScan   bool

	// corruptKeys, when populated, returns invalid JSON from Get for the
	// listed keys so loadTenantConfigs exercises its unmarshal-continue path.
	corruptKeys map[string]bool
}

func newFaultyKVClient() *faultyKVClient {
	return &faultyKVClient{
		data:        make(map[string][]byte),
		corruptKeys: make(map[string]bool),
	}
}

func (c *faultyKVClient) Put(_ context.Context, key string, value []byte, _ ...qrdbtransport.WriteOption) error {
	if c.failPut {
		return kvBackendError
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	c.data[key] = cp
	return nil
}

func (c *faultyKVClient) Get(_ context.Context, key string, _ ...qrdbtransport.ReadOption) ([]byte, error) {
	if c.failGet {
		return nil, kvBackendError
	}
	if c.corruptKeys[key] {
		return []byte("{not-valid-json"), nil
	}
	v, ok := c.data[key]
	if !ok {
		return nil, &qrdbtransport.KeyNotFoundError{Key: key}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (c *faultyKVClient) Delete(_ context.Context, key string, _ ...qrdbtransport.WriteOption) error {
	if c.failDelete {
		return kvBackendError
	}
	if _, ok := c.data[key]; !ok {
		return &qrdbtransport.KeyNotFoundError{Key: key}
	}
	delete(c.data, key)
	return nil
}

func (c *faultyKVClient) List(_ context.Context, prefix string, _ ...qrdbtransport.ReadOption) ([]string, error) {
	if c.failList {
		return nil, kvBackendError
	}
	var result []string
	for k := range c.data {
		if strings.HasPrefix(k, prefix) {
			result = append(result, k)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (c *faultyKVClient) Scan(_ context.Context, prefix string, _ ...qrdbtransport.ReadOption) (map[string][]byte, error) {
	if c.failScan {
		return nil, kvBackendError
	}
	result := make(map[string][]byte)
	for k, v := range c.data {
		if strings.HasPrefix(k, prefix) {
			cp := make([]byte, len(v))
			copy(cp, v)
			result[k] = cp
		}
	}
	return result, nil
}

func (c *faultyKVClient) Exists(_ context.Context, key string, _ ...qrdbtransport.ReadOption) (bool, error) {
	_, ok := c.data[key]
	return ok, nil
}

var _ qrdbtransport.KVClient = (*faultyKVClient)(nil)

func newFaultyService(t *testing.T, kv *faultyKVClient) SignalService {
	t.Helper()
	log := newTestLogger(t)
	return NewSignalService(kv, log, &SignalConfig{Enabled: true})
}

func TestSignalService_Start_LoadConfigsError(t *testing.T) {
	kv := newFaultyKVClient()
	kv.failList = true
	svc := newFaultyService(t, kv)

	err := svc.Start(context.Background())
	if !errors.Is(err, kvBackendError) {
		t.Fatalf("expected kv backend error from Start, got %v", err)
	}
}

func TestSignalService_LoadTenantConfigs_GetErrorSkips(t *testing.T) {
	kv := newFaultyKVClient()
	key := tenantConfigKeyPrefix + "tenant-x" + tenantConfigKeySuffix
	kv.data[key] = []byte(`{"enabled":true}`)
	kv.failGet = true
	svc := newFaultyService(t, kv)

	// List succeeds but every Get fails; loadTenantConfigs must log-and-continue
	// and still return nil with no tenant cached.
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("expected Start to tolerate per-key Get errors, got %v", err)
	}
	if _, err := svc.GetTenantConfig(context.Background(), "tenant-x"); err == nil {
		t.Fatal("expected tenant-x to be absent after Get failure")
	}
}

func TestSignalService_LoadTenantConfigs_UnmarshalErrorSkips(t *testing.T) {
	kv := newFaultyKVClient()
	key := tenantConfigKeyPrefix + "tenant-y" + tenantConfigKeySuffix
	kv.data[key] = []byte("placeholder")
	kv.corruptKeys[key] = true
	svc := newFaultyService(t, kv)

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("expected Start to tolerate corrupt JSON, got %v", err)
	}
	if _, err := svc.GetTenantConfig(context.Background(), "tenant-y"); err == nil {
		t.Fatal("expected tenant-y to be absent after unmarshal failure")
	}
}

func TestSignalService_LoadTenantConfigs_Success(t *testing.T) {
	kv := newFaultyKVClient()
	key := tenantConfigKeyPrefix + "tenant-z" + tenantConfigKeySuffix
	kv.data[key] = []byte(`{"enabled":true,"maxAccounts":5}`)
	svc := newFaultyService(t, kv)

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("expected Start to succeed, got %v", err)
	}
	cfg, err := svc.GetTenantConfig(context.Background(), "tenant-z")
	if err != nil {
		t.Fatalf("expected tenant-z to be loaded, got %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected loaded tenant config to be enabled")
	}
}

func TestSignalService_ProvisionTenant_PutError(t *testing.T) {
	kv := newFaultyKVClient()
	kv.failPut = true
	svc := newFaultyService(t, kv)

	cfg := &SignalTenantConfig{Enabled: true}
	err := svc.ProvisionTenant(context.Background(), "tenant-put", cfg)
	if !errors.Is(err, kvBackendError) {
		t.Fatalf("expected kv backend error from ProvisionTenant, got %v", err)
	}

	// The in-memory cache must not retain the tenant when the Put failed.
	if _, gErr := svc.GetTenantConfig(context.Background(), "tenant-put"); gErr == nil {
		t.Fatal("expected tenant-put to be absent after Put failure")
	}
}

func TestSignalService_DeprovisionTenant_DeleteError(t *testing.T) {
	kv := newFaultyKVClient()
	svc := newFaultyService(t, kv)

	cfg := &SignalTenantConfig{Enabled: true}
	if err := svc.ProvisionTenant(context.Background(), "tenant-del", cfg); err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	kv.failDelete = true
	err := svc.DeprovisionTenant(context.Background(), "tenant-del")
	if !errors.Is(err, kvBackendError) {
		t.Fatalf("expected kv backend error from DeprovisionTenant, got %v", err)
	}

	// The tenant must remain cached because the Delete failed.
	if _, gErr := svc.GetTenantConfig(context.Background(), "tenant-del"); gErr != nil {
		t.Fatalf("expected tenant-del to remain cached after Delete failure, got %v", gErr)
	}
}

func TestSignalService_RegisterAccount_CountError(t *testing.T) {
	kv := newFaultyKVClient()
	svc := newFaultyService(t, kv)

	cfg := &SignalTenantConfig{Enabled: true}
	if err := svc.ProvisionTenant(context.Background(), "tenant-count", cfg); err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	ctx := iam.ContextWithTenant(context.Background(), "tenant-count")
	kv.failList = true // countAccounts -> tenantKV.List fails

	_, err := svc.RegisterAccount(ctx, &RegisterRequest{Number: "+15555550100"})
	if !errors.Is(err, kvBackendError) {
		t.Fatalf("expected kv backend error from RegisterAccount count path, got %v", err)
	}
}
