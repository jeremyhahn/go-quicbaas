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

package server

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-qrdb/pkg/seal"
	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func providerAccountsRegistry(t *testing.T) *orchestration.AccountProviderRegistry {
	t.Helper()
	registry, err := orchestration.NewAccountProviderRegistry()
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	return registry
}

// tenantProviderAccounts returns tenantID's accounts of each frontier
// provider, keyed by provider.
func tenantProviderAccounts(t *testing.T, store orchestration.AccountStore, tenantID string) map[orchestration.AccountProvider][]string {
	t.Helper()
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	got := map[orchestration.AccountProvider][]string{}
	for _, provider := range []orchestration.AccountProvider{orchestration.AccountProviderClaude, orchestration.AccountProviderCodex} {
		accounts, err := store.ListByTenant(ctx, tenantID, provider, "")
		if err != nil {
			t.Fatalf("ListByTenant(%s, %s) error = %v", tenantID, provider, err)
		}
		for _, account := range accounts {
			got[provider] = append(got[provider], account.ID)
		}
	}
	return got
}

func assertSeededDefaults(t *testing.T, store orchestration.AccountStore, tenantID string) {
	t.Helper()
	got := tenantProviderAccounts(t, store, tenantID)
	claude, codex := got[orchestration.AccountProviderClaude], got[orchestration.AccountProviderCodex]
	if len(claude) != 1 || claude[0] != "default-claude" || len(codex) != 1 || codex[0] != "default-codex" {
		t.Fatalf("tenant %s accounts = %v, want exactly default-claude and default-codex", tenantID, got)
	}
}

func TestSeedDefaultProviderAccounts_SystemTenantAtBoot(t *testing.T) {
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))
	assertSeededDefaults(t, store, orchestration.SystemTenantID)
}

func TestSeedDefaultProviderAccounts_ReachesATenantUnsealedLater(t *testing.T) {
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))
	if got := tenantProviderAccounts(t, store, "acme"); len(got) != 0 {
		t.Fatalf("a tenant the boot pass does not reach has accounts %v", got)
	}

	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	if err := barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")); err != nil {
		t.Fatalf("unseal: %v", err)
	}
	assertSeededDefaults(t, store, "acme")
}

// TestSeedDefaultProviderAccounts_ReachesATenantCreatedLater pins that a
// tenant created after the boot pass, which has no barrier unseal to wait
// for when it is prefix-isolated, has both default accounts as soon as its
// creation returns.
func TestSeedDefaultProviderAccounts_ReachesATenantCreatedLater(t *testing.T) {
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))
	if got := tenantProviderAccounts(t, store, "acme"); len(got) != 0 {
		t.Fatalf("a tenant the boot pass does not reach has accounts %v", got)
	}

	manager := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
	manager.AddCreatedObserver(&s.tenantCreatedHooks)
	if _, err := manager.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: "acme", Name: "Acme"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	assertSeededDefaults(t, store, "acme")
}

func TestSeedDefaultProviderAccounts_SecondBootCreatesNothing(t *testing.T) {
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	registry := providerAccountsRegistry(t)
	s.seedDefaultProviderAccounts(context.Background(), store, registry)
	s.seedDefaultProviderAccounts(context.Background(), store, registry)
	assertSeededDefaults(t, store, orchestration.SystemTenantID)
}

// nativeTenantDirectory is go-qrdb's native tenant service as far as the
// default accounts see it: the tenants it creates and lists.
type nativeTenantDirectory struct {
	qrdbsdk.ServiceTenantServicer

	mu        sync.Mutex
	tenants   []qrdbtenant.TenantInfo
	createErr error
}

func (d *nativeTenantDirectory) CreateTenant(_ context.Context, id, name string, isolationType qrdbtenant.IsolationType, _ ...qrdbtenant.CreateTenantOption) (*qrdbtenant.TenantInfo, error) {
	if d.createErr != nil {
		return nil, d.createErr
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	info := qrdbtenant.TenantInfo{ID: id, Name: name, IsolationType: isolationType}
	d.tenants = append(d.tenants, info)
	return &info, nil
}

func (d *nativeTenantDirectory) ListTenants(context.Context) ([]qrdbtenant.TenantInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.tenants), nil
}

func TestSeedDefaultProviderAccounts_ReachesATenantCreatedNatively(t *testing.T) {
	s := agentsTestServer(t, nil)
	directory := &nativeTenantDirectory{}
	s.tenantUnsealRouter = s.newNodeTenantRouter(func() qrdbsdk.ServiceTenantServicer { return directory })
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))

	// The servicer the SDK's TenantMgmt() is wired to in an embedded node;
	// the network transports call the router itself.
	tenantMgmt := hookedTenantServicer(directory, s.tenantUnsealRouter)
	if _, err := tenantMgmt.CreateTenant(context.Background(), "acme", "Acme", qrdbtenant.IsolationBarrier); err != nil {
		t.Fatalf("native CreateTenant: %v", err)
	}
	assertSeededDefaults(t, store, "acme")

	if _, err := s.tenantUnsealRouter.CreateTenant(context.Background(), "globex", "Globex", qrdbtenant.IsolationBarrier); err != nil {
		t.Fatalf("router CreateTenant: %v", err)
	}
	assertSeededDefaults(t, store, "globex")
}

func TestSeedDefaultProviderAccounts_FailedNativeCreateSeedsNothing(t *testing.T) {
	s := agentsTestServer(t, nil)
	directory := &nativeTenantDirectory{createErr: errors.New("tenant exists")}
	s.tenantUnsealRouter = s.newNodeTenantRouter(func() qrdbsdk.ServiceTenantServicer { return directory })
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))

	if _, err := s.tenantUnsealRouter.CreateTenant(context.Background(), "acme", "Acme", qrdbtenant.IsolationBarrier); err == nil {
		t.Fatal("native CreateTenant: want the directory's error")
	}
	if got := tenantProviderAccounts(t, store, "acme"); len(got) != 0 {
		t.Fatalf("a tenant that was not created has accounts %v", got)
	}
}

func TestSeedDefaultProviderAccounts_ReachesANativeTenantAtBoot(t *testing.T) {
	s := agentsTestServer(t, nil)
	directory := &nativeTenantDirectory{tenants: []qrdbtenant.TenantInfo{{ID: "acme", Name: "Acme"}}}
	s.tenantUnsealRouter = s.newNodeTenantRouter(func() qrdbsdk.ServiceTenantServicer { return directory })
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))
	assertSeededDefaults(t, store, orchestration.SystemTenantID)
	assertSeededDefaults(t, store, "acme")
}

func TestSeedDefaultProviderAccounts_NoNodeTenantServiceAtBoot(t *testing.T) {
	s := agentsTestServer(t, nil)
	s.tenantUnsealRouter = s.newNodeTenantRouter(func() qrdbsdk.ServiceTenantServicer { return nil })
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))
	assertSeededDefaults(t, store, orchestration.SystemTenantID)
}

// sealedAccountStore is the tenant-routed store as seeding sees it: a
// barrier tenant's records are unreachable until its barrier is initialized
// and unsealed.
type sealedAccountStore struct {
	orchestration.AccountStore

	mu     sync.Mutex
	sealed map[string]bool
}

func (s *sealedAccountStore) seal(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sealed[id] = true
}

func (s *sealedAccountStore) open(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sealed, id)
}

func (s *sealedAccountStore) isSealed(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sealed[id]
}

func (s *sealedAccountStore) ListByTenant(ctx context.Context, tenantID string, provider orchestration.AccountProvider, projectID string) ([]*orchestration.Account, error) {
	if s.isSealed(tenantID) {
		return nil, qrdbtenant.ErrTenantSealed
	}
	return s.AccountStore.ListByTenant(ctx, tenantID, provider, projectID)
}

func (s *sealedAccountStore) Create(ctx context.Context, account *orchestration.Account) error {
	if s.isSealed(account.TenantID) {
		return qrdbtenant.ErrTenantSealed
	}
	return s.AccountStore.Create(ctx, account)
}

// barrierTenantDirectory is a node tenant service whose self-managed barrier
// tenants are created sealed and uninitialized, and opened by a barrier init.
type barrierTenantDirectory struct {
	*nativeTenantDirectory

	store   *sealedAccountStore
	initErr error
}

func (d *barrierTenantDirectory) CreateTenant(ctx context.Context, id, name string, isolationType qrdbtenant.IsolationType, opts ...qrdbtenant.CreateTenantOption) (*qrdbtenant.TenantInfo, error) {
	info, err := d.nativeTenantDirectory.CreateTenant(ctx, id, name, isolationType, opts...)
	if err == nil && isolationType == qrdbtenant.IsolationBarrier {
		d.store.seal(id)
	}
	return info, err
}

func (d *barrierTenantDirectory) TenantBarrierInit(_ context.Context, id string, _ []byte, _, _ int) (*qrdbtenant.TenantBarrierInitResult, error) {
	if d.initErr != nil {
		return nil, d.initErr
	}
	d.store.open(id)
	return &qrdbtenant.TenantBarrierInitResult{TenantID: id}, nil
}

func (d *barrierTenantDirectory) TenantBarrierInitWithStrategy(_ context.Context, id string, _ []byte, _, _ int, _ seal.StrategyID) (*qrdbtenant.TenantBarrierInitResult, error) {
	if d.initErr != nil {
		return nil, d.initErr
	}
	d.store.open(id)
	return &qrdbtenant.TenantBarrierInitResult{TenantID: id}, nil
}

func TestSeedDefaultProviderAccounts_ReachesANativeBarrierTenantOnceInitialized(t *testing.T) {
	ctx := context.Background()
	s := agentsTestServer(t, nil)
	store := &sealedAccountStore{AccountStore: orchestration.NewInMemoryAccountStore(), sealed: map[string]bool{}}
	directory := &barrierTenantDirectory{nativeTenantDirectory: &nativeTenantDirectory{}, store: store}
	s.tenantUnsealRouter = s.newNodeTenantRouter(func() qrdbsdk.ServiceTenantServicer { return directory })
	s.seedDefaultProviderAccounts(ctx, store, providerAccountsRegistry(t))

	if _, err := s.tenantUnsealRouter.CreateTenant(ctx, "acme", "Acme", qrdbtenant.IsolationBarrier); err != nil {
		t.Fatalf("router CreateTenant: %v", err)
	}
	if got := tenantProviderAccounts(t, store.AccountStore, "acme"); len(got) != 0 {
		t.Fatalf("a sealed barrier tenant has accounts %v before its init", got)
	}
	if _, err := s.tenantUnsealRouter.TenantBarrierInit(ctx, "acme", []byte("passphrase"), 0, 0); err != nil {
		t.Fatalf("TenantBarrierInit: %v", err)
	}
	assertSeededDefaults(t, store, "acme")

	if _, err := s.tenantUnsealRouter.CreateTenant(ctx, "globex", "Globex", qrdbtenant.IsolationBarrier); err != nil {
		t.Fatalf("router CreateTenant: %v", err)
	}
	// The servicer the SDK's TenantMgmt() is wired to in an embedded node.
	tenantMgmt := hookedTenantServicer(directory, s.tenantUnsealRouter)
	if _, err := tenantMgmt.TenantBarrierInitWithStrategy(ctx, "globex", []byte("passphrase"), 0, 0, seal.StrategyPassphrase); err != nil {
		t.Fatalf("TenantBarrierInitWithStrategy: %v", err)
	}
	assertSeededDefaults(t, store, "globex")
}

func TestSeedDefaultProviderAccounts_FailedBarrierInitSeedsNothing(t *testing.T) {
	ctx := context.Background()
	s := agentsTestServer(t, nil)
	store := &sealedAccountStore{AccountStore: orchestration.NewInMemoryAccountStore(), sealed: map[string]bool{}}
	directory := &barrierTenantDirectory{nativeTenantDirectory: &nativeTenantDirectory{}, store: store, initErr: errors.New("init refused")}
	s.tenantUnsealRouter = s.newNodeTenantRouter(func() qrdbsdk.ServiceTenantServicer { return directory })
	s.seedDefaultProviderAccounts(ctx, store, providerAccountsRegistry(t))

	if _, err := s.tenantUnsealRouter.CreateTenant(ctx, "acme", "Acme", qrdbtenant.IsolationBarrier); err != nil {
		t.Fatalf("router CreateTenant: %v", err)
	}
	if _, err := s.tenantUnsealRouter.TenantBarrierInit(ctx, "acme", []byte("passphrase"), 0, 0); err == nil {
		t.Fatal("TenantBarrierInit succeeded, want the servicer's error")
	}
	if got := tenantProviderAccounts(t, store.AccountStore, "acme"); len(got) != 0 {
		t.Fatalf("a tenant whose barrier init failed has accounts %v", got)
	}
}

// pausingTenantDirectory is the boot pass paused between its directory
// snapshot and the rest of its work.
type pausingTenantDirectory struct {
	*nativeTenantDirectory

	afterSnapshot func()
}

func (d *pausingTenantDirectory) ListTenants(ctx context.Context) ([]qrdbtenant.TenantInfo, error) {
	tenants, err := d.nativeTenantDirectory.ListTenants(ctx)
	if pause := d.afterSnapshot; pause != nil {
		d.afterSnapshot = nil
		pause()
	}
	return tenants, err
}

func TestSeedDefaultProviderAccounts_ReachesATenantCreatedDuringTheBootPass(t *testing.T) {
	ctx := context.Background()
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	directory := &pausingTenantDirectory{nativeTenantDirectory: &nativeTenantDirectory{}}
	s.tenantUnsealRouter = s.newNodeTenantRouter(func() qrdbsdk.ServiceTenantServicer { return directory })
	directory.afterSnapshot = func() {
		if _, err := s.tenantUnsealRouter.CreateTenant(ctx, "acme", "Acme", qrdbtenant.IsolationPrefix); err != nil {
			t.Errorf("router CreateTenant: %v", err)
		}
	}
	s.seedDefaultProviderAccounts(ctx, store, providerAccountsRegistry(t))
	assertSeededDefaults(t, store, "acme")
}

func TestSeedDefaultProviderAccounts_ReachesATenantUnsealedDuringTheBootPass(t *testing.T) {
	ctx := context.Background()
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	directory := &pausingTenantDirectory{nativeTenantDirectory: &nativeTenantDirectory{}}
	s.tenantUnsealRouter = s.newNodeTenantRouter(func() qrdbsdk.ServiceTenantServicer { return directory })
	directory.afterSnapshot = func() {
		barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
		if err := barrier.TenantBarrierUnseal(tenantAdminCtx("globex"), "globex", []byte("key")); err != nil {
			t.Errorf("unseal: %v", err)
		}
	}
	s.seedDefaultProviderAccounts(ctx, store, providerAccountsRegistry(t))
	assertSeededDefaults(t, store, "globex")
}
